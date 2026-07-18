package api

import (
	"bufio"
	"fmt"
	"strings"
	"testing"
)

func TestReadRESPBulkString_NormalAndNull(t *testing.T) {
	t.Parallel()

	got, err := readRESPBulkString(bufio.NewReader(strings.NewReader("3\r\nfoo\r\n")))
	if err != nil {
		t.Fatalf("normal bulk: %v", err)
	}
	if got != "foo" {
		t.Fatalf("normal bulk = %q, want foo", got)
	}

	got, err = readRESPBulkString(bufio.NewReader(strings.NewReader("-1\r\n")))
	if err != nil {
		t.Fatalf("null bulk: %v", err)
	}
	if got != "" {
		t.Fatalf("null bulk = %q, want empty", got)
	}
}

func TestReadRESPBulkString_RejectsInvalidNegative(t *testing.T) {
	t.Parallel()

	_, err := readRESPBulkString(bufio.NewReader(strings.NewReader("-2\r\n")))
	if err == nil {
		t.Fatal("expected error for invalid negative bulk length")
	}
	if !strings.Contains(err.Error(), "invalid bulk string length") {
		t.Fatalf("error = %v, want invalid bulk string length", err)
	}
}

func TestReadRESPBulkString_RejectsTooLarge(t *testing.T) {
	t.Parallel()

	// Length exceeds maxRESPBulkSize without sending the payload body.
	line := fmt.Sprintf("%d\r\n", maxRESPBulkSize+1)
	_, err := readRESPBulkString(bufio.NewReader(strings.NewReader(line)))
	if err == nil {
		t.Fatal("expected error for oversized bulk length")
	}
	if !strings.Contains(err.Error(), "bulk string too large") {
		t.Fatalf("error = %v, want bulk string too large", err)
	}
}

func TestReadRESPBulkString_RejectsNonNumericLength(t *testing.T) {
	t.Parallel()

	_, err := readRESPBulkString(bufio.NewReader(strings.NewReader("abc\r\n")))
	if err == nil {
		t.Fatal("expected protocol error for non-numeric length")
	}
}
