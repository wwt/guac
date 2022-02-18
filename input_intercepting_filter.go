package guac

import (
	"encoding/base64"
	"errors"
	"io"
	"strconv"
	"sync"

	"github.com/sirupsen/logrus"
)

var (
	_ Filter = (*InputInterceptingFilter)(nil)
	_ Filter = (*OutputInterceptingFilter)(nil)
)

// Whether this OutputInterceptingFilter should respond to received
// blobs with "ack" messages on behalf of the client. If false, blobs will
// still be handled by this filter, but empty blobs will be sent to the
// client, forcing the client to respond on its own.
var acknowledgeBlobs bool = true

type InputInterceptingFilter struct {
	tunnel Tunnel
	l      sync.Mutex

	streams map[string]*InterceptedInputStream
}

func NewInputInterceptingFilter(tunnel Tunnel) *InputInterceptingFilter {
	streams := make(map[string]*InterceptedInputStream)
	return &InputInterceptingFilter{tunnel: tunnel, streams: streams}
}

func (t *InputInterceptingFilter) sendInstruction(instr *Instruction) (err error) {
	w := t.tunnel.AcquireWriter()
	defer t.tunnel.ReleaseWriter()

	if _, err = w.Write(instr.Byte()); err != nil {
		logrus.WithError(err).Error("failed to write instruction")
		return err
	}

	return nil
}

func (t *InputInterceptingFilter) getInterceptedInputStream(index string) *InterceptedInputStream {
	t.l.Lock()
	defer t.l.Unlock()

	return t.streams[index]
}

func (t *InputInterceptingFilter) closeInterceptedStream(index string, err error) {
	t.l.Lock()
	defer t.l.Unlock()

	if t.streams[index] != nil {
		t.streams[index].done <- err
	}
	delete(t.streams, index)
}

func (t *InputInterceptingFilter) CloseAll() {
	for k := range t.streams {
		t.closeInterceptedStream(k, nil)
	}
}

func (t *InputInterceptingFilter) InterceptStream(index string, stream io.Reader) <-chan error {
	signal := make(chan error, 1)

	interceptedInputStream := NewInterceptedInputStream(index, stream, signal)

	t.l.Lock()
	t.streams[index] = interceptedInputStream
	t.l.Unlock()

	t.handleInterceptedStream(interceptedInputStream)

	return signal
}

func (t *InputInterceptingFilter) sendBlob(index string, blob []byte) {
	data := base64.StdEncoding.Strict().EncodeToString(blob)
	if err := t.sendInstruction(NewInstruction("blob", index, data)); err != nil {
		logrus.Errorf("failed to send base64 blob to stream index %s %v", index, err)

		t.sendEnd(index)
		t.closeInterceptedStream(index, err)
	}
}

func (t *InputInterceptingFilter) sendEnd(index string) {
	if err := t.sendInstruction(NewInstruction("end", index)); err != nil {
		logrus.Errorf("failed to send end to stream index %s %v", index, err)
	}
}

func (t *InputInterceptingFilter) readNextBlob(stream *InterceptedInputStream) {
	blob := make([]byte, 4096)

	if n, err := io.ReadFull(stream.Stream, blob); err != nil {
		if n > 0 {
			logrus.Debug("there are still some bytes")
			t.sendBlob(stream.Index, blob[:n])
			return
		}

		if !errors.Is(err, io.EOF) {
			logrus.WithError(err).Errorf("could not read from stream %s", stream.Index)
		} else {
			err = nil
		}

		t.sendEnd(stream.Index)
		t.closeInterceptedStream(stream.Index, err)

		return
	}

	t.sendBlob(stream.Index, blob)
}

func (t *InputInterceptingFilter) handleACK(instruction *Instruction) {
	if len(instruction.Args) < 3 {
		return
	}

	index := instruction.Args[0]

	stream := t.getInterceptedInputStream(index)
	if stream == nil {
		logrus.Warning("empty intercepted input stream on ACK")
		return
	}

	status := instruction.Args[2]
	code := Success

	if status != "0" {
		codeInt, err := strconv.Atoi(status)
		code = FromGuacamoleStatusCode(codeInt)

		if err != nil {
			logrus.Error("failed to translate status code")
			code = ServerError
		}

		t.closeInterceptedStream(stream.Index, ErrServer.NewError(code.String(), instruction.Args[1]))
		return
	}

	t.readNextBlob(stream)
}

func (t *InputInterceptingFilter) Filter(instruction *Instruction) (*Instruction, error) {
	if instruction.Opcode == "ack" {
		t.handleACK(instruction)
	}

	return instruction, nil
}

func (t *InputInterceptingFilter) handleInterceptedStream(stream *InterceptedInputStream) {
	t.readNextBlob(stream)
}
