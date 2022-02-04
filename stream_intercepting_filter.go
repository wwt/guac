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
	_ Filter = (*InputStreamInterceptingFilter)(nil)
	_ Filter = (*OutputStreamInterceptingFilter)(nil)
)

// Whether this OutputStreamInterceptingFilter should respond to received
// blobs with "ack" messages on behalf of the client. If false, blobs will
// still be handled by this filter, but empty blobs will be sent to the
// client, forcing the client to respond on its own.
var acknowledgeBlobs bool = true

type InputStreamInterceptingFilter struct {
	tunnel      Tunnel
	istreamLock sync.Mutex

	streams map[string]*InterceptedInputStream
}

func NewInputStreamInterceptingFilter(tunnel Tunnel) *InputStreamInterceptingFilter {
	streams := make(map[string]*InterceptedInputStream)
	return &InputStreamInterceptingFilter{tunnel: tunnel, streams: streams}
}

func (t *InputStreamInterceptingFilter) sendInstruction(instr *Instruction) (err error) {
	w := t.tunnel.AcquireWriter()
	defer t.tunnel.ReleaseWriter()

	if _, err = w.Write(instr.Byte()); err != nil {
		return err
	}

	return nil
}

func (t *InputStreamInterceptingFilter) getInterceptedInputStream(index string) *InterceptedInputStream {
	return t.streams[index]
}

func (t *InputStreamInterceptingFilter) closeInterceptedStream(index string) {
	t.istreamLock.Lock()
	if t.streams[index] != nil {
		t.streams[index].Closed <- true
	}
	delete(t.streams, index)
	t.istreamLock.Unlock()
}

func (t *InputStreamInterceptingFilter) CloseAll() {
	for k := range t.streams {
		t.closeInterceptedStream(k)
	}
}

func (t *InputStreamInterceptingFilter) InterceptStream(index int, stream io.Reader) error {
	indexStr := strconv.Itoa(index)

	interceptedInputStream := NewInterceptedInputStream(indexStr, stream)

	logrus.Debug("intercepting input stream", indexStr)

	t.istreamLock.Lock()
	t.streams[indexStr] = interceptedInputStream
	t.istreamLock.Unlock()

	t.handleInterceptedStream(interceptedInputStream)

	<-interceptedInputStream.Closed

	return interceptedInputStream.Error
}

func (t *InputStreamInterceptingFilter) sendBlob(index string, blob []byte) {
	data := base64.StdEncoding.Strict().EncodeToString(blob)
	if err := t.sendInstruction(NewInstruction("blob", index, data)); err != nil {
		logrus.Errorf("failed to send base64 blob to stream index %s %v", index, err)
	}
}

func (t *InputStreamInterceptingFilter) sendEnd(index string) {
	if err := t.sendInstruction(NewInstruction("end", index)); err != nil {
		logrus.Errorf("failed to send end to stream index %s %v", index, err)
	}
}

func (t *InputStreamInterceptingFilter) readNextBlob(stream *InterceptedInputStream) {
	blob := make([]byte, 4096)

	if n, err := stream.Stream.Read(blob); err != nil {
		if n > 0 {
			t.sendBlob(stream.Index, blob[:n])
			return
		}
		logrus.Errorf("could not read from stream %s: %v", stream.Index, err)
		t.sendEnd(stream.Index)
		t.closeInterceptedStream(stream.Index)

		return
	}

	t.sendBlob(stream.Index, blob)
}

func (t *InputStreamInterceptingFilter) handleACK(instruction *Instruction) {
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

		stream.Error = ErrServer.NewError(code.String(), instruction.Args[1])
		t.closeInterceptedStream(stream.Index)
		return
	}

	t.readNextBlob(stream)
}

func (t *InputStreamInterceptingFilter) Filter(instruction *Instruction) (*Instruction, error) {
	if instruction.Opcode == "ack" {
		t.handleACK(instruction)
	}

	return instruction, nil
}

