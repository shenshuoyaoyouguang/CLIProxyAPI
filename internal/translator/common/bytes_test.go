package common

import (
	"bytes"
	"testing"
)

func TestJoinRawArray(t *testing.T) {
	tests := []struct {
		name  string
		items [][]byte
		want  string
	}{
		{name: "empty", want: "[]"},
		{name: "single", items: [][]byte{[]byte(`{"id":1}`)}, want: `[{"id":1}]`},
		{name: "multiple", items: [][]byte{[]byte(`{"id":1}`), []byte(`{"id":2}`)}, want: `[{"id":1},{"id":2}]`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := string(JoinRawArray(test.items)); got != test.want {
				t.Fatalf("JoinRawArray() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestNewRawArrayItems(t *testing.T) {
	if items := NewRawArrayItems(0); items != nil {
		t.Fatalf("NewRawArrayItems(0) = %#v, want nil", items)
	}
	if items := NewRawArrayItems(3); len(items) != 0 || cap(items) != 3 {
		t.Fatalf("NewRawArrayItems(3) len = %d, cap = %d; want len 0, cap 3", len(items), cap(items))
	}
}

func TestSetRawArrayItems(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		path  string
		items [][]byte
		want  string
	}{
		{name: "empty", data: `{"items":[]}`, path: "items", want: `{"items":[]}`},
		{name: "single nested", data: `{"before":1,"request":{"contents":[]},"after":2}`, path: "request.contents", items: [][]byte{[]byte(`{"id":1}`)}, want: `{"before":1,"request":{"contents":[{"id":1}]},"after":2}`},
		{name: "single fallback", data: `{"items":[{"old":1},{"old":2}]}`, path: "items", items: [][]byte{[]byte(`{"id":1}`)}, want: `{"items":[{"id":1}]}`},
		{name: "multiple", data: `{"items":[]}`, path: "items", items: [][]byte{[]byte(`{"id":1}`), []byte(`{"id":2}`)}, want: `{"items":[{"id":1},{"id":2}]}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := SetRawArrayItems([]byte(test.data), test.path, test.items)
			if string(got) != test.want {
				t.Fatalf("SetRawArrayItems() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestSSEDataPayload(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		want   string
		wantOK bool
	}{
		{name: "data with space", line: "data: {\"foo\":1}", want: `{"foo":1}`, wantOK: true},
		{name: "data without space", line: "data:{\"foo\":1}", want: `{"foo":1}`, wantOK: true},
		{name: "data with leading whitespace", line: "  data: hello", want: "hello", wantOK: true},
		{name: "data with trailing whitespace", line: "data: hello  ", want: "hello", wantOK: true},
		{name: "data empty payload", line: "data:", want: "", wantOK: true},
		{name: "data empty payload with space", line: "data: ", want: "", wantOK: true},
		{name: "event line rejected", line: "event: foo", wantOK: false},
		{name: "empty line rejected", line: "", wantOK: false},
		{name: "not data prefix", line: ": comment", wantOK: false},
		{name: "substring prefix rejected", line: "mydata: foo", wantOK: false},
		{name: "data multiple spaces", line: "data:   foo", want: "foo", wantOK: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := SSEDataPayload([]byte(test.line))
			if ok != test.wantOK {
				t.Fatalf("SSEDataPayload(%q) ok = %v, want %v", test.line, ok, test.wantOK)
			}
			if !test.wantOK {
				return
			}
			if !bytes.Equal(got, []byte(test.want)) {
				t.Fatalf("SSEDataPayload(%q) = %q, want %q", test.line, string(got), test.want)
			}
		})
	}
}
