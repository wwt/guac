package guac

import "io"

type InterceptedOutputStream struct {
	Index  string
	Stream io.Writer

	done chan<- error
}

func NewInterceptedOutputStream(index string, stream io.Writer, signal chan<- error) *InterceptedOutputStream {
	return &InterceptedOutputStream{Index: index, Stream: stream, done: signal}
}

type InterceptedInputStream struct {
	Index  string
	Stream io.Reader

	done chan<- error
}

func NewInterceptedInputStream(index string, stream io.Reader, signal chan<- error) *InterceptedInputStream {
	return &InterceptedInputStream{Index: index, Stream: stream, done: signal}
}
