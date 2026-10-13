package template

import (
	"reflect"
	"testing"

	"github.com/really-knows-ai/kalide/internal/mdcheck"
)

// This file is the unit-test deliverable for the `body.tables` manifest
// option and its declared cell attributes (table-opt-in,
// table-limits, table-cell-attributes): parseManifestTables and
// parseManifestCellAttributes, reached through the real parseManifest entry
// point via the package's existing manifest test helpers
// (manifestLibraryTemplate, mustParseManifest, asLibraryError).
//
// It pins:
//
//   - an absent `tables:` key and the scalar `false` both opt out
//     (Body.Tables nil); any other value must be a mapping;
//   - the mapping's numeric limits parse with their declared defaults
//     (max_tables 1, min_columns 1, the rest 0), and an explicit `0` on a
//     `max_*` key (unbounded) or `min_columns` is not overwritten by a
//     default;
//   - `true` and other scalars, an unknown sub-key (with a closest-match
//     suggestion), a non-integer or negative bound and a min above its
//     bounded max are positioned templates errors naming the offending
//     template.yaml;
//   - `cell_attributes` decodes boolean and enum descriptors, and rejects a
//     malformed descriptor, the reserved `br`, a duplicate name, a bad type,
//     an enum without variants and an unknown descriptor key;
//   - an ambiguous declaration set — a declared boolean whose name is also an
//     enum variant, or a variant declared by two enums — is refused at load by
//     checkCellAttributeAmbiguity with an error naming the ambiguous name and
//     the template.yaml;
//   - the single shared CellAttributeSpecs conversion maps FieldBoolean and
//     FieldEnum onto mdcheck's plain boolean/enum descriptors, copies an enum's
//     variants rather than aliasing the declaration slice, and yields nil for a
//     template that declares none.
//
// The fixture manifest is always a minimal slide whose body carries `mode`
// (required) and the `tables:` value under test; the test reads the real
// error text from the implementation rather than a paraphrase.

// tableTestManifestPath is the ManifestPath manifestLibraryTemplate gives a
// KindSlide template named name — the path a parseManifest error is
// positioned at.
func tableTestManifestPath(name string) string {
	return "slides/" + name + "/" + ManifestFile
}

// tableManifestError parses manifest for a slide template named "tables" and
// returns the *LibraryError parseManifest refuses it with.
func tableManifestError(t *testing.T, manifest string) *LibraryError {
	t.Helper()
	_, err := parseManifest(manifestLibraryTemplate("tables", KindSlide, manifest), nil)
	return asLibraryError(t, err)
}

// TestManifestBodyTablesOption is the success half of the `body.tables` option
// decoding: the opt-out forms and the mapping's limits with their declared
// defaults and the explicit-zero overrides.
func TestManifestBodyTablesOption(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     *TableRule
	}{
		{
			name:     "an absent tables key opts out",
			manifest: "body:\n  mode: optional\n",
			want:     nil,
		},
		{
			name:     "the scalar false opts out",
			manifest: "body:\n  mode: optional\n  tables: false\n",
			want:     nil,
		},
		{
			name:     "an empty mapping opts in with the declared defaults",
			manifest: "body:\n  mode: optional\n  tables: {}\n",
			want:     &TableRule{MaxTables: 1, MinColumns: 1},
		},
		{
			name:     "an explicit max_tables 0 is unbounded and is not overwritten by the default 1",
			manifest: "body:\n  mode: optional\n  tables:\n    max_tables: 0\n",
			want:     &TableRule{MaxTables: 0, MinColumns: 1},
		},
		{
			name:     "an explicit min_columns 0 stands rather than taking the default 1",
			manifest: "body:\n  mode: optional\n  tables:\n    min_columns: 0\n",
			want:     &TableRule{MaxTables: 1, MinColumns: 0},
		},
		{
			name:     "every limit parses",
			manifest: "body:\n  mode: optional\n  tables:\n    min_tables: 1\n    max_tables: 3\n    min_columns: 2\n    max_columns: 4\n    min_rows: 1\n    max_rows: 5\n",
			want:     &TableRule{MinTables: 1, MaxTables: 3, MinColumns: 2, MaxColumns: 4, MinRows: 1, MaxRows: 5},
		},
		{
			name:     "a min equal to its bounded max is accepted",
			manifest: "body:\n  mode: optional\n  tables:\n    min_tables: 2\n    max_tables: 2\n    min_columns: 3\n    max_columns: 3\n",
			want:     &TableRule{MinTables: 2, MaxTables: 2, MinColumns: 3, MaxColumns: 3},
		},
		{
			name:     "a min above an unbounded max is accepted",
			manifest: "body:\n  mode: optional\n  tables:\n    min_tables: 9\n    max_tables: 0\n",
			want:     &TableRule{MinTables: 9, MaxTables: 0, MinColumns: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := mustParseManifest(t, "tables", KindSlide, tt.manifest)
			if !reflect.DeepEqual(def.Body.Tables, tt.want) {
				t.Errorf("Body.Tables = %+v, want %+v", def.Body.Tables, tt.want)
			}
		})
	}
}

