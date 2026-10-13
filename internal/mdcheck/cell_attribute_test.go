package mdcheck

import "testing"

// TestResolveCellAttributeBoolean covers the boolean forms: a bare `{name}` is
// `{name:true}`, `{name:true}` sets it true and `{name:false}` sets it false.
// The bare form is asserted equal to the explicit true form so the two cannot
// drift apart.
func TestResolveCellAttributeBoolean(t *testing.T) {
	tests := []struct {
		name       string
		occurrence string
		want       any
	}{
		{
			name:       "bare boolean is true",
			occurrence: "bold",
			want:       true,
		},
		{
			name:       "explicit true",
			occurrence: "bold:true",
			want:       true,
		},
		{
			name:       "explicit false",
			occurrence: "bold:false",
			want:       false,
		},
	}

	var bare, explicit any
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCellAttribute(tt.occurrence, cellTestSpecs)
			if err != nil {
				t.Fatalf("resolveCellAttribute(%q) error = %v, want none", tt.occurrence, err)
			}
			if got != tt.want {
				t.Fatalf("resolveCellAttribute(%q) = %#v, want %#v", tt.occurrence, got, tt.want)
			}
			switch tt.occurrence {
			case "bold":
				bare = got
			case "bold:true":
				explicit = got
			}
		})
	}

	if bare != explicit {
		t.Fatalf("bare {bold} = %#v, {bold:true} = %#v, want equal", bare, explicit)
	}
}

// TestResolveCellAttributeEnum covers the enum forms: `{name:variant}` sets the
// declared enum to the named variant, and a bare name that is the variant of
// exactly one declared enum (`{green}`) sets that attribute to the variant via
// the mutex short-hand.
func TestResolveCellAttributeEnum(t *testing.T) {
	tests := []struct {
		name       string
		occurrence string
		want       any
	}{
		{
			name:       "explicit variant",
			occurrence: "highlight:green",
			want:       "green",
		},
		{
			name:       "explicit other variant",
			occurrence: "highlight:red",
			want:       "red",
		},
		{
			name:       "bare variant resolves the sole matching enum",
			occurrence: "green",
			want:       "green",
		},
		{
			name:       "bare second variant resolves the sole matching enum",
			occurrence: "red",
			want:       "red",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCellAttribute(tt.occurrence, cellTestSpecs)
			if err != nil {
				t.Fatalf("resolveCellAttribute(%q) error = %v, want none", tt.occurrence, err)
			}
			if got != tt.want {
				t.Fatalf("resolveCellAttribute(%q) = %#v, want %#v", tt.occurrence, got, tt.want)
			}
		})
	}
}

// TestResolveCellAttributeErrors covers every rejection: a value that is not of
// the declared type (an enum value outside its variants, or a non-boolean for a
// boolean attribute), a bare declared enum written without a variant, an
// undeclared attribute — with the closest-match suggestion over the declared
// names when one is close enough and without a suggestion when none is — and the
// reserved `br`, which is never resolvable whether written bare or with a value.
func TestResolveCellAttributeErrors(t *testing.T) {
	tests := []struct {
		name       string
		occurrence string
		want       string
	}{
		{
			name:       "enum value outside the allowed variants",
			occurrence: "highlight:purple",
			want:       `cell attribute "highlight": "purple" is not one of the allowed variants (green, red)`,
		},
		{
			name:       "boolean given a non-boolean value",
			occurrence: "bold:green",
			want:       `cell attribute "bold": "green" is not a boolean`,
		},
		{
			name:       "bare declared enum requires a variant",
			occurrence: "highlight",
			want:       `cell attribute "highlight": a variant is required (green, red)`,
		},
		{
			name:       "undeclared attribute suggests the closest declared name",
			occurrence: "hilight",
			want:       `unknown cell attribute "hilight": did you mean "highlight"?`,
		},
		{
			name:       "far-off undeclared attribute carries no suggestion",
			occurrence: "xyz",
			want:       `unknown cell attribute "xyz"`,
		},
		{
			name:       "reserved br is never resolvable bare",
			occurrence: "br",
			want:       `cell attribute "br" is reserved and cannot be set`,
		},
		{
			name:       "reserved br is never resolvable with a value",
			occurrence: "br:true",
			want:       `cell attribute "br" is reserved and cannot be set`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCellAttribute(tt.occurrence, cellTestSpecs)
			if err == nil {
				t.Fatalf("resolveCellAttribute(%q) = %#v, want error", tt.occurrence, got)
			}
			if err.Error() != tt.want {
				t.Fatalf("resolveCellAttribute(%q) error = %q, want %q", tt.occurrence, err.Error(), tt.want)
			}
		})
	}
}
