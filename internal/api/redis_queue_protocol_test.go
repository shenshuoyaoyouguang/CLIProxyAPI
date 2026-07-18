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

func TestReadRESPArray_RejectsTooLarge(t *testing.T) {
	t.Parallel()

	payload := fmt.Sprintf("*%d\r\n", maxRESPArrayCount+1)
	_, err := readRESPArray(bufio.NewReader(strings.NewReader(payload)))
	if err == nil {
		t.Fatal("expected error for oversized array arity")
	}
	if !strings.Contains(err.Error(), "array too large") {
		t.Fatalf("error = %v, want array too large", err)
	}
}

func TestReadRESPArray_AcceptsNormalCommand(t *testing.T) {
	t.Parallel()

	// *2\r\n$4\r\nAUTH\r\n$3\r\nkey\r\n
	payload := "*2\r\n$4\r\nAUTH\r\n$3\r\nkey\r\n"
	args, err := readRESPArray(bufio.NewReader(strings.NewReader(payload)))
	if err != nil {
		t.Fatalf("readRESPArray: %v", err)
	}
	if len(args) != 2 || args[0] != "AUTH" || args[1] != "key" {
		t.Fatalf("args = %#v, want [AUTH key]", args)
	}
}

func TestParsePopCount_CapsAndRejectsInvalid(t *testing.T) {
	t.Parallel()

	count, hasCount, ok := parsePopCount([]string{"LPOP", "usage", "999999"})
	if !ok || !hasCount || count != maxRedisPopCount {
		t.Fatalf("capped count = %d has=%v ok=%v, want %d true true", count, hasCount, ok, maxRedisPopCount)
	}

	_, hasCount, ok = parsePopCount([]string{"LPOP", "usage", "abc"})
	if ok || !hasCount {
		t.Fatalf("invalid count ok=%v has=%v, want ok=false has=true", ok, hasCount)
	}

	count, hasCount, ok = parsePopCount([]string{"LPOP", "usage"})
	if !ok || hasCount || count != 1 {
		t.Fatalf("default count = %d has=%v ok=%v, want 1 false true", count, hasCount, ok)
	}
}
