package common

import (
	"mime"
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
)

// bareMediaType strips parameters (e.g. "; charset=utf-8") that system MIME
// tables append, so fallback types match the curated table's bare media types.
func bareMediaType(mediaType string) string {
	bare, _, _ := strings.Cut(mediaType, ";")
	return strings.TrimSpace(bare)
}

// NormalizeOpenAIFileData returns the MIME type and raw base64 payload for OpenAI file content.
func NormalizeOpenAIFileData(filename, fallbackMIMEType, fileData string) (mimeType, data string, ok bool) {
	if fileData == "" {
		return "", "", false
	}

	if fallbackMIMEType == "" {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
		fallbackMIMEType = misc.MimeTypes[ext]
		if fallbackMIMEType == "" && ext != "" {
			fallbackMIMEType = bareMediaType(mime.TypeByExtension("." + ext))
		}
	}
	const dataURLPrefix = "data:"
	if len(fileData) < len(dataURLPrefix) || !strings.EqualFold(fileData[:len(dataURLPrefix)], dataURLPrefix) {
		if fallbackMIMEType == "" {
			return "", "", false
		}
		return fallbackMIMEType, fileData, true
	}

	metadata, payload, found := strings.Cut(fileData[len(dataURLPrefix):], ",")
	if !found || payload == "" {
		return "", "", false
	}
	fields := strings.Split(metadata, ";")
	mimeType = strings.TrimSpace(fields[0])
	if mimeType == "" {
		return "", "", false
	}
	for _, field := range fields[1:] {
		if strings.EqualFold(strings.TrimSpace(field), "base64") {
			return mimeType, payload, true
		}
	}
	return "", "", false
}

// SplitBase64DataURL parses a "data:<mime>;base64,<data>" URL into its mime
// type and payload. It reports ok=false for anything that is not a data URL
// with a base64 payload.
func SplitBase64DataURL(dataURL string) (mimeType, data string, ok bool) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", "", false
	}
	pieces := strings.SplitN(dataURL[5:], ";", 2)
	if len(pieces) != 2 || !strings.HasPrefix(pieces[1], "base64,") {
		return "", "", false
	}
	return pieces[0], pieces[1][len("base64,"):], true
}
