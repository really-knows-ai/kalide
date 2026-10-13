package template

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for expandVariantsFrom (the
// enum-variants-from expansion) and for the way its result feeds the schema
// checker's enum membership check and closest-match suggestion. It pins:
//
//   - the glob resolves against the owning layer's root filesystem, so the
//     layer root has "media" as a direct entry;
//   - only files under that layer's media/ tree become variants, written
//     media-relative (the media/ prefix dropped) and sorted;
//   - a directory match is not a variant, a match outside media/ is ignored,
//     and an absolute or parent-escaping glob is an error;
//   - the expanded set is the field's effective Variants, so validateEnum
//     accepts a variant and reports a non-variant with "did you mean …?".
//
// Fixtures are testing/fstest.MapFS: in-process, deterministic, no real
// filesystem.

// enumLayerRootFS is a library-layer root as expandVariantsFrom expects it,
// with media/ a direct entry alongside the other layer trees.
func enumLayerRootFS() fstest.MapFS {
	return fstest.MapFS{
		"media/bgs/a.jpg":        &fstest.MapFile{Data: []byte("a")},
		"media/bgs/b.jpg":        &fstest.MapFile{Data: []byte("b")},
		"media/bgs/right-8.jpg":  &fstest.MapFile{Data: []byte("8")},
		"media/bgs/right-9.jpg":  &fstest.MapFile{Data: []byte("9")},
		"media/bgs/right-10.jpg": &fstest.MapFile{Data: []byte("10")},
		"media/bgs/sub/deep.jpg": &fstest.MapFile{Data: []byte("d")},
		"media/logo.png":         &fstest.MapFile{Data: []byte("l")},
		"slides/hello/photo.jpg": &fstest.MapFile{Data: []byte("p")},
	}
}

// TestExpandVariantsFrom exercises the pure expansion: the media-relative
// sorted result, directory and out-of-media filtering, and the rejected or
// empty globs.
func TestExpandVariantsFrom(t *testing.T) {
	root := enumLayerRootFS()

	tests := []struct {
		name       string
		field      *Field
		want       []string
		wantErrSub string
	}{
		{
			name:  "empty variants_from is a no-op",
			field: &Field{Name: "bg", Type: FieldEnum},
		},
		{
			name:  "nil field is a no-op",
			field: nil,
		},
		{
			name:  "glob expands media-relative and sorted",
			field: &Field{Name: "bg", Type: FieldEnum, VariantsFrom: "media/bgs/*.jpg"},
			want:  []string{"bgs/a.jpg", "bgs/b.jpg", "bgs/right-10.jpg", "bgs/right-8.jpg", "bgs/right-9.jpg"},
		},
		{
			name:  "directory match is not a variant",
			field: &Field{Name: "bg", Type: FieldEnum, VariantsFrom: "media/bgs/*"},
			want:  []string{"bgs/a.jpg", "bgs/b.jpg", "bgs/right-10.jpg", "bgs/right-8.jpg", "bgs/right-9.jpg"},
		},
		{
			name:  "glob at the media root drops the prefix",
			field: &Field{Name: "bg", Type: FieldEnum, VariantsFrom: "media/*"},
			want:  []string{"logo.png"},
		},
		{
			name:       "a match outside media is ignored and reports no file",
			field:      &Field{Name: "bg", Type: FieldEnum, VariantsFrom: "slides/hello/*.jpg"},
			wantErrSub: "matches no file under the library layer's media/ tree",
		},
		{
			name:       "glob matching no media file errors",
			field:      &Field{Name: "bg", Type: FieldEnum, VariantsFrom: "media/nope/*.jpg"},
			wantErrSub: "matches no file under the library layer's media/ tree",
		},
		{
			name:       "absolute glob is rejected",
			field:      &Field{Name: "bg", Type: FieldEnum, VariantsFrom: "/media/bgs/*.jpg"},
			wantErrSub: "must be a path relative to the library layer's root",
		},
		{
			name:       "parent-escaping glob is rejected",
			field:      &Field{Name: "bg", Type: FieldEnum, VariantsFrom: "../media/bgs/*.jpg"},
			wantErrSub: "must be a path relative to the library layer's root",
		},
		{
			name:       "bad glob pattern errors",
			field:      &Field{Name: "bg", Type: FieldEnum, VariantsFrom: "media/[.jpg"},
			wantErrSub: "is not a valid glob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandVariantsFrom(tt.field, root)
			if tt.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expandVariantsFrom() error = nil, want one containing %q (got %v)", tt.wantErrSub, got)
				}
				if !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("expandVariantsFrom() error = %q, want one containing %q", err.Error(), tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("expandVariantsFrom() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("expandVariantsFrom() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestEnumVariantsFromFeedsCheckValues wires the expanded set through
// checkFieldVariants — the production path that writes expandVariantsFrom's
// result into f.Variants — and then checks that CheckValues' enum membership
// check and its suggestion see exactly those variants.
func TestEnumVariantsFromFeedsCheckValues(t *testing.T) {
	root := enumLayerRootFS()

	f := Field{Name: "bg", Type: FieldEnum, VariantsFrom: "media/bgs/[ab].jpg"}
	// The glob matches the two bgs jpgs named a.jpg and b.jpg; the wiring
	// through checkFieldVariants is what this test pins.
	if err := checkFieldVariants("enumfrom", &f, root); err != nil {
		t.Fatalf("checkFieldVariants() error = %v", err)
	}
	if !reflect.DeepEqual(f.Variants, []string{"bgs/a.jpg", "bgs/b.jpg"}) {
		t.Fatalf("expanded Variants = %#v, want %#v", f.Variants, []string{"bgs/a.jpg", "bgs/b.jpg"})
	}
	if f.VariantsFrom != "" {
		t.Errorf("VariantsFrom = %q after expansion, want cleared", f.VariantsFrom)
	}

	tmpl := newSlide("enumfrom", f)

	t.Run("an expanded variant is accepted", func(t *testing.T) {
		got := CheckValues(map[string]any{"bg": "bgs/a.jpg"}, tmpl, nil)
		if len(got.Errors) != 0 {
			t.Fatalf("errors = %s, want none", renderErrors(got.Errors))
		}
	})

	t.Run("a non-variant reports the membership error and closest suggestion", func(t *testing.T) {
		got := CheckValues(map[string]any{"bg": "bgs/c.jpg"}, tmpl, nil)
		if len(got.Errors) != 1 {
			t.Fatalf("errors = %s, want 1", renderErrors(got.Errors))
		}
		e := got.Errors[0]
		want := we("enum", "bg", "bgs/c.jpg",
			`enum: "bgs/c.jpg" is not one of the allowed variants (bgs/a.jpg, bgs/b.jpg)`,
			`did you mean "bgs/a.jpg"? use one of: bgs/a.jpg, bgs/b.jpg`)
		if e.Rule != want.Rule || e.PathString() != want.Path || e.What != want.What || e.Fix != want.Fix {
			t.Errorf("error = rule %q path %q what %q fix %q, want rule %q path %q what %q fix %q",
				e.Rule, e.PathString(), e.What, e.Fix, want.Rule, want.Path, want.What, want.Fix)
		}
	})

	t.Run("a non-string value lists the expanded variants", func(t *testing.T) {
		got := CheckValues(map[string]any{"bg": 3}, tmpl, nil)
		if len(got.Errors) != 1 {
			t.Fatalf("errors = %s, want 1", renderErrors(got.Errors))
		}
		e := got.Errors[0]
		want := we("type", "bg", 3,
			"type: expected one of the enum variants, got a number",
			"use one of: bgs/a.jpg, bgs/b.jpg")
		if e.Rule != want.Rule || e.PathString() != want.Path || e.What != want.What || e.Fix != want.Fix {
			t.Errorf("error = rule %q path %q what %q fix %q, want rule %q path %q what %q fix %q",
				e.Rule, e.PathString(), e.What, e.Fix, want.Rule, want.Path, want.What, want.Fix)
		}
	})
}
