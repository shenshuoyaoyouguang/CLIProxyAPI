package cache

import "testing"

// TestHashText_LengthAndStable verifies the xxhash-based key keeps the
// original 16-hex-char length/semantics and is stable per input.
func TestHashText_LengthAndStable(t *testing.T) {
	a := hashText("hello world")
	b := hashText("hello world")
	c := hashText("different input")

	if len(a) != SignatureTextHashLen {
		t.Fatalf("hashText len = %d, want %d", len(a), SignatureTextHashLen)
	}
	if a != b {
		t.Fatalf("hashText not stable for same input: %q != %q", a, b)
	}
	if a == c {
		t.Fatalf("hashText produced identical keys for different inputs: %q", a)
	}
}
