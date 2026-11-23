package extracted

import (
	"errors"
	"testing"
)

func mustImport(t *testing.T, input *Input) Value {
	t.Helper()
	v, err := Import(input, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCodecOperations(t *testing.T) {
	limits := DefaultLimits()
	literal, err := RunLiteral(limits)
	if err != nil || literal != 65535 {
		t.Fatal("literal", literal, err)
	}
	boundary := []uint16{0, 1, 7, 8, 15, 16, 17, 31, 32, 255, 256, 32767, 32768, 0xabcd, 65534, 65535}
	for _, a := range boundary {
		input := &Input{Type: "UInt16", UInt16: a}
		left := mustImport(t, input)
		input.UInt16++
		identity, err := RunRoot("Example.id", []Value{left}, limits)
		got, ok := identity.UInt16()
		if !ok || got != a || err != nil || identity.Type() != "UInt16" || identity.Constructor() != "" || len(identity.Fields()) != 0 {
			t.Fatal("raw identity", a, got, err)
		}
		if got, err := RunId(a, limits); got != a || err != nil {
			t.Fatal("typed identity", a, got, err)
		}
		low, err := RunLow(a, limits)
		if err != nil || uint16(low) != a%256 {
			t.Fatal("narrow", a, low, err)
		}
		wide, err := RunWide(low, limits)
		if err != nil || wide != a%256 {
			t.Fatal("widen", low, wide, err)
		}
		raw, err := RunRoot("Example.low", []Value{left}, limits)
		if got, ok := raw.UInt8(); !ok || got != low || err != nil {
			t.Fatal("raw narrowing", got, err)
		}
		raw, err = RunRoot("Example.wide", []Value{raw}, limits)
		if got, ok := raw.UInt16(); !ok || got != wide || err != nil {
			t.Fatal("raw widening", got, err)
		}
		for _, b := range boundary {
			right := mustImport(t, &Input{Type: "UInt16", UInt16: b})
			power := uint32(1) << (b % 16)
			for _, op := range []struct {
				root string
				run  func(uint16, uint16, Limits) (uint16, error)
				want uint16
			}{
				{"left", RunLeft, uint16(uint32(a) * power % 65536)},
				{"right", RunRight, uint16(uint32(a) / power)},
				{"or", RunOr, a | b},
			} {
				out, rawErr := RunRoot("Example."+op.root, []Value{left, right}, limits)
				raw, ok := out.UInt16()
				typed, typedErr := op.run(a, b, limits)
				if !ok || rawErr != nil || typedErr != nil || raw != op.want || typed != op.want {
					t.Fatalf("%s(%d,%d): raw %d (%v), typed %d (%v), want %d", op.root, a, b, raw, rawErr, typed, typedErr, op.want)
				}
			}
		}
	}
}

func TestRecordsAndMalformedPayloads(t *testing.T) {
	limits := DefaultLimits()
	sample, err := NewSample(65535, true, limits)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunRecord(sample, limits)
	if word, ok := out.Word(); !ok || word != 65535 || err != nil {
		t.Fatal(word, err)
	}
	for _, input := range []*Input{
		{Type: "UInt16", Constructor: "UInt16.mk"}, {Type: "UInt16", Fields: []*Input{{Type: "UInt16"}}},
		{Type: "UInt16", UInt8: 1}, {Type: "UInt16", UInt32: 1}, {Type: "UInt16", UInt64: 1},
		{Type: "UInt8", UInt16: 1}, {Type: "UInt32", UInt16: 1}, {Type: "UInt64", UInt16: 1},
		{Type: "Bool", Constructor: "Bool.false", UInt16: 1},
		{Type: "Example.Sample", Constructor: "Example.Sample.mk", UInt16: 1, Fields: []*Input{{Type: "UInt16"}, {Type: "Bool", Constructor: "Bool.true"}}},
	} {
		if value, err := Import(input, limits); !errors.Is(err, ErrMalformed) || value != (Value{}) {
			t.Fatal("malformed import", input, value, err)
		}
	}
	for _, corrupt := range []func(*node){
		func(n *node) { n.scalar8 = 1 }, func(n *node) { n.scalar = 1 }, func(n *node) { n.scalar64 = 1 },
		func(n *node) { n.tag = 1 }, func(n *node) { n.fields = []*node{n} },
		func(n *node) { n.size = 2 }, func(n *node) { n.depth = 2 },
	} {
		n := apiUInt16Node(42)
		corrupt(n)
		if _, ok := (Value{n}).UInt16(); ok {
			t.Fatal("malformed scalar", n)
		}
	}
	for _, n := range []*node{apiUInt8Node(1), apiUInt32Node(1), apiUInt64Node(1), apiBoolNode(true)} {
		n.scalar16 = 1
		v := Value{n}
		_, ok8 := v.UInt8()
		_, ok16 := v.UInt16()
		_, ok32 := v.UInt32()
		_, ok64 := v.UInt64()
		_, okBool := apiReadBool(n)
		if ok8 || ok16 || ok32 || ok64 || okBool {
			t.Fatal("inactive UInt16", n)
		}
	}
	out.v.n.scalar16 = 1
	if _, ok := out.Word(); ok {
		t.Fatal("inactive record payload")
	}
	if _, ok := (Value{}).UInt16(); ok {
		t.Fatal("zero value")
	}
}

func TestUInt16Budgets(t *testing.T) {
	l := DefaultLimits()
	l.MaxAllocations, l.MaxSteps, l.MaxNodes, l.MaxSlots, l.MaxOutputNodes = 1, 2, 2, 3, 1
	if out, err := RunLeft(1, 15, l); out != 32768 || err != nil {
		t.Fatal("exact budget", out, err)
	}
	v := mustImport(t, &Input{Type: "UInt16", UInt16: 1})
	for _, reduce := range []func(*Limits){
		func(l *Limits) { l.MaxNodes-- }, func(l *Limits) { l.MaxSteps-- }, func(l *Limits) { l.MaxSlots-- },
	} {
		tiny := l
		reduce(&tiny)
		if out, err := RunLeft(1, 15, tiny); out != 0 || !errors.Is(err, ErrResource) {
			t.Fatal("typed budget", tiny, out, err)
		}
		if out, err := RunRoot("Example.left", []Value{v, v}, tiny); out != (Value{}) || !errors.Is(err, ErrResource) {
			t.Fatal("raw budget", tiny, out, err)
		}
	}
	l.MaxNodes = 1
	if out, err := NewSample(0, false, l); out != (Sample{}) || !errors.Is(err, ErrResource) {
		t.Fatal("record input budget", out, err)
	}
	l.MaxNodes = 3
	sample, err := NewSample(0, false, l)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := RunRecord(sample, l); out != (Sample{}) || !errors.Is(err, ErrResource) {
		t.Fatal("record output budget", out, err)
	}
}
