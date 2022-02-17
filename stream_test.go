package guac

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestInstructionReader_ReadSome(t *testing.T) {
	conn := &fakeConn{
		ToRead: []byte("4.copy,2.ab;4.copy"),
	}
	stream := NewStream(conn, 1*time.Minute)

	ins, err := stream.ReadSome()

	if err != nil {
		t.Error("Unexpected error", err)
	}
	if !bytes.Equal(ins, []byte("4.copy,2.ab;")) {
		t.Error("Unexpected bytes returned")
	}
	if !stream.Available() {
		t.Error("Stream has more available but returned false")
	}

	// Read the rest of the fragmented instruction
	n := copy(conn.ToRead, ",2.ab;")
	conn.ToRead = conn.ToRead[:n]
	conn.HasRead = false
	ins, err = stream.ReadSome()

	if err != nil {
		t.Error("Unexpected error", err)
	}
	if !bytes.Equal(ins, []byte("4.copy,2.ab;")) {
		t.Error("Unexpected bytes returned")
	}
	if stream.Available() {
		t.Error("Stream thinks it has more available but doesn't")
	}
}

func TestInstructionReader_ReadSome_Unicode(t *testing.T) {
	conn := &fakeConn{
		ToRead: []byte("4.copy,1.🚀;4.copy"),
	}
	stream := NewStream(conn, 1*time.Minute)

	ins, err := stream.ReadSome()

	if err != nil {
		t.Error("Unexpected error", err)
	}
	if !bytes.Equal(ins, []byte("4.copy,1.🚀;")) {
		t.Error("Unexpected bytes returned")
	}
	if !stream.Available() {
		t.Error("Stream has more available but returned false")
	}

	// Read the rest of the fragmented instruction
	n := copy(conn.ToRead, ",1.🚀;")
	conn.ToRead = conn.ToRead[:n]
	conn.HasRead = false
	ins, err = stream.ReadSome()

	if err != nil {
		t.Error("Unexpected error", err)
	}
	if !bytes.Equal(ins, []byte("4.copy,1.🚀;")) {
		t.Error("Unexpected bytes returned")
	}
	if stream.Available() {
		t.Error("Stream thinks it has more available but doesn't")
	}
}

func TestInstructionReader_Flush(t *testing.T) {
	s := NewStream(&fakeConn{}, time.Second)
	s.buffer = s.buffer[:4]
	s.buffer[0] = '1'
	s.buffer[1] = '2'
	s.buffer[2] = '3'
	s.buffer[3] = '4'
	s.buffer = s.buffer[2:]

	s.Flush()

	if s.buffer[0] != '3' && s.buffer[1] != '4' {
		t.Error("Unexpected buffer contents:", string(s.buffer[:2]))
	}
	if len(s.buffer) != 2 {
		t.Error("Unexpected length", len(s.buffer))
	}
}

type fakeConn struct {
	lock    sync.Mutex
	ToRead  []byte
	ToWrite []byte
	HasRead bool
	Closed  bool
}

func (f *fakeConn) Read(b []byte) (n int, err error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.HasRead {
		return 0, io.EOF
	} else {
		f.HasRead = true
		return copy(b, f.ToRead), nil
	}
}

func (f *fakeConn) Write(b []byte) (n int, err error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	f.ToWrite = append(f.ToWrite, b...)
	return len(b), nil
}

func (f *fakeConn) Close() error {
	f.Closed = true
	return nil
}

func (f *fakeConn) LocalAddr() net.Addr {
	return nil
}

func (f *fakeConn) RemoteAddr() net.Addr {
	return nil
}

func (f *fakeConn) SetDeadline(t time.Time) error {
	return nil
}

func (f *fakeConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (f *fakeConn) SetWriteDeadline(t time.Time) error {
	return nil
}
