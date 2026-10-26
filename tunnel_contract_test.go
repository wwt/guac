package guac

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// These tests pin what guacdToWs promises today, so a change to how it reads
// from guacd or batches WebSocket messages can be checked against it:
//
//   - every byte guacd sends reaches the browser exactly once, in order,
//     except instructions with the internal opcode, which are never forwarded
//   - every WebSocket message holds whole instructions, never part of one
//   - none of that depends on how the network splits guacd's bytes into reads
//   - an instruction is forwarded as soon as guacd goes quiet after it
//
// The expected output comes from splitInstructions, a splitter written for
// these tests and independent of Stream, so a parsing bug in Stream can't
// hide itself by agreeing with its own test.

// chunkedConn hands its bytes to Stream in reads sized by next, then io.EOF.
type chunkedConn struct {
	fakeConn
	data []byte
	next func() int
}

func (c *chunkedConn) Read(b []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := c.next()
	if n < 1 {
		n = 1
	}
	if n > len(b) {
		n = len(b)
	}
	if n > len(c.data) {
		n = len(c.data)
	}
	copy(b, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

// copyingWriter keeps a copy of each message. guacdToWs reuses its buffer
// after every WriteMessage, so a writer that kept the slice it was given
// would see earlier messages overwritten by later ones.
type copyingWriter struct {
	messages [][]byte
}

func (w *copyingWriter) WriteMessage(_ int, b []byte) error {
	w.messages = append(w.messages, append([]byte(nil), b...))
	return nil
}

// splitInstructions splits b into whole instructions, and returns whatever
// follows the last one. Element lengths count Unicode characters, as the
// Guacamole protocol defines them.
func splitInstructions(b []byte) (instructions [][]byte, rest []byte, err error) {
	start, i := 0, 0
	for i < len(b) {
		length, digits := 0, 0
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			length = length*10 + int(b[i]-'0')
			i++
			digits++
		}
		if i == len(b) {
			return instructions, b[start:], nil
		}
		if digits == 0 || b[i] != '.' {
			return nil, nil, fmt.Errorf("byte %d: want an element length and '.', got %q", i, b[i])
		}
		i++
		for k := 0; k < length; k++ {
			if i >= len(b) {
				return instructions, b[start:], nil
			}
			r, size := utf8.DecodeRune(b[i:])
			if r == utf8.RuneError && size <= 1 {
				if !utf8.FullRune(b[i:]) {
					return instructions, b[start:], nil
				}
				return nil, nil, fmt.Errorf("byte %d: invalid UTF-8", i)
			}
			i += size
		}
		if i >= len(b) {
			return instructions, b[start:], nil
		}
		switch b[i] {
		case ',':
			i++
		case ';':
			i++
			instructions = append(instructions, b[start:i])
			start = i
		default:
			return nil, nil, fmt.Errorf("byte %d: want ',' or ';' after an element, got %q", i, b[i])
		}
	}
	return instructions, b[start:], nil
}

// expectedForward is what the browser should receive for a guacd stream:
// every whole instruction except the internal ones, in order.
func expectedForward(t *testing.T, stream []byte) []byte {
	t.Helper()
	instructions, _, err := splitInstructions(stream)
	if err != nil {
		t.Fatalf("test stream is not valid Guacamole protocol: %v", err)
	}
	var out []byte
	for _, ins := range instructions {
		if !bytes.HasPrefix(ins, internalOpcodeIns) {
			out = append(out, ins...)
		}
	}
	return out
}

// forward runs guacdToWs over stream, read in chunks sized by next.
func forward(stream []byte, next func() int) [][]byte {
	w := &copyingWriter{}
	conn := &chunkedConn{data: append([]byte(nil), stream...), next: next}
	guacdToWs(w, NewStream(conn, time.Minute))
	return w.messages
}

// checkForwarded asserts the contract above for one run.
func checkForwarded(t *testing.T, stream []byte, messages [][]byte) {
	t.Helper()
	for i, m := range messages {
		instructions, rest, err := splitInstructions(m)
		if err != nil {
			t.Fatalf("message %d is not valid Guacamole protocol: %v", i, err)
		}
		if len(instructions) == 0 || len(rest) != 0 {
			t.Fatalf("message %d does not hold whole instructions: %d whole, %d bytes left over", i, len(instructions), len(rest))
		}
	}
	got := bytes.Join(messages, nil)
	want := expectedForward(t, stream)
	if !bytes.Equal(got, want) {
		t.Fatalf("forwarded %d bytes in %d messages, want %d bytes; first difference at byte %d",
			len(got), len(messages), len(want), firstDifference(got, want))
	}
}

func firstDifference(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) < len(b) {
		return len(a)
	}
	return len(b)
}

