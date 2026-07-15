package store

import (
	"path/filepath"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestResolveManagedPath(t *testing.T) {
	root := t.TempDir()
	baseDir := filepath.Join(root, "auths")

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "relative file", input: "openai.json"},
		{name: "relative nested file", input: filepath.Join("team", "openai.json")},
		{name: "absolute inside base", input: filepath.Join(baseDir, "inside.json")},
		{name: "parent traversal", input: filepath.Join("..", "outside.json"), wantErr: true},
		{name: "absolute outside base", input: filepath.Join(root, "outside.json"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveManagedPath(baseDir, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveManagedPath(%q) succeeded with %q, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveManagedPath(%q) error = %v", tt.input, err)
			}
			if _, err := filepath.Rel(baseDir, got); err != nil {
				t.Fatalf("resolved path %q is not relative to %q: %v", got, baseDir, err)
			}
		})
	}
}

func TestStoreBackendsRejectExternalAuthPaths(t *testing.T) {
	root := t.TempDir()
	authDir := filepath.Join(root, "auths")
	outsidePath := filepath.Join(root, "outside.json")

	t.Run("postgres save path", func(t *testing.T) {
		store := &PostgresStore{authDir: authDir}
		_, err := store.resolveAuthPath(&cliproxyauth.Auth{ID: "outside", FileName: outsidePath})
		if err == nil {
			t.Fatal("PostgresStore.resolveAuthPath external absolute path succeeded, want error")
		}
	})

	t.Run("postgres delete path", func(t *testing.T) {
		store := &PostgresStore{authDir: authDir}
		if _, err := store.resolveDeletePath(outsidePath); err == nil {
			t.Fatal("PostgresStore.resolveDeletePath external absolute path succeeded, want error")
		}
	})

	t.Run("object save path", func(t *testing.T) {
		store := &ObjectTokenStore{authDir: authDir}
		_, err := store.resolveAuthPath(&cliproxyauth.Auth{ID: "outside", FileName: outsidePath})
		if err == nil {
			t.Fatal("ObjectTokenStore.resolveAuthPath external absolute path succeeded, want error")
		}
	})

	t.Run("object delete path", func(t *testing.T) {
		store := &ObjectTokenStore{authDir: authDir}
		if _, err := store.resolveDeletePath(outsidePath); err == nil {
			t.Fatal("ObjectTokenStore.resolveDeletePath external absolute path succeeded, want error")
		}
	})
}

func TestJsonEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []byte
		want bool
	}{
		{
			name: "equivalent JSON different key ordering",
			a:    []byte(`{"name":"alice","age":30}`),
			b:    []byte(`{"age":30,"name":"alice"}`),
			want: true,
		},
		{
			name: "different values",
			a:    []byte(`{"name":"alice"}`),
			b:    []byte(`{"name":"bob"}`),
			want: false,
		},
		{
			name: "invalid JSON a",
			a:    []byte(`{invalid`),
			b:    []byte(`{"a":1}`),
			want: false,
		},
		{
			name: "invalid JSON b",
			a:    []byte(`{"a":1}`),
			b:    []byte(`not json`),
			want: false,
		},
		{
			name: "nested objects equal",
			a:    []byte(`{"outer":{"inner":1,"arr":[1,2]}}`),
			b:    []byte(`{"outer":{"arr":[1,2],"inner":1}}`),
			want: true,
		},
		{
			name: "arrays with same elements",
			a:    []byte(`[1,2,3]`),
			b:    []byte(`[1,2,3]`),
			want: true,
		},
		{
			name: "arrays with different order",
			a:    []byte(`[1,2,3]`),
			b:    []byte(`[3,2,1]`),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jsonEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("jsonEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeepEqualJSON(t *testing.T) {
	tests := []struct {
		name string
		a, b any
		want bool
	}{
		{
			name: "maps equal",
			a:    map[string]any{"k": "v", "n": float64(1)},
			b:    map[string]any{"n": float64(1), "k": "v"},
			want: true,
		},
		{
			name: "maps different keys",
			a:    map[string]any{"k": "v"},
			b:    map[string]any{"x": "v"},
			want: false,
		},
		{
			name: "maps different values",
			a:    map[string]any{"k": "v"},
			b:    map[string]any{"k": "w"},
			want: false,
		},
		{
			name: "slices equal",
			a:    []any{float64(1), "two", true},
			b:    []any{float64(1), "two", true},
			want: true,
		},
		{
			name: "slices different length",
			a:    []any{float64(1)},
			b:    []any{float64(1), float64(2)},
			want: false,
		},
		{
			name: "slices different values",
			a:    []any{"a", "b"},
			b:    []any{"a", "c"},
			want: false,
		},
		{
			name: "float64 equal",
			a:    float64(3.14),
			b:    float64(3.14),
			want: true,
		},
		{
			name: "float64 not equal",
			a:    float64(1.0),
			b:    float64(2.0),
			want: false,
		},
		{
			name: "string equal",
			a:    "hello",
			b:    "hello",
			want: true,
		},
		{
			name: "string not equal",
			a:    "hello",
			b:    "world",
			want: false,
		},
		{
			name: "bool equal",
			a:    true,
			b:    true,
			want: true,
		},
		{
			name: "bool not equal",
			a:    true,
			b:    false,
			want: false,
		},
		{
			name: "nil vs nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "type mismatch string vs float",
			a:    "1",
			b:    float64(1),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deepEqualJSON(tt.a, tt.b); got != tt.want {
				t.Errorf("deepEqualJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeLineEndings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: ""},
		{name: "crlf to lf", input: "a\r\nb", want: "a\nb"},
		{name: "cr to lf", input: "a\rb", want: "a\nb"},
		{name: "lf unchanged", input: "a\nb", want: "a\nb"},
		{name: "mixed endings", input: "a\r\nb\rc\nd", want: "a\nb\nc\nd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLineEndings(tt.input); got != tt.want {
				t.Errorf("normalizeLineEndings() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeAuthID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "forward slashes unchanged", input: "foo/bar", want: "foo/bar"},
		{name: "backslash to slash", input: "foo\\bar", want: "foo/bar"},
		{name: "double slash cleaned", input: "foo//bar", want: "foo/bar"},
		{name: "dot prefix cleaned", input: "./foo/bar", want: "foo/bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAuthID(tt.input)
			// Reference behavior: filepath.ToSlash(filepath.Clean(input))
			ref := filepath.ToSlash(filepath.Clean(tt.input))
			if got != ref {
				t.Errorf("normalizeAuthID(%q) = %q, reference = %q", tt.input, got, ref)
			}
			if got != tt.want {
				t.Errorf("normalizeAuthID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

type testStringer struct{ val string }

func (s testStringer) String() string { return s.val }

func TestValueAsString(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{name: "string value", v: "hello", want: "hello"},
		{name: "nil", v: nil, want: ""},
		{name: "int", v: 42, want: ""},
		{name: "fmt.Stringer", v: testStringer{val: "stringer-result"}, want: "stringer-result"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := valueAsString(tt.v); got != tt.want {
				t.Errorf("valueAsString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLabelFor(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
		want     string
	}{
		{name: "nil metadata", metadata: nil, want: ""},
		{name: "has label", metadata: map[string]any{"label": "my-label"}, want: "my-label"},
		{name: "no label but email", metadata: map[string]any{"email": "a@b.com"}, want: "a@b.com"},
		{name: "only project_id", metadata: map[string]any{"project_id": "proj-1"}, want: "proj-1"},
		{name: "all empty strings", metadata: map[string]any{"label": "", "email": "", "project_id": ""}, want: ""},
		{name: "label takes priority", metadata: map[string]any{"label": "L", "email": "E"}, want: "L"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := labelFor(tt.metadata); got != tt.want {
				t.Errorf("labelFor() = %q, want %q", got, tt.want)
			}
		})
	}
}
