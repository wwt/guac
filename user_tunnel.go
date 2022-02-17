package guac

import "io"

// Ensure UserTunnel implements Tunnel
var _ Tunnel = (*UserTunnel)(nil)

type UserTunnel struct {
	Tunnel

	outputFilter *OutputInterceptingFilter
	inputFilter  *InputInterceptingFilter
}

func NewUserTunnel(tunnel Tunnel) *UserTunnel {
	tun := &UserTunnel{Tunnel: tunnel}

	tun.inputFilter, tun.outputFilter = NewInputInterceptingFilter(tun), NewOutputInterceptingFilter(tun)

	return tun
}

// InterceptOutputStream intercepts an output stream, i.e. when downloading
// a file you provide a http.ResponseWriter and InterceptOutputStream will
// pipe the stream numbers through it.
func (t *UserTunnel) InterceptOutputStream(id string, stream io.Writer) error {
	return <-t.outputFilter.InterceptStream(id, stream)
}

// InterceptInputStream intercepts an input stream, i.e. when uploading a file.
// For example you can pass a http.Request.Body() to inject a file in a Guacamole stream.
func (t *UserTunnel) InterceptInputStream(id string, stream io.Reader) error {
	return <-t.inputFilter.InterceptStream(id, stream)
}

// AcquireReader of UserTunnel wraps the original AcquireReader
// but it filters the instructions before handing them to the
// caller.
func (t *UserTunnel) AcquireReader() InstructionReader {
	reader := t.Tunnel.AcquireReader()

	// Filter both for input and output streams
	return NewFilteredInstructionReader(
		NewFilteredInstructionReader(reader, t.inputFilter),
		t.outputFilter,
	)
}
