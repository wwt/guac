package guac

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	t.Run("OKWithUnicode", func(t *testing.T) {
		valid := []byte("4.name,7.rocket🚀;")

		if instr, err := Parse(valid); err != nil {
			t.Fatal(err)
		} else if got, want := len(instr.Args), 1; got != want {
			t.Fatalf("Args=%v, want %v", got, want)
		} else if got, want := instr.Opcode, "name"; got != want {
			t.Fatalf("Opcode=%v, want %v", got, want)
		}
	})

	t.Run("ErrorInvalidLength", func(t *testing.T) {
		invalid := []byte("5.name,7.rocket*;")

		if _, err := Parse(invalid); err == nil {
			t.Fatal("expected error")
		} else if err.Error() != "guac.Parse: wrong pattern instruction" {
			t.Fatalf("unexpected error: %#v", err.Error())
		}
	})

	t.Run("Error", func(t *testing.T) {
		invalid := []byte("4.name")
		if _, err := Parse(invalid); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestInstruction_String(t *testing.T) {
	ins := NewInstruction("select", "hi", "hello", "asdf")
	if ins.String() != "6.select,2.hi,5.hello,4.asdf;" {
		t.Error("Unexpected result:", ins.String())
	}
	if ins.String() != "6.select,2.hi,5.hello,4.asdf;" {
		t.Error("Unexpected result:", ins.String())
	}

	ins = NewInstruction(InternalDataOpcode, "hi", "hello", "asdf")
	if ins.String() != "0.,2.hi,5.hello,4.asdf;" {
		t.Error("Unexpected result:", ins.String())
	}
	if ins.String() != "0.,2.hi,5.hello,4.asdf;" {
		t.Error("Unexpected result:", ins.String())
	}
}

func TestReadOne(t *testing.T) {
	stream := NewStream(&fakeConn{
		ToRead: []byte(`6.select,2.hi,5.hello,4.asdf;6.select,2.hi,5.hello,4.asdf;`),
	}, time.Minute)

	ins, err := ReadOne(stream)
	if err != nil {
		t.Fatal(err)
	}

	if ins.String() != "6.select,2.hi,5.hello,4.asdf;" {
		t.Error("Unexpected", ins.String())
	}
}

var _ Filter = (*dropFilter)(nil)

// dropFilter drops all the instructions defined in drop
type dropFilter struct {
	Drop []string
}

func (f *dropFilter) Filter(i *Instruction) (*Instruction, error) {
	for _, v := range f.Drop {
		if v == i.Opcode {
			return nil, nil
		}
	}

	return i, nil
}

func TestFilteredInstructionReader(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		f := &dropFilter{Drop: []string{"select"}}

		s := NewStream(&fakeConn{
			ToRead: []byte(`6.select,2.hi,5.hello,4.asdf;6.teston,2.hi,5.hello,4.asdf;`),
		}, time.Minute)

		fi := NewFilteredInstructionReader(s, f)

		result, err := fi.ReadSome()
		if err != nil {
			t.Fatal(err)
		}

		if got, want := string(result), "6.teston,2.hi,5.hello,4.asdf;"; got != want {
			t.Fatalf("Result=%v, want %v", got, want)
		}
	})

	// Won't test malformed input, because that's already tested on Stream
}