func (t *InputStreamInterceptingFilter) handleInterceptedStream(stream *InterceptedInputStream) {
	t.readNextBlob(stream)
}

type OutputStreamInterceptingFilter struct {
	istreamLock sync.Mutex
	tunnel      Tunnel
	streams     map[string]*InterceptedOutputStream
}

func NewOutputStreamInterceptingFilter(tunnel Tunnel) *OutputStreamInterceptingFilter {
	streams := make(map[string]*InterceptedOutputStream)
	return &OutputStreamInterceptingFilter{tunnel: tunnel, streams: streams}
}

func (t *OutputStreamInterceptingFilter) sendInstruction(instr *Instruction) error {
	w := t.tunnel.AcquireWriter()
	if _, err := w.Write(instr.Byte()); err != nil {
		return err
	}

	t.tunnel.ReleaseWriter()
	return nil
}

func (t *OutputStreamInterceptingFilter) getInterceptedStream(idx string) *InterceptedOutputStream {
	return t.streams[idx]
}

func (t *OutputStreamInterceptingFilter) sendACK(index string, message string, status Status) {
	if status != Success {
		t.closeInterceptedStream(index)
	}

	if err := t.sendInstruction(NewInstruction("ack", index, message, status.String())); err != nil {
		logrus.Errorf("unable to send ACK for stream %s", index)
	}
}

func (t *OutputStreamInterceptingFilter) InterceptStream(index int, outStream io.Writer) error {
	idxStr := strconv.Itoa(index)
	if t.tunnel == nil {
		return errors.New("invalid tunnel, it's nil")
	}

	interceptedOutputStream := NewInterceptedOutputStream(idxStr, outStream)

	logrus.Debug(idxStr, "is now intercepted by outStream", outStream)

	t.istreamLock.Lock()
	t.streams[idxStr] = interceptedOutputStream
	t.istreamLock.Unlock()

	t.handleInterceptedStream(interceptedOutputStream)

	<-interceptedOutputStream.Closed

	return interceptedOutputStream.Error
}

func (t *OutputStreamInterceptingFilter) handleBlob(instruction *Instruction) (*Instruction, error) {
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
		logrus.Error("stream in outputInterceptedStream is nil")
		return nil, errors.New("stream in outputInterceptedStream is nil")
	}

	if _, err := outputInterceptedStream.Stream.Write(blob); err != nil {
		logrus.WithError(err).Error("failed to write to intercepted stream")
		return nil, err
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

func (t *OutputStreamInterceptingFilter) handleEnd(instruction *Instruction) {
	args := instruction.Args
	if len(args) < 1 {
		return
	}

	t.closeInterceptedStream(args[0])
}

func (t *OutputStreamInterceptingFilter) handleSync(instruction *Instruction) {
	acknowledgeBlobs = false
}

func (t *OutputStreamInterceptingFilter) Filter(instruction *Instruction) (*Instruction, error) {
	switch instruction.Opcode {
	case "blob":
		// When a user cancels the download, the connection abruptly drops
		// TODO: find a better design
		return t.handleBlob(instruction)
	case "end":
		t.handleEnd(instruction)
		return instruction, nil
	case "sync":
		t.handleSync(instruction)
		return instruction, nil
	default:
		return instruction, nil
	}
}

func (t *OutputStreamInterceptingFilter) handleInterceptedStream(stream *InterceptedOutputStream) {
	t.sendACK(stream.Index, "OK", Success)
}

func (t *OutputStreamInterceptingFilter) closeInterceptedStream(index string) *InterceptedOutputStream {
	interceptedStream := t.streams[index]
	if interceptedStream != nil {
		interceptedStream.Closed <- true
	}

	t.istreamLock.Lock()
	delete(t.streams, index)
	t.istreamLock.Unlock()

	return interceptedStream
}

func (t *OutputStreamInterceptingFilter) CloseAllInterceptedStreams() {
	for k := range t.streams {
		t.closeInterceptedStream(k)
	}
}