// TestManifestBodyTablesOptionErrors is the rejection half of the
// `body.tables` option decoding: a value that is neither a mapping nor
// `false`, an unknown sub-key (with its closest-match suggestion), a
// malformed bound and a min above its bounded max. Each is a positioned
// templates error naming the offending template.yaml.
func TestManifestBodyTablesOptionErrors(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wantMsg  string
		wantSugg string
	}{
		{
			name:     "the scalar true is neither a mapping nor false",
			manifest: "body:\n  mode: optional\n  tables: true\n",
			wantMsg:  `key "tables": expected a mapping or false`,
		},
		{
			name:     "a number is neither a mapping nor false",
			manifest: "body:\n  mode: optional\n  tables: 5\n",
			wantMsg:  `key "tables": expected a mapping or false`,
		},
		{
			name:     "a sequence is neither a mapping nor false",
			manifest: "body:\n  mode: optional\n  tables: [a]\n",
			wantMsg:  `key "tables": expected a mapping or false`,
		},
		{
			name:     "an unknown sub-key suggests the closest known one",
			manifest: "body:\n  mode: optional\n  tables:\n    max_table: 2\n",
			wantMsg:  `unknown key "max_table" in the tables rule`,
			wantSugg: "max_tables",
		},
		{
			name:     "a non-integer bound is refused naming the key",
			manifest: "body:\n  mode: optional\n  tables:\n    max_rows: many\n",
			wantMsg:  `key "max_rows": expected an integer`,
		},
		{
			name:     "a fractional bound is refused naming the key",
			manifest: "body:\n  mode: optional\n  tables:\n    max_columns: 1.5\n",
			wantMsg:  `key "max_columns": expected an integer`,
		},
		{
			name:     "a negative bound is refused naming the key",
			manifest: "body:\n  mode: optional\n  tables:\n    min_tables: -1\n",
			wantMsg:  `key "min_tables": expected a non-negative integer`,
		},
		{
			name:     "min_tables above the default max_tables 1 is refused",
			manifest: "body:\n  mode: optional\n  tables:\n    min_tables: 2\n",
			wantMsg:  `tables: min_tables 2 is above max_tables 1`,
		},
		{
			name:     "min_tables above a declared max_tables is refused",
			manifest: "body:\n  mode: optional\n  tables:\n    min_tables: 3\n    max_tables: 2\n",
			wantMsg:  `tables: min_tables 3 is above max_tables 2`,
		},
		{
			name:     "min_columns above a declared max_columns is refused",
			manifest: "body:\n  mode: optional\n  tables:\n    min_columns: 4\n    max_columns: 3\n",
			wantMsg:  `tables: min_columns 4 is above max_columns 3`,
		},
		{
			name:     "min_rows above a declared max_rows is refused",
			manifest: "body:\n  mode: optional\n  tables:\n    min_rows: 5\n    max_rows: 4\n",
			wantMsg:  `tables: min_rows 5 is above max_rows 4`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			libErr := tableManifestError(t, tt.manifest)
			if want := tableTestManifestPath("tables"); libErr.Path != want {
				t.Errorf("Path = %q, want %q (the offending template.yaml)", libErr.Path, want)
			}
			if libErr.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", libErr.Message, tt.wantMsg)
			}
			if libErr.Suggestion != tt.wantSugg {
				t.Errorf("Suggestion = %q, want %q", libErr.Suggestion, tt.wantSugg)
			}
		})
	}
}

