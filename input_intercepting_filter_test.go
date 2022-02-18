package guac

import (
	"bytes"
	"encoding/base64"
	"fmt"
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

		firstBlob := bytes.Repeat([]byte("A"), 4096)
		secondBlob := bytes.Repeat([]byte("B"), 100)

		toInject := append(firstBlob, secondBlob...)

		// Hijack stream 1 and inject some data that will need to end up on the wire
		finished := f.InterceptStream("1", bytes.NewReader([]byte(toInject)))

		encoded := base64.StdEncoding.EncodeToString(firstBlob)

		if got, want := string(conn.ToWrite), fmt.Sprintf("4.blob,1.1,%d.%s;", len(encoded), encoded); got != want {
			t.Fatalf("On the wire: %v, want=%v", got, want)
		}

		// Simulate an ACK from guacd
		f.Filter(NewInstruction("ack", "1", "", "0"))

		encoded = base64.StdEncoding.EncodeToString(secondBlob)

		if got, want := string(conn.ToWrite), fmt.Sprintf("4.blob,1.1,%d.%s;", len(encoded), encoded); got != want {
			t.Fatalf("On the wire: %v, want=%v", got, want)
		}

		// Simulate another ACK from guacd, the packet should have been
		// fragmented in two: one which contains the first 4096 bytes
		// base64 encoded, and the second which contains the remaining
		// 100 bytes base64 encoded.
		f.Filter(NewInstruction("ack", "1", "", "0"))

		// There shouldn't be any pending read, so finished should have
		// completed by now, if not that's an error and this test should
		// timeout.
		if err := <-finished; err != nil {
			t.Fatal(err)
		}

		if got, want := string(conn.ToWrite), "3.end,1.1;"; got != want {
			t.Fatalf("On the wire: %v, want=%v", got, want)
		}
	})
}
