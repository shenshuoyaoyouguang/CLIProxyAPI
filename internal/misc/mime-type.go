// Package misc provides miscellaneous utility functions and embedded data for the CLI Proxy API.
// This package contains general-purpose helpers and embedded resources that do not fit into
// more specific domain packages. It includes a curated MIME type mapping for file operations.
package misc

// MimeTypes maps file extensions to their MIME types for the formats this proxy actually
// serves. Keys are lower-case extension without the leading dot, matching the key
// construction in internal/translator/common/file_data.go (filepath.Ext minus the dot).
// System MIME tables cover the long tail via mime.TypeByExtension at the call site's
// discretion. Re-add entries if a provider starts rejecting a specific extension.
var MimeTypes = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"webp": "image/webp",
	"gif":  "image/gif",
	"bmp":  "image/bmp",
	"pdf":  "application/pdf",
	"mp3":  "audio/mpeg",
	"wav":  "audio/wav",
	"mp4":  "video/mp4",
	"txt":  "text/plain; charset=utf-8",
	"json": "application/json",
	"yaml": "application/yaml",
	"yml":  "application/yaml",
}