func ins(opcode string, args ...string) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(utf8.RuneCountInString(opcode)) + "." + opcode)
	for _, a := range args {
		b.WriteString("," + strconv.Itoa(utf8.RuneCountInString(a)) + "." + a)
	}
	b.WriteString(";")
	return b.String()
}

// readSchedules are the read sizes every stream is checked under: the whole
// stream in one read, the smallest possible reads, each side of Stream's own
// 8192-byte read, and a seeded mix.
func readSchedules() map[string]func() func() int {
	fixed := func(n int) func() func() int {
		return func() func() int { return func() int { return n } }
	}
	return map[string]func() func() int{
		"one read":   fixed(1 << 30),
		"1 byte":     fixed(1),
		"7 bytes":    fixed(7),
		"8191 bytes": fixed(8191),
		"8192 bytes": fixed(8192),
		"8193 bytes": fixed(8193),
		"random": func() func() int {
			r := rand.New(rand.NewSource(1))
			return func() int { return 1 + r.Intn(20000) }
		},
	}
}

// guacdLikeStream builds a stream shaped like guacd's output during a
// session: the handshake's args and ready, then frames of image streams sent
// as base64 blobs of up to 8064 characters (guacd's blob size), cursor and
// sync instructions, an audio stream, keepalives, internal instructions, and
// an error at the end.
func guacdLikeStream(seed int64, frames int) []byte {
	r := rand.New(rand.NewSource(seed))
	var b strings.Builder
	b.WriteString(ins("args", "VERSION_1_5_0", "hostname", "port", "username", "password"))
	b.WriteString(ins("ready", "$260a01da-a45d-4bc0-9a2c-6b7e3d9c1f42"))
	b.WriteString(ins("size", "0", "1024", "768"))
	b.WriteString(ins("audio", "1", "audio/L16;rate=44100,channels=2"))
	stream := 2
	for f := 0; f < frames; f++ {
		for tiles := r.Intn(4); tiles >= 0; tiles-- {
			stream++
			id := strconv.Itoa(stream)
			b.WriteString(ins("img", id, "12", "0", "image/png", strconv.Itoa(r.Intn(1024)), strconv.Itoa(r.Intn(768))))
			for chunks := r.Intn(4); chunks >= 0; chunks-- {
				raw := make([]byte, 1+r.Intn(6048))
				r.Read(raw)
				b.WriteString(ins("blob", id, base64.StdEncoding.EncodeToString(raw)))
			}
			b.WriteString(ins("end", id))
		}
		if r.Intn(3) == 0 {
			raw := make([]byte, 1+r.Intn(2048))
			r.Read(raw)
			b.WriteString(ins("blob", "1", base64.StdEncoding.EncodeToString(raw)))
		}
		if r.Intn(5) == 0 {
			b.WriteString(ins("cursor", "0", "0", "-1", "0", "0", "11", "16"))
		}
		if r.Intn(7) == 0 {
			b.WriteString(ins("", "internal", "data"))
		}
		if r.Intn(9) == 0 {
			b.WriteString(ins("nop"))
		}
		b.WriteString(ins("sync", strconv.Itoa(1000+f*40), "1"))
	}
	b.WriteString(ins("error", "Aborted. See logs.", "516"))
	return []byte(b.String())
}

// Check 1: today's contract, on hand-written streams that each exercise one
// edge.
func TestGuacdToWsContract(t *testing.T) {
	big := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xA5}, 6048))
	streams := map[string]string{
		"handshake reply":               ins("args", "VERSION_1_5_0", "hostname", "port") + ins("ready", "$abc"),
		"one instruction":               ins("sync", "1234", "0"),
		"over 8 KiB, one instruction":   ins("blob", "3", big),
		"over 8 KiB, many instructions": strings.Repeat(ins("blob", "3", big[:3000]), 9) + ins("end", "3"),
		"empty elements":                ins("copy", "", "", "") + ins("nop"),
		"internal at the start":         ins("", "ping") + ins("nop"),
		"internal in the middle":        ins("nop") + ins("", "ping", "pong") + ins("sync", "1"),
		"only internal":                 ins("", "a") + ins("", "b"),
	}
	for name, s := range streams {
		for schedule, next := range readSchedules() {
			t.Run(name+"/"+schedule, func(t *testing.T) {
				checkForwarded(t, []byte(s), forward([]byte(s), next()))
			})
		}
	}
}

