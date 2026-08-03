package executor

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestReadSSELineCapsOversizedLine verifies a pathological line longer than
// maxSSELineBytes fails the stream explicitly instead of accumulating the whole
// stream in memory, and that the reader position stays aligned afterwards.
func TestReadSSELineCapsOversizedLine(t *testing.T) {
	oversized := strings.Repeat("x", maxSSELineBytes+1024) + "\n"
	reader := bufio.NewReaderSize(strings.NewReader(oversized+"data: ok\n"), 64<<10)

	line, err := readSSELine(reader)
	if err == nil {
		t.Fatal("readSSELine() error = nil, want cap error")
	}
	if !errors.Is(err, errSSELineTooLong) {
		t.Fatalf("readSSELine() error = %v, want errSSELineTooLong", err)
	}
	if len(line) <= maxSSELineBytes {
		t.Fatalf("returned line length = %d, want > %d", len(line), maxSSELineBytes)
	}

	// The oversized line was drained; the following line reads normally.
	next, errNext := readSSELine(reader)
	if errNext != nil {
		t.Fatalf("readSSELine() after cap error = %v", errNext)
	}
	if string(next) != "data: ok\n" {
		t.Fatalf("next line = %q, want %q", string(next), "data: ok\n")
	}
}

// TestReadSSELineSplitsLongLinesAcrossFragments verifies lines larger than the
// bufio buffer are reassembled correctly.
func TestReadSSELineSplitsLongLinesAcrossFragments(t *testing.T) {
	longLine := strings.Repeat("y", 200<<10) + "\n" // 200 KiB > 64 KiB buffer
	reader := bufio.NewReaderSize(strings.NewReader(longLine+"tail\n"), 64<<10)

	line, err := readSSELine(reader)
	if err != nil {
		t.Fatalf("readSSELine() error = %v", err)
	}
	if string(line) != longLine {
		t.Fatalf("reassembled line length = %d, want %d", len(line), len(longLine))
	}

	next, errNext := readSSELine(reader)
	if errNext != nil {
		t.Fatalf("readSSELine() tail error = %v", errNext)
	}
	if string(next) != "tail\n" {
		t.Fatalf("tail line = %q, want %q", string(next), "tail\n")
	}
}

// TestReadSSELineTrailingPartialAtEOF verifies a final line without a newline
// is returned together with io.EOF, mirroring bufio.Reader.ReadBytes.
func TestReadSSELineTrailingPartialAtEOF(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader("data: [DONE]"), 64<<10)

	line, err := readSSELine(reader)
	if err != io.EOF {
		t.Fatalf("readSSELine() error = %v, want io.EOF", err)
	}
	if string(line) != "data: [DONE]" {
		t.Fatalf("line = %q, want %q", string(line), "data: [DONE]")
	}
}
