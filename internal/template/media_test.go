package template

import (
	htmltemplate "html/template"
	"io"
	"reflect"
	"strings"
	"testing"
)

// This file is the unit-test deliverable for plan.phase-01.task-6: it exercises
// the layout helper constructors in media.go — DictFunc and ListFunc — and the
// helper set LayoutFuncMap exposes. `dict` builds a map[string]any from
// alternating string-keyed arguments and rejects a malformed call (an odd
// argument count, a non-string key such as `dict 1 "v"`, a repeated key)
// rather than returning a partial map; `list` returns a fresh, non-nil []any of
// its arguments; and LayoutFuncMap registers exactly the four helpers
// media/section/dict/list alongside the built-in number/date format functions,
// with the `section` entry a parse-resolvable stub whose unbound execution
// errors. A layout using `{{ section "x" }}` parsing at load — but failing to
// execute until the renderer rebinds the stub — is the load-time contract the
// section-helper feature depends on.
//
// Everything is in-process with a nil mediaFS: LayoutFuncMap's media helper is
// never called here, so no filesystem is touched and the tests run under
// -short.

// TestDictFunc is table-driven over DictFunc's accept and reject paths. A
// malformed call returns (nil, error), never a partial map; a successful call
// always yields a non-nil map whose keys are strings (proved by comparing
// against a map[string]any with reflect.DeepEqual).
func TestDictFunc(t *testing.T) {
	tests := []struct {
		name    string
		kv      []any
		want    map[string]any
		wantErr string // a substring of the error; empty means no error
	}{
		{
			name: "pairs build a string-keyed map",
			kv:   []any{"k1", "v1", "k2", 2},
			want: map[string]any{"k1": "v1", "k2": 2},
		},
		{
			name: "empty call returns a non-nil empty map",
			kv:   nil,
			want: map[string]any{},
		},
		{
			name:    "repeated key is rejected",
			kv:      []any{"k", "a", "k", "b"},
			wantErr: `duplicate key "k"`,
		},
		{
			name:    "odd argument count is rejected",
			kv:      []any{"k", "v", "extra"},
			wantErr: "even number of arguments",
		},
		{
			name:    "non-string key is rejected",
			kv:      []any{1, "v"},
			wantErr: "must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DictFunc(tt.kv...)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("DictFunc() error = %v, want nil", err)
				}
				if got == nil {
					t.Fatal("DictFunc() = nil, want a non-nil map")
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("DictFunc() = %#v, want %#v", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("DictFunc() error = nil, want it to contain %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("DictFunc() error = %q, want it to contain %q", err, tt.wantErr)
			}
			if got != nil {
				t.Errorf("DictFunc() = %#v, want nil on error (no partial map)", got)
			}
		})
	}
}

// TestDictFuncLayoutNonStringKey drives the documented `{{ dict 1 "v" }}`
// syntax through the real layout func map: the call parses, and executing it
// surfaces DictFunc's non-string-key error the way any html/template function
// error fails execution.
func TestDictFuncLayoutNonStringKey(t *testing.T) {
	tmpl, err := htmltemplate.New("dict").Funcs(LayoutFuncMap(nil, "")).Parse(`{{ dict 1 "v" }}`)
	if err != nil {
		t.Fatalf("Parse() error = %v, want the dict call to parse", err)
	}
	err = tmpl.Execute(io.Discard, nil)
	if err == nil {
		t.Fatal("Execute() error = nil, want a non-string-key error")
	}
	if !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("Execute() error = %q, want it to name the non-string key", err)
	}
}

// TestListFunc checks that ListFunc returns its arguments as a []any, that an
// empty call is a non-nil empty slice (so len/range behave), and that it copies
// rather than aliasing the caller's backing array.
func TestListFunc(t *testing.T) {
	t.Run("returns a slice of its arguments", func(t *testing.T) {
		got := ListFunc("a", 1, nil)
		want := []any{"a", 1, nil}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ListFunc() = %#v, want %#v", got, want)
		}
	})

	t.Run("empty call returns a non-nil empty slice", func(t *testing.T) {
		got := ListFunc()
		if got == nil {
			t.Fatal("ListFunc() = nil, want a non-nil slice")
		}
		if len(got) != 0 {
			t.Errorf("ListFunc() = %#v, want empty", got)
		}
	})

	t.Run("returns a fresh slice, not aliasing the input", func(t *testing.T) {
		in := []any{"a", "b"}
		got := ListFunc(in...)
		got[0] = "changed"
		if !reflect.DeepEqual(in, []any{"a", "b"}) {
			t.Errorf("ListFunc() aliased its input: in = %#v, want [a b]", in)
		}
	})
}

// TestLayoutFuncMapHelpers proves LayoutFuncMap registers exactly the four
// documented helpers — media/section/dict/list — and nothing beyond them and
// the built-in number/date format functions. It reads the format function names
// from BuiltinFormats.FuncMap() rather than hardcoding them, so the allowed set
// can never drift from format.go.
func TestLayoutFuncMapHelpers(t *testing.T) {
	fm := LayoutFuncMap(nil, "")

	for _, name := range []string{"media", "section", "dict", "list"} {
		fn, ok := fm[name]
		if !ok {
			t.Errorf("LayoutFuncMap() missing helper %q", name)
			continue
		}
		if fn == nil {
			t.Errorf("LayoutFuncMap()[%q] = nil, want a function", name)
		}
	}

	allowed := map[string]bool{"media": true, "section": true, "dict": true, "list": true}
	for name := range BuiltinFormats.FuncMap() {
		allowed[name] = true
	}
	for name := range fm {
		if !allowed[name] {
			t.Errorf("LayoutFuncMap() has unexpected func %q, want only the four helpers plus the format functions", name)
		}
	}
}

// TestLayoutFuncMapSectionStub proves the section stub makes a layout using
// `{{ section "x" }}` parse at load, and that executing it unbound — before the
// renderer rebinds it — is a clear error rather than a silent empty render.
func TestLayoutFuncMapSectionStub(t *testing.T) {
	tmpl, err := htmltemplate.New("layout").Funcs(LayoutFuncMap(nil, "")).Parse(`{{ section "x" }}`)
	if err != nil {
		t.Fatalf("Parse() error = %v, want a layout using section to parse", err)
	}

	err = tmpl.Execute(io.Discard, nil)
	if err == nil {
		t.Fatal("Execute() error = nil, want the unbound section stub to error")
	}
	if !strings.Contains(err.Error(), "no render-time implementation bound") {
		t.Errorf("Execute() error = %q, want it to name the unbound section stub", err)
	}
}
