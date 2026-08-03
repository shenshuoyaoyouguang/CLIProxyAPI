package handlers

import "testing"

func TestValidateSSEDataJSON(t *testing.T) {
	tests := []struct {
		name    string
		chunk   string
		wantErr bool
		// final marks a chunk whose buffered partial line would fail finalize if
		// the stream ended without a continuation (mid-stream it must pass).
		final bool
	}{
		{
			name:    "valid framed chunks",
			chunk:   "data: {\"id\":\"evt_1\"}\n\ndata: {\"id\":\"evt_2\"}\n\n",
			wantErr: false,
		},
		{
			name:    "done marker",
			chunk:   "data: [DONE]\n\n",
			wantErr: false,
		},
		{
			name:    "empty data payload",
			chunk:   "data: \n\n",
			wantErr: false,
		},
		{
			name:    "garbage data payload",
			chunk:   "data: not-json\n\n",
			wantErr: true,
		},
		{
			name:    "boundary split frame deferral",
			chunk:   "data: {\"id\":\"evt_",
			wantErr: false,
			final:   true,
		},
		{
			name:    "crlf framed",
			chunk:   "data: {\"id\":\"evt_1\"}\r\n\r\n",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pending []byte
			err := validateSSEDataJSON(&pending, []byte(tt.chunk))
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSSEDataJSON(%q) error = %v, wantErr %v", tt.chunk, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.final {
				if errFinal := finalizeSSEDataJSON(pending); errFinal == nil {
					t.Fatalf("finalizeSSEDataJSON(%q) = nil, want truncated-line error", pending)
				}
				return
			}
			if errFinal := finalizeSSEDataJSON(pending); errFinal != nil {
				t.Fatalf("finalizeSSEDataJSON(%q) error = %v, want nil", pending, errFinal)
			}
		})
	}
}

func TestValidateSSEDataJSONReassemblesBoundarySplitFrame(t *testing.T) {
	var pending []byte
	if err := validateSSEDataJSON(&pending, []byte("event: response.completed\ndata: {\"id\":\"evt_")); err != nil {
		t.Fatalf("first half error = %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("expected pending partial line after first half")
	}
	// The continuation completes the frame; the reassembled line is valid JSON.
	if err := validateSSEDataJSON(&pending, []byte("123\"}\n\n")); err != nil {
		t.Fatalf("continuation error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %q, want empty", pending)
	}
}

func TestFinalizeSSEDataJSONRejectsTruncatedTrailingData(t *testing.T) {
	var pending []byte
	if err := validateSSEDataJSON(&pending, []byte("event: response.completed\ndata: {\"type\"")); err != nil {
		t.Fatalf("chunk error = %v", err)
	}
	if err := finalizeSSEDataJSON(pending); err == nil {
		t.Fatal("expected truncated trailing data line to fail at stream end")
	}
}