// TestManifestBodyTablesCellAttributes is the success half of the
// `cell_attributes` decoding: a boolean and an enum descriptor land on the
// rule in declaration order, an empty list is an empty (non-nil) set, and the
// declarations coexist with the rule's limits.
func TestManifestBodyTablesCellAttributes(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     []CellAttribute
	}{
		{
			name: "a boolean and an enum attribute parse in declaration order",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: bold
        type: boolean
      - name: highlight
        type: enum
        variants: [red, green]
`,
			want: []CellAttribute{
				{Name: "bold", Kind: FieldBoolean},
				{Name: "highlight", Kind: FieldEnum, Variants: []string{"red", "green"}},
			},
		},
		{
			name: "an empty cell_attributes list is an empty set",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes: []
`,
			want: []CellAttribute{},
		},
		{
			name: "cell_attributes coexist with the rule's limits",
			manifest: `body:
  mode: optional
  tables:
    max_tables: 2
    min_columns: 2
    cell_attributes:
      - name: bold
        type: boolean
`,
			want: []CellAttribute{
				{Name: "bold", Kind: FieldBoolean},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := mustParseManifest(t, "tables", KindSlide, tt.manifest)
			if def.Body.Tables == nil {
				t.Fatal("Body.Tables = nil, want the declared rule")
			}
			if !reflect.DeepEqual(def.Body.Tables.CellAttributes, tt.want) {
				t.Errorf("Body.Tables.CellAttributes = %+v, want %+v", def.Body.Tables.CellAttributes, tt.want)
			}
		})
	}
}

// TestManifestBodyTablesCellAttributesErrors is the rejection half of the
// `cell_attributes` decoding. Each fixture carries one malformed descriptor
// and each is refused as a positioned templates error naming the offending
// template.yaml.
func TestManifestBodyTablesCellAttributesErrors(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wantMsg  string
		wantSugg string
	}{
		{
			name: "cell_attributes is not a sequence",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes: nope
`,
			wantMsg: `key "cell_attributes": expected a sequence`,
		},
		{
			name: "an entry is not a mapping",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - nope
`,
			wantMsg: "expected a mapping describing a cell attribute",
		},
		{
			name: "an unknown descriptor key suggests the closest known one",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: bold
        type: boolean
        variant: red
`,
			wantMsg:  `unknown key "variant" in a cell attribute`,
			wantSugg: "variants",
		},
		{
			name: "a missing name is refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - type: boolean
`,
			wantMsg: `cell attribute: missing required key "name"`,
		},
		{
			name: "a non-string name is refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: [x]
        type: boolean
`,
			wantMsg: `key "name": expected a string`,
		},
		{
			name: "the reserved name br is refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: br
        type: boolean
`,
			wantMsg: `cell attribute "br" is reserved and cannot be declared`,
		},
		{
			name: "a duplicate name is refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: bold
        type: boolean
      - name: bold
        type: enum
        variants: [x]
`,
			wantMsg: `cell attribute "bold" is declared more than once`,
		},
		{
			name: "a missing type is refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: bold
`,
			wantMsg: `cell attribute "bold": missing required key "type"`,
		},
		{
			name: "a type that is neither boolean nor enum is refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: bold
        type: text
`,
			wantMsg: `cell attribute "bold": cell attribute type must be boolean or enum`,
		},
		{
			name: "variants on a boolean attribute are refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: bold
        type: boolean
        variants: [x]
`,
			wantMsg: `cell attribute "bold": key "variants" is only valid on an enum cell attribute`,
		},
		{
			name: "an enum without variants is refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: mood
        type: enum
`,
			wantMsg: `cell attribute "mood": missing required key "variants"`,
		},
		{
			name: "an enum with an empty variants list is refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: mood
        type: enum
        variants: []
`,
			wantMsg: `cell attribute "mood": key "variants" must not be empty`,
		},
		{
			name: "a repeated variant in one enum is refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: mood
        type: enum
        variants: [red, red]
`,
			wantMsg: `cell attribute "mood": variant "red" is declared more than once`,
		},
		{
			name: "variants that is not a sequence of strings is refused",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: mood
        type: enum
        variants: red
`,
			wantMsg: `key "variants": expected a sequence of strings`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			libErr := tableManifestError(t, tt.manifest)
			if want := tableTestManifestPath("tables"); libErr.Path != want {
				t.Errorf("Path = %q, want %q (the offending template.yaml)", libErr.Path, want)
			}
			if libErr.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", libErr.Message, tt.wantMsg)
			}
			if libErr.Suggestion != tt.wantSugg {
				t.Errorf("Suggestion = %q, want %q", libErr.Suggestion, tt.wantSugg)
			}
		})
	}
}

