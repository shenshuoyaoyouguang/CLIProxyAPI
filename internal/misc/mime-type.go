// Package misc provides miscellaneous utility functions and embedded data for the CLI Proxy API.
// This package contains general-purpose helpers and embedded resources that do not fit into
// more specific domain packages. It includes a curated MIME type mapping for file operations.
package misc

// MimeTypes maps file extensions to their MIME types for the formats this proxy actually
// serves. Keys are lower-case extension without the leading dot, matching the key
// construction in internal/translator/common/file_data.go (filepath.Ext minus the dot).
// System MIME tables cover the long tail via mime.TypeByExtension at the call
// site in file_data.go. Re-add entries if a provider starts rejecting the
// system table's value for an extension.
//
// Values match the prior comprehensive table for the extensions it covered
// (archive formats and audio x- prefixes); the long-tail entries below
// (md/csv/mov/heic/heif/ts/aac/flac) are pinned because the OS tables resolve
// them inconsistently or not at all: on Windows, mime.TypeByExtension returns
// empty for md/iso/dmg/heic/heif, and wrong values for zip (x-zip-compressed),
// 7z (x-compressed), csv (vnd.ms-excel), ts (text/plain) and aac
// (audio/vnd.dlna.adts); slim containers without mime.types return empty for
// everything not in Go's builtin table (only .zip is builtin).
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
	// Archive formats: absent from Go's builtin table and from slim containers,
	// or overridden by the Windows registry.
	"zip":  "application/zip",
	"epub": "application/epub+zip",
	"tar":  "application/x-tar",
	"7z":   "application/x-7z-compressed",
	"iso":  "application/x-iso9660-image",
	"dmg":  "application/x-apple-diskimage",
	// Long-tail extensions that system MIME tables resolve inconsistently
	// (e.g. mime.TypeByExtension(".md") is empty on Windows) but that
	// providers commonly accept.
	"md":   "text/markdown",
	"csv":  "text/csv",
	"mov":  "video/quicktime",
	"heic": "image/heic",
	"heif": "image/heif",
	"ts":   "video/mp2t",
	// Audio formats kept with the x- prefix from the prior table: the Windows
	// registry reports audio/vnd.dlna.adts for aac and modern providers still
	// accept the x- forms used across the ecosystem.
	"aac":  "audio/x-aac",
	"flac": "audio/x-flac",
}
