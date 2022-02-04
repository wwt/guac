package guac

import "io"

type InterceptedOutputStream struct {
	Index  string
	Stream io.Writer
	Error  error
	Closed chan bool
}

func NewInterceptedOutputStream(index string, stream io.Writer) *InterceptedOutputStream {
	return &InterceptedOutputStream{Index: index, Stream: stream, Closed: make(chan bool, 1)}
}

type InterceptedInputStream struct {
	Index  string
	Stream io.Reader
	Error  error
	Closed chan bool
}

func NewInterceptedInputStream(index string, stream io.Reader) *InterceptedInputStream {
	return &InterceptedInputStream{Index: index, Stream: stream, Closed: make(chan bool, 1)}
}