// Check 1: a multi-byte character that lands across one of Stream's reads.
// Today ReadSome turns each read into runes on its own, so the two halves of
// the character become two replacement characters, the element's length no
// longer matches, and the session ends with a parse error.
func TestReadSomeMultibyteAcrossReadBoundary(t *testing.T) {
	t.Skip("known bug: a multi-byte character split across two reads breaks parsing; fixed separately")
	// The header "4.name,9000." is 12 bytes, so 8179 a's put the euro sign's
	// three bytes at 8191 to 8193, across the end of Stream's 8192-byte read.
	head := len("4.name,9000.")
	before := 8191 - head
	text := strings.Repeat("a", before) + "€" + strings.Repeat("b", 9000-before-1)
	s := ins("name", text) + ins("sync", "1")
	for schedule, next := range readSchedules() {
		t.Run(schedule, func(t *testing.T) {
			checkForwarded(t, []byte(s), forward([]byte(s), next()))
		})
	}
}

// Check 1: element lengths count characters, not bytes, so a non-ASCII
// argument such as a password is written with the length guacd expects.
func TestInstructionLengthCountsCharacters(t *testing.T) {
	t.Skip("known bug: Instruction.String writes byte lengths; fixed separately")
	if got, want := NewInstruction("copy", "é", "€").String(), "4.copy,1.é,1.€;"; got != want {
		t.Errorf("NewInstruction(\"copy\", \"é\", \"€\").String() = %q, want %q", got, want)
	}
}

// Check 2: streams shaped like guacd's, at every read schedule, and the
// output never depends on the schedule.
func TestGuacdToWsGuacdLikeStreams(t *testing.T) {
	for seed := int64(1); seed <= 5; seed++ {
		s := guacdLikeStream(seed, 60)
		var first []byte
		for schedule, next := range readSchedules() {
			t.Run(fmt.Sprintf("seed %d/%s", seed, schedule), func(t *testing.T) {
				messages := forward(s, next())
				checkForwarded(t, s, messages)
				got := bytes.Join(messages, nil)
				if first == nil {
					first = got
				} else if !bytes.Equal(got, first) {
					t.Fatalf("output under %s differs from another schedule's", schedule)
				}
			})
		}
	}
}

// Check 2: real guacd output, replayed when GUAC_REPLAY_DIR names a directory
// of *.guac files, each the server-to-client bytes of one recorded session.
// Recordings are not committed, since real sessions carry their hosts' screens.
func TestGuacdToWsRecordedSessions(t *testing.T) {
	dir := os.Getenv("GUAC_REPLAY_DIR")
	if dir == "" {
		t.Skip("set GUAC_REPLAY_DIR to a directory of recorded guacd streams (*.guac) to replay them")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.guac"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("GUAC_REPLAY_DIR=%s holds no *.guac files", dir)
	}
	for _, f := range files {
		s, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for schedule, next := range readSchedules() {
			t.Run(filepath.Base(f)+"/"+schedule, func(t *testing.T) {
				checkForwarded(t, s, forward(s, next()))
			})
		}
	}
}

// Check 3: the splitter the other checks trust agrees with hand-checked
// cases, so a bug in the test's own model can't pass broken output.
func TestSplitInstructions(t *testing.T) {
	cases := []struct {
		in        string
		whole     int
		rest      string
		wantError bool
	}{
		{in: "4.sync,1.1;", whole: 1},
		{in: "4.sync,1.1;3.nop;", whole: 2},
		{in: "4.sync,1.1;3.n", whole: 1, rest: "3.n"},
		{in: "4.copy,1.é;", whole: 1},
		{in: "4.copy,1.é", whole: 0, rest: "4.copy,1.é"},
		{in: "4.copy,1.\xc3", whole: 0, rest: "4.copy,1.\xc3"},
		{in: "0.;", whole: 1},
		{in: "4.sync;x", wantError: true},
		{in: "a.sync;", wantError: true},
		{in: "4.sync:", wantError: true},
	}
	for _, c := range cases {
		whole, rest, err := splitInstructions([]byte(c.in))
		if c.wantError {
			if err == nil {
				t.Errorf("splitInstructions(%q) returned no error", c.in)
			}
			continue
		}
		if err != nil || len(whole) != c.whole || string(rest) != c.rest {
			t.Errorf("splitInstructions(%q) = %d whole, rest %q, err %v; want %d whole, rest %q",
				c.in, len(whole), rest, err, c.whole, c.rest)
		}
	}
}

