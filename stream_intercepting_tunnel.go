package guac

import (
	"io"

	"github.com/sirupsen/logrus"
)

var _ Tunnel = (*StreamInterceptingTunnel)(nil)

type StreamInterceptingTunnel struct {
	tunnel Tunnel

	outputStreamFilter *OutputStreamInterceptingFilter
	inputStreamFilter  *InputStreamInterceptingFilter
}

func NewStreamInterceptingTunnel(tunnel Tunnel) *StreamInterceptingTunnel {
	stream := &StreamInterceptingTunnel{tunnel: tunnel}
	stream.outputStreamFilter = NewOutputStreamInterceptingFilter(stream)
	stream.inputStreamFilter = NewInputStreamInterceptingFilter(stream)
	return stream
}

func (t *StreamInterceptingTunnel) AcquireReader() InstructionReader {
	reader := t.tunnel.AcquireReader()

	reader = NewFilteredGuacamoleReader(reader, t.outputStreamFilter)
	reader = NewFilteredGuacamoleReader(reader, t.inputStreamFilter)

	return reader
}

func (t *StreamInterceptingTunnel) ReleaseReader() {
	t.tunnel.ReleaseReader()
}

func (t *StreamInterceptingTunnel) HasQueuedReaderThreads() bool {
	return t.tunnel.HasQueuedReaderThreads()
}

func (t *StreamInterceptingTunnel) AcquireWriter() io.Writer {
	return t.tunnel.AcquireWriter()
}

func (t *StreamInterceptingTunnel) ReleaseWriter() {
	t.tunnel.ReleaseWriter()
}

func (t *StreamInterceptingTunnel) HasQueuedWriterThreads() bool {
	return t.tunnel.HasQueuedWriterThreads()
}

func (t *StreamInterceptingTunnel) GetUUID() string {
	return t.tunnel.GetUUID()
}

func (t *StreamInterceptingTunnel) Close() error {
	t.outputStreamFilter.CloseAllInterceptedStreams()

	return t.tunnel.Close()
}

func (t *StreamInterceptingTunnel) ConnectionID() string {
	return t.tunnel.ConnectionID()
}

func (t *StreamInterceptingTunnel) InterceptOutputStream(idx int, output io.Writer) error {
	logrus.Debugf("Intercepting output stream %d of tunnel %s", idx, t.tunnel.ConnectionID())

	if err := t.outputStreamFilter.InterceptStream(idx, output); err != nil {
		return err
	}

	logrus.Debugf("Finished intercepting output stream %d of tunnel %s", idx, t.ConnectionID())
	return nil
}

func (t *StreamInterceptingTunnel) InterceptInputStream(idx int, input io.Reader) error {
	logrus.Debugf("Intercepting input stream %d of tunnel %s", idx, t.ConnectionID())

	if err := t.inputStreamFilter.InterceptStream(idx, input); err != nil {
		return err
	}

	logrus.Debugf("Finished intercepting input stream %d of tunnel %s", idx, t.ConnectionID())
	return nil
}
