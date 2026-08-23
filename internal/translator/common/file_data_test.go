package common

import "testing"

func TestNormalizeOpenAIFileData(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		fallbackMIME string
		fileData     string
		wantMIMEType string
		wantData     string
		wantOK       bool
	}{
		{
			name:         "data URL",
			filename:     "test.pdf",
			fileData:     "data:application/pdf;base64,JVBERi0xLjQK",
			wantMIMEType: "application/pdf",
			wantData:     "JVBERi0xLjQK",
			wantOK:       true,
		},
		{
			name:         "data URL metadata and MIME override",
			filename:     "test.txt",
			fileData:     "data:application/pdf;charset=binary;BASE64,JVBERi0xLjQK",
			wantMIMEType: "application/pdf",
			wantData:     "JVBERi0xLjQK",
			wantOK:       true,
		},
		{
			name:         "case-insensitive data URL scheme",
			filename:     "test.pdf",
			fileData:     "DATA:application/pdf;base64,JVBERi0xLjQK",
			wantMIMEType: "application/pdf",
			wantData:     "JVBERi0xLjQK",
			wantOK:       true,
		},
		{
			name:         "raw base64",
			filename:     "TEST.PDF",
			fileData:     "JVBERi0xLjQK",
			wantMIMEType: "application/pdf",
			wantData:     "JVBERi0xLjQK",
			wantOK:       true,
		},
		{
			name:         "raw base64 with explicit MIME type",
			fallbackMIME: "application/pdf",
			fileData:     "JVBERi0xLjQK",
			wantMIMEType: "application/pdf",
			wantData:     "JVBERi0xLjQK",
			wantOK:       true,
		},
		{
			// Not in misc.MimeTypes; resolved via mime.TypeByExtension, which
			// returns "text/html; charset=utf-8" on some platforms — parameters
			// must be stripped to match the curated table's bare media types.
			name:         "raw base64 with system-table extension",
			filename:     "page.html",
			fileData:     "JVBERi0xLjQK",
			wantMIMEType: "text/html",
			wantData:     "JVBERi0xLjQK",
			wantOK:       true,
		},
		{
			name:         "raw base64 with system-table extension without parameters",
			filename:     "logo.svg",
			fileData:     "JVBERi0xLjQK",
			wantMIMEType: "image/svg+xml",
			wantData:     "JVBERi0xLjQK",
			wantOK:       true,
		},
		{
			// misc.MimeTypes maps wav to audio/x-wav while system tables report
			// audio/wav, so this pins that the curated table wins over the system.
			name:         "raw base64 prefers curated table over system table",
			filename:     "sound.wav",
			fileData:     "JVBERi0xLjQK",
			wantMIMEType: "audio/x-wav",
			wantData:     "JVBERi0xLjQK",
			wantOK:       true,
		},
		{name: "raw base64 with extension unknown to both tables", filename: "file.xyzq", fileData: "JVBERi0xLjQK"},
		{name: "empty data", filename: "test.pdf"},
		{name: "raw base64 without known extension", filename: "test", fileData: "JVBERi0xLjQK"},
		{name: "data URL without base64 marker", filename: "test.pdf", fileData: "data:application/pdf,JVBERi0xLjQK"},
		{name: "data URL without MIME type", filename: "test.pdf", fileData: "data:;base64,JVBERi0xLjQK"},
		{name: "data URL without payload", filename: "test.pdf", fileData: "data:application/pdf;base64,"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mimeType, data, ok := NormalizeOpenAIFileData(test.filename, test.fallbackMIME, test.fileData)
			if mimeType != test.wantMIMEType || data != test.wantData || ok != test.wantOK {
				t.Fatalf("NormalizeOpenAIFileData() = (%q, %q, %v), want (%q, %q, %v)", mimeType, data, ok, test.wantMIMEType, test.wantData, test.wantOK)
			}
		})
	}
}