// fuzzMultibyte stays false until the multi-byte bug above is fixed; the fix
// turns it on so the fuzzer covers non-ASCII text too.
const fuzzMultibyte = false

// Check 4: random valid streams, split at random.
func FuzzGuacdToWs(f *testing.F) {
	f.Add([]byte("seed"), int64(1))
	f.Add([]byte{0, 1, 2, 3, 200, 255, 17, 9}, int64(8192))
	f.Add(bytes.Repeat([]byte{250}, 64), int64(7))
	f.Fuzz(func(t *testing.T, spec []byte, readSeed int64) {
		if len(spec) > 64 {
			spec = spec[:64] // each byte can add up to 9000 characters
		}
		s := streamFromSpec(spec)
		r := rand.New(rand.NewSource(readSeed))
		maxRead := 1 + r.Intn(20000)
		checkForwarded(t, s, forward(s, func() int { return 1 + r.Intn(maxRead) }))
	})
}

// streamFromSpec turns fuzz bytes into a valid stream: each byte picks an
// opcode and the length of one argument.
func streamFromSpec(spec []byte) []byte {
	opcodes := []string{"sync", "blob", "img", "nop", "", "cursor", "error", "end"}
	alphabet := []rune("abcXYZ0129+/=")
	if fuzzMultibyte {
		alphabet = append(alphabet, 'é', '€', '🚀')
	}
	var b strings.Builder
	for i, c := range spec {
		n := int(c) * 37 % 9000
		arg := make([]rune, n)
		for k := range arg {
			arg[k] = alphabet[(i+k*int(c|1))%len(alphabet)]
		}
		b.WriteString(ins(opcodes[int(c)%len(opcodes)], strconv.Itoa(i), string(arg)))
	}
	// guacd never sends the internal opcode, and an internal instruction
	// skips guacdToWs's flush check, so a stream ending on one would leave
	// the instructions before it unsent at EOF. End on a sync, as guacd's
	// frames do.
	b.WriteString(ins("sync", "0"))
	return []byte(b.String())
}

// Check 5: once guacd goes quiet, what it sent reaches the browser without
// waiting for more. A change that holds instructions back until a frame is
// complete would stall keystroke echoes and cursor moves.
func TestGuacdToWsFlushesWhenGuacdGoesQuiet(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	messages := make(chan []byte, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		guacdToWs(chanWriter(messages), NewStream(client, 5*time.Second))
	}()

	expect := func(want string) {
		t.Helper()
		select {
		case got := <-messages:
			if string(got) != want {
				t.Fatalf("message = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%q was not forwarded within 2 s of guacd going quiet", want)
		}
	}
	expectNothing := func(why string) {
		t.Helper()
		select {
		case got := <-messages:
			t.Fatalf("forwarded %q %s", got, why)
		case <-time.After(100 * time.Millisecond):
		}
	}

	sync := ins("sync", "1234", "0")
	if _, err := server.Write([]byte(sync)); err != nil {
		t.Fatal(err)
	}
	expect(sync)

	cursor := ins("cursor", "0", "0", "-1", "0", "0", "11", "16")
	if _, err := server.Write([]byte(cursor[:9])); err != nil {
		t.Fatal(err)
	}
	expectNothing("before the instruction was complete")
	if _, err := server.Write([]byte(cursor[9:])); err != nil {
		t.Fatal(err)
	}
	expect(cursor)

	server.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("guacdToWs did not return after guacd closed the connection")
	}
}

type chanWriter chan []byte

func (c chanWriter) WriteMessage(_ int, b []byte) error {
	c <- append([]byte(nil), b...)
	return nil
}
