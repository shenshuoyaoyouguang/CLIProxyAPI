package store

import "bytes"

// normalizeLineEndingsBytes normalizes CRLF and lone CR line endings to LF.
// Shared by the postgres and object store backends for config payloads written
// to disk or persisted to the database.
func normalizeLineEndingsBytes(data []byte) []byte {
	replaced := bytes.ReplaceAll(data, []byte{'\r', '\n'}, []byte{'\n'})
	return bytes.ReplaceAll(replaced, []byte{'\r'}, []byte{'\n'})
}

func normalizeLineEndings(s string) string {
	return string(normalizeLineEndingsBytes([]byte(s)))
}
