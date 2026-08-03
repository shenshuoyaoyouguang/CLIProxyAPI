package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

// maxReadableRequestBodyBytes bounds both the raw request body and the
// decompressed output of Content-Encoding processing. Without the bound, a
// small zstd body can expand to gigabytes and OOM the process (compression
// bomb); the raw side also has no default limit in gin.
const maxReadableRequestBodyBytes = 128 << 20 // 128 MiB

// ReadRequestBody reads the incoming request body and decodes supported
// Content-Encoding values before handlers inspect JSON fields.
func ReadRequestBody(c *gin.Context) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxReadableRequestBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxReadableRequestBodyBytes {
		return nil, fmt.Errorf("request body exceeds %d bytes", maxReadableRequestBodyBytes)
	}

	encoding := ""
	if c != nil && c.Request != nil {
		encoding = strings.TrimSpace(c.Request.Header.Get("Content-Encoding"))
	}
	if encoding == "" || strings.EqualFold(encoding, "identity") {
		return raw, nil
	}

	decoded, err := decodeRequestBody(raw, encoding)
	if err != nil {
		if json.Valid(raw) {
			return raw, nil
		}
		return nil, err
	}
	return decoded, nil
}

func decodeRequestBody(raw []byte, encoding string) ([]byte, error) {
	parts := strings.Split(encoding, ",")
	body := raw
	for i := len(parts) - 1; i >= 0; i-- {
		enc := strings.ToLower(strings.TrimSpace(parts[i]))
		switch enc {
		case "", "identity":
			continue
		case "zstd":
			decoded, err := decodeZstdRequestBody(body)
			if err != nil {
				return nil, err
			}
			body = decoded
		default:
			return nil, fmt.Errorf("unsupported request content encoding: %s", enc)
		}
	}
	return body, nil
}

func decodeZstdRequestBody(raw []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd request decoder: %w", err)
	}
	defer decoder.Close()

	decoded, err := io.ReadAll(io.LimitReader(decoder, maxReadableRequestBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to decode zstd request body: %w", err)
	}
	if int64(len(decoded)) > maxReadableRequestBodyBytes {
		return nil, fmt.Errorf("decompressed request body exceeds %d bytes", maxReadableRequestBodyBytes)
	}
	return decoded, nil
}
