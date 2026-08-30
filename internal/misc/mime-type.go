// Package misc provides miscellaneous utility functions and embedded data for the CLI Proxy API.
// This package contains general-purpose helpers and embedded resources that do not fit into
// more specific domain packages. It includes a curated MIME type mapping for file operations.
package misc

// MimeTypes maps file extensions to their MIME types for the formats this proxy actually
// serves. Keys are lower-case extension without the leading dot, matching the key
// construction in internal/translator/common/file_data.go (filepath.Ext minus the dot).
// Values are byte-for-byte the same as the prior comprehensive table for these keys.
// System MIME tables cover the long tail via mime.TypeByExtension at the call
// site in file_data.go. Re-add entries if a provider starts rejecting the
// system table's value for an extension.
var MimeTypes = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"webp": "image/webp",
	"gif":  "image/gif",
	"bmp":  "image/bmp",
	"pdf":  "application/pdf",
	"mp3":  "audio/mpeg",
	"wav":  "audio/x-wav",
	"mp4":  "video/mp4",
	"txt":  "text/plain",
	"json": "application/json",
	// Long-tail extensions that system MIME tables resolve inconsistently
	// (e.g. mime.TypeByExtension(".md") is empty on Windows) but that
	// providers commonly accept.
	"md":   "text/markdown",
	"csv":  "text/csv",
	"mov":  "video/quicktime",
	"heic": "image/heic",
	"heif": "image/heif",
	"ts":   "video/mp2t",
	"aac":  "audio/aac",
	"flac": "audio/flac",
}
