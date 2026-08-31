package origins

import "testing"

func TestB64RoundTrip(t *testing.T) {
	for _, value := range []int{0, 1, 202, 4095, 262143} {
		encoded, err := EncodeB64Int(value, 3)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeB64Int(encoded)
		if err != nil || decoded != value {
			t.Fatalf("%d virou %d (%v)", value, decoded, err)
		}
	}
}

func TestPlainServerBuffer(t *testing.T) {
	b := &PlainServerBuffer{}
	b.Push([]byte{'@', '@', 1, '@'})
	b.Push([]byte{'A', 1})
	first, ok := b.Next()
	if !ok || string(first) != "@@" {
		t.Fatalf("primeiro=%q", first)
	}
	second, ok := b.Next()
	if !ok || string(second) != "@A" {
		t.Fatalf("segundo=%q", second)
	}
}
