package logging

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func compressForLogTest(t *testing.T, encoding string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	switch encoding {
	case "gzip":
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	case "deflate":
		w, err := flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	case "br":
		w := brotli.NewWriter(&buf)
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	case "zstd":
		w, err := zstd.NewWriter(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown encoding %q", encoding)
	}
	return buf.Bytes()
}

// TestDecompressResponseBombTruncated verifies that a decompression bomb in a
// response body is capped at maxDecompressedResponseBytesForLog for every
// supported encoding (C1 sibling regression: response-log path).
func TestDecompressResponseBombTruncated(t *testing.T) {
	logger := NewFileRequestLogger(true, t.TempDir(), "", 0)
	payload := bytes.Repeat([]byte("A"), 4<<20) // 4 MiB bomb
	for _, enc := range []string{"gzip", "deflate", "br", "zstd"} {
		t.Run(enc, func(t *testing.T) {
			compressed := compressForLogTest(t, enc, payload)
			decoded, truncated, err := logger.decompressResponse(
				map[string][]string{"Content-Encoding": {enc}}, compressed,
			)
			if err != nil {
				t.Fatalf("decompressResponse(%s): %v", enc, err)
			}
			if !truncated {
				t.Fatalf("decompressResponse(%s): expected truncated", enc)
			}
			if int64(len(decoded)) != maxDecompressedResponseBytesForLog {
				t.Fatalf("decompressResponse(%s): got %d bytes, want cap %d", enc, len(decoded), maxDecompressedResponseBytesForLog)
			}
		})
	}
}

// TestDecompressResponseRoundTrip verifies small bodies decode unchanged.
func TestDecompressResponseRoundTrip(t *testing.T) {
	logger := NewFileRequestLogger(true, t.TempDir(), "", 0)
	payload := []byte("hello world, this is a small response body")
	for _, enc := range []string{"gzip", "deflate", "br", "zstd"} {
		t.Run(enc, func(t *testing.T) {
			compressed := compressForLogTest(t, enc, payload)
			decoded, truncated, err := logger.decompressResponse(
				map[string][]string{"Content-Encoding": {enc}}, compressed,
			)
			if err != nil {
				t.Fatalf("decompressResponse(%s): %v", enc, err)
			}
			if truncated {
				t.Fatalf("decompressResponse(%s): unexpected truncation", enc)
			}
			if !bytes.Equal(decoded, payload) {
				t.Fatalf("decompressResponse(%s): round-trip mismatch", enc)
			}
		})
	}
}

// TestLogRequestBombResponseMarkedTruncated verifies the truncation marker
// reaches the written log file through the full LogRequest path.
func TestLogRequestBombResponseMarkedTruncated(t *testing.T) {
	dir := t.TempDir()
	logger := NewFileRequestLogger(true, dir, "", 0)
	payload := bytes.Repeat([]byte("B"), 4<<20) // 4 MiB bomb
	compressed := compressForLogTest(t, "gzip", payload)

	err := logger.LogRequest(
		"https://example.com/v1/responses",
		"POST",
		map[string][]string{"Content-Type": {"application/json"}},
		[]byte(`{"model":"x"}`),
		200,
		map[string][]string{"Content-Encoding": {"gzip"}},
		compressed, nil, nil, nil, nil, nil, "",
		time.Now(), time.Now(),
	)
	if err != nil {
		t.Fatalf("LogRequest: %v", err)
	}

	files, errGlob := filepath.Glob(filepath.Join(dir, "*.log"))
	if errGlob != nil {
		t.Fatal(errGlob)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(files))
	}
	content, errRead := os.ReadFile(files[0])
	if errRead != nil {
		t.Fatal(errRead)
	}
	if !bytes.Contains(content, []byte("TRUNCATED")) {
		t.Fatalf("log file missing truncation marker: %s", files[0])
	}
}
