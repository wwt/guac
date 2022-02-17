package guac

import (
	"bytes"
	"testing"
	"time"
)

func TestInputInterceptingFilter(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		conn := &fakeConn{
			ToRead: []byte(""),
		}

		f := NewInputInterceptingFilter(NewUserTunnel(
			NewSimpleTunnel(
				NewStream(conn, time.Minute),
			),
		))

		toInject := []byte("Hello")

		// Hijack stream 1 and inject some data that will need to end up on the wire
		finished := f.InterceptStream("1", bytes.NewReader([]byte(toInject)))

		// base64("Hello") = "SGVsbG8="
		if got, want := string(conn.ToWrite), "4.blob,1.1,8.SGVsbG8=;"; got != want {
			t.Fatalf("On the wire: %v, want=%v", got, want)
		}

		// Simulate an ACK from guacd
		f.Filter(NewInstruction("ack", "1", "", "0"))
		if err := <-finished; err != nil {
			t.Fatal(err)
		}

		if got, want := string(conn.ToWrite), "4.blob,1.1,8.SGVsbG8=;3.end,1.1;"; got != want {
			t.Fatalf("On the wire: %v, want=%v", got, want)
		}
	})
}
