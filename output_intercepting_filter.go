package guac

import (
	"encoding/base64"
	"errors"
	"io"
	"sync"

	"github.com/sirupsen/logrus"
)

type OutputInterceptingFilter struct {
	l       sync.Mutex
	tunnel  Tunnel
	streams map[string]*InterceptedOutputStream
}

func NewOutputInterceptingFilter(tunnel Tunnel) *OutputInterceptingFilter {
	streams := make(map[string]*InterceptedOutputStream)
	return &OutputInterceptingFilter{tunnel: tunnel, streams: streams}
}

func (t *OutputInterceptingFilter) sendInstruction(instr *Instruction) error {
	w := t.tunnel.AcquireWriter()
	if _, err := w.Write(instr.Byte()); err != nil {
		logrus.WithError(err).Error("failed to send instruction")
		return err
	}

	t.tunnel.ReleaseWriter()
	return nil
}

func (t *OutputInterceptingFilter) getInterceptedStream(idx string) *InterceptedOutputStream {
	t.l.Lock()
	defer t.l.Unlock()

	return t.streams[idx]
}

func (t *OutputInterceptingFilter) sendACK(index string, message string, status Status) {
	if status != Success {
		t.closeInterceptedStream(index, ErrServer.NewError(status.String(), message))
	}

	if err := t.sendInstruction(NewInstruction("ack", index, message, status.String())); err != nil {
		logrus.Errorf("unable to send ACK for stream %s", index)
	}
}

func (t *OutputInterceptingFilter) InterceptStream(index string, outStream io.Writer) <-chan error {
	signal := make(chan error, 1)

	if t.tunnel == nil {
		defer func() {
			signal <- errors.New("invalid tunnel")
		}()

		return signal
	}

	interceptedOutputStream := NewInterceptedOutputStream(index, outStream, signal)

	t.l.Lock()
	t.streams[index] = interceptedOutputStream
	t.l.Unlock()

	t.handleInterceptedStream(interceptedOutputStream)

	return signal
}

func (t *OutputInterceptingFilter) handleBlob(instruction *Instruction) (*Instruction, error) {
	// Verify all required arguments are present
	args := instruction.Args
	if len(args) < 2 {
		return instruction, nil
	}

	// Pull associated stream
	streamIndex := args[0]

	outputInterceptedStream := t.getInterceptedStream(streamIndex)
	if outputInterceptedStream == nil {
		return instruction, nil
	}

	// Decode blob
	data := args[1]

	blob, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}

	if outputInterceptedStream.Stream == nil {
		return nil, errors.New("stream in outputInterceptedStream is nil")
	}

	if _, err := outputInterceptedStream.Stream.Write(blob); err != nil {
		// User closed the connection, no need to panic,
		// Just don't track it anymore and close the stream.
		t.closeInterceptedStream(streamIndex, nil)

		logrus.WithError(err).Info("failed to write to intercepted stream: maybe user has closed the connection?")

		// Exit cleanly, we don't need to make the server quit listening.
		return nil, nil
	}

	// Force client to respond with their own "ack" if we need to
	// confirm that they are not falling behind with respect to the
	// graphical session
	if !acknowledgeBlobs {
		acknowledgeBlobs = true
		return NewInstruction("blob", streamIndex, ""), nil
	}

	t.sendACK(streamIndex, "OK", Success)

	// Instruction was handled purely internally
	return nil, nil
}

func (t *OutputInterceptingFilter) handleEnd(instruction *Instruction) {
	args := instruction.Args
	if len(args) < 1 {
		return
	}

	t.closeInterceptedStream(args[0], nil)
}

func (t *OutputInterceptingFilter) handleSync(instruction *Instruction) {
	acknowledgeBlobs = false
}

func (t *OutputInterceptingFilter) Filter(instruction *Instruction) (*Instruction, error) {
	switch instruction.Opcode {
	case "blob":
		return t.handleBlob(instruction)
	case "end":
		t.handleEnd(instruction)
	case "sync":
		t.handleSync(instruction)
	}
	return instruction, nil
}

func (t *OutputInterceptingFilter) handleInterceptedStream(stream *InterceptedOutputStream) {
	t.sendACK(stream.Index, "OK", Success)
}

func (t *OutputInterceptingFilter) closeInterceptedStream(index string, err error) *InterceptedOutputStream {
	interceptedStream := t.streams[index]
	if interceptedStream != nil {
		interceptedStream.done <- err
	}

	t.l.Lock()
	delete(t.streams, index)
	t.l.Unlock()

	return interceptedStream
}

func (t *OutputInterceptingFilter) CloseAllInterceptedStreams() {
	for k := range t.streams {
		t.closeInterceptedStream(k, nil)
	}
}