// TestManifestBodyTablesCellAttributeAmbiguity proves checkCellAttributeAmbiguity
// runs at template load over the whole decoded declaration set: a declared
// boolean whose name is also an enum variant, and an enum variant declared by
// more than one enum, are each refused with an error naming the ambiguous name
// and the offending template.yaml, while an unambiguous set loads.
func TestManifestBodyTablesCellAttributeAmbiguity(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wantMsg  string
	}{
		{
			name: "a boolean whose name is also an enum variant is ambiguous",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: bold
        type: boolean
      - name: mood
        type: enum
        variants: [bold, calm]
`,
			wantMsg: `cell attribute "bold" is ambiguous: it is declared as a boolean attribute and as a variant of the enum attribute "mood"`,
		},
		{
			name: "a variant declared by two enums is ambiguous",
			manifest: `body:
  mode: optional
  tables:
    cell_attributes:
      - name: mood
        type: enum
        variants: [calm, bright]
      - name: tone
        type: enum
        variants: [bright, loud]
`,
			wantMsg: `cell attribute "bright" is ambiguous: it is a variant of more than one declared enum attribute`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			libErr := tableManifestError(t, tt.manifest)
			if want := tableTestManifestPath("tables"); libErr.Path != want {
				t.Errorf("Path = %q, want %q (the offending template.yaml)", libErr.Path, want)
			}
			if libErr.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", libErr.Message, tt.wantMsg)
			}
			if libErr.Suggestion != "" {
				t.Errorf("Suggestion = %q, want none", libErr.Suggestion)
			}
		})
	}

	t.Run("an unambiguous set loads", func(t *testing.T) {
		const manifest = `body:
  mode: optional
  tables:
    cell_attributes:
      - name: bold
        type: boolean
      - name: mood
        type: enum
        variants: [calm]
      - name: tone
        type: enum
        variants: [loud]
`
		def := mustParseManifest(t, "tables", KindSlide, manifest)
		if def.Body.Tables == nil {
			t.Fatal("Body.Tables = nil, want the declared rule")
		}
		if got := len(def.Body.Tables.CellAttributes); got != 3 {
			t.Fatalf("len(CellAttributes) = %d, want 3", got)
		}
		if def.Body.Tables.CellAttributes[0].Kind != FieldBoolean {
			t.Errorf("CellAttributes[0].Kind = %q, want %q", def.Body.Tables.CellAttributes[0].Kind, FieldBoolean)
		}
	})
}

// TestCellAttributeSpecs covers the single shared template.CellAttribute ->
// mdcheck.CellAttributeSpec conversion (task-1): the FieldBoolean/FieldEnum
// kind mapping onto mdcheck's plain boolean/enum constants, the copied
// variants that never alias the template's own declaration slice, and nil for
// a template that declares no cell attributes.
func TestCellAttributeSpecs(t *testing.T) {
	t.Run("FieldBoolean and FieldEnum map to mdcheck's boolean and enum", func(t *testing.T) {
		got := CellAttributeSpecs([]CellAttribute{
			{Name: "bold", Kind: FieldBoolean},
			{Name: "highlight", Kind: FieldEnum, Variants: []string{"red", "green"}},
		})
		want := []mdcheck.CellAttributeSpec{
			{Name: "bold", Kind: mdcheck.CellAttributeBoolean},
			{Name: "highlight", Kind: mdcheck.CellAttributeEnum, Variants: []string{"red", "green"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("CellAttributeSpecs = %+v, want %+v", got, want)
		}
	})

	t.Run("no declarations yields nil", func(t *testing.T) {
		if got := CellAttributeSpecs(nil); got != nil {
			t.Errorf("CellAttributeSpecs(nil) = %+v, want nil", got)
		}
		if got := CellAttributeSpecs([]CellAttribute{}); got != nil {
			t.Errorf("CellAttributeSpecs(empty) = %+v, want nil", got)
		}
	})

	t.Run("variants are copied and never alias the declaration slice", func(t *testing.T) {
		attrs := []CellAttribute{
			{Name: "highlight", Kind: FieldEnum, Variants: []string{"red", "green"}},
		}
		got := CellAttributeSpecs(attrs)
		if len(got) != 1 {
			t.Fatalf("len(CellAttributeSpecs) = %d, want 1", len(got))
		}

		// Mutating the declaration slice must leave the converted spec's copy
		// untouched: the converter took a copy rather than aliasing it.
		attrs[0].Variants[0] = "mutated"
		if want := []string{"red", "green"}; !reflect.DeepEqual(got[0].Variants, want) {
			t.Errorf("after mutating the declaration, Variants = %v, want the copy %v", got[0].Variants, want)
		}

		// And the copy owns its own backing array, so growing it cannot spill
		// back into the declaration slice.
		got[0].Variants = append(got[0].Variants, "blue")
		if want := []string{"mutated", "green"}; !reflect.DeepEqual(attrs[0].Variants, want) {
			t.Errorf("after appending to the copy, declaration Variants = %v, want %v (unaffected)", attrs[0].Variants, want)
		}
	})
}
