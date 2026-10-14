package yamlpos

import (
	"errors"
	"testing"

	"gopkg.in/yaml.v3"
)

// This file is the unit-test deliverable for phase-02.task-7: it exercises
// yamlpos.Resolve, the one resolver of a yaml.v3 parse error's offending source
// line, message and plain-scalar quoting hint, on which the three loaders
// (template.parseLibraryErrorLine, slide.yamlErrorWithLine,
// deck.yamlErrorWithLine) rely (requirements.requirement.shared-yaml-error-position).
//
// Cases whose error text real yaml.v3 produces run yaml.Unmarshal on the source
// and hand Resolve that exact error, so a change to yaml.v3's error wording or
// the resolver's patterns is caught here. Cases that exercise a branch real
// yaml.v3 cannot reach at the resolved position — a valid block scalar that
// produces no parse error, an out-of-range line, a multi-line unmarshal error
// — supply the error text directly.

// yamlParseError returns the error yaml.v3 reports for src. Decoding into
// struct{} suppresses type errors, so only the scanner/alias errors Resolve is
// built to read surface — the same failure a loader's yaml.Unmarshal hands it.
func yamlParseError(t *testing.T, src string) error {
	t.Helper()
	var v struct{}
	err := yaml.Unmarshal([]byte(src), &v)
	if err == nil {
		t.Fatalf("yaml.Unmarshal(%q) = nil, want a parse error", src)
	}
	return err
}

// TestResolve covers Resolve's three products — the 1-based offending line, the
// message with yaml.v3's location prefix stripped, and the plain-scalar quoting
// hint — across the resolver's branches.
func TestResolve(t *testing.T) {
	const (
		colonMsg   = "mapping values are not allowed in this context"
		anchorMsg  = "unknown anchor 'foo' referenced"
		scannerMsg = "did not find expected alphabetic or numeric character"
		charMsg    = "found character that cannot start any token"
	)

	cases := []struct {
		name string
		src  string
		// err, when set, is used verbatim; when nil the error is produced by
		// real yaml.v3 from src.
		err         error
		wantLine    int
		wantMessage string
		wantHint    string
	}{
		{
			name:        "line prefix stripped and the offending line resolved",
			src:         "title: Deck\nauthor: Ada\ndate: Hello: world\n",
			wantLine:    3,
			wantMessage: colonMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			// A one-character `*` is a scanner error that names its line; the
			// hint still comes from the offending value.
			name:        "bare alias indicator resolved through yaml's own line",
			src:         "title: Deck\nauthor: *\n",
			wantLine:    2,
			wantMessage: scannerMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			// yaml.v3 reports an undefined alias with no line, so the line is
			// recovered from the source.
			name:        "named alias without an anchor recovers its line",
			src:         "title: Deck\nauthor: *foo\n",
			wantLine:    2,
			wantMessage: anchorMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			// The error names only the anchor token, so the line's value
			// (`*foo bar`) matches by the `*foo` prefix.
			name:        "named alias with trailing text recovers by alias prefix",
			src:         "title: Deck\nauthor: *foo bar\n",
			wantLine:    2,
			wantMessage: anchorMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			name:        "recovery skips a comment line naming the alias",
			src:         "title: Deck\n# see *foo\nauthor: *foo\n",
			wantLine:    3,
			wantMessage: anchorMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			name:        "recovery matches a sequence-indicator value",
			src:         "title: Deck\nitems:\n  - *foo\n",
			wantLine:    3,
			wantMessage: anchorMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			name:        "anchor indicator start is forbidden",
			src:         "title: Deck\nauthor: &\n",
			wantLine:    2,
			wantMessage: scannerMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			name:        "directive indicator start is forbidden",
			src:         "title: Deck\nauthor: %example.com\n",
			wantLine:    2,
			wantMessage: charMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			name:        "reserved indicator start is forbidden",
			src:         "title: Deck\nauthor: @example.com\n",
			wantLine:    2,
			wantMessage: charMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			name:        "trailing colon plain scalar is hinted",
			src:         "title: Deck\ndate: Hello:\n",
			wantLine:    2,
			wantMessage: colonMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			name:        "flow collection carries no hint",
			src:         "title: Deck\ndate: [\n",
			wantLine:    2,
			wantMessage: "did not find expected node content",
			wantHint:    "",
		},
		{
			name:        "quoted scalar carries no hint",
			src:         "title: Deck\nauthor: Ada\ndate: \"unterminated\n",
			wantLine:    3,
			wantMessage: "found unexpected end of stream",
			wantHint:    "",
		},
		{
			// A block scalar's own `:` needs no quoting; a valid block header
			// produces no real parse error, so the error text is supplied.
			name:        "block scalar carries no hint",
			src:         "title: Deck\nbody: |\n  text\n",
			err:         errors.New("yaml: line 2: found unexpected end of stream"),
			wantLine:    2,
			wantMessage: "found unexpected end of stream",
			wantHint:    "",
		},
		{
			name:        "folded block scalar carries no hint",
			src:         "title: Deck\nbody: >\n  text\n",
			err:         errors.New("yaml: line 2: found unexpected end of stream"),
			wantLine:    2,
			wantMessage: "found unexpected end of stream",
			wantHint:    "",
		},
		{
			// CRLF: yaml.v3 counts \n separators, and the resolver strips the
			// trailing \r before inspecting the line.
			name:        "CRLF line counting and carriage-return trimming",
			src:         "title: Deck\r\nauthor: Ada\r\ndate: [\r\n",
			wantLine:    3,
			wantMessage: "did not find expected node content",
			wantHint:    "",
		},
		{
			name:        "CRLF plain scalar still hints",
			src:         "title: Deck\r\nauthor: @presenter\r\n",
			wantLine:    2,
			wantMessage: charMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			name:        "CRLF undefined-anchor recovery",
			src:         "title: Deck\r\nauthor: *foo\r\n",
			wantLine:    2,
			wantMessage: anchorMsg,
			wantHint:    yamlQuotingHint,
		},
		{
			// A multi-line unmarshal error (type mismatch) names its first
			// offending line; Resolve reads that first `line N:`.
			name:        "multi-line unmarshal error resolves its first line",
			src:         "a: 1\nb: 2\nd: [x]\n",
			err:         errors.New("yaml: unmarshal errors:\n  line 3: cannot unmarshal !!seq into string"),
			wantLine:    3,
			wantMessage: "unmarshal errors:\n  line 3: cannot unmarshal !!seq into string",
			wantHint:    "",
		},
		{
			name:        "out-of-range line yields no hint",
			src:         "title: Deck\n",
			err:         errors.New("yaml: line 99: boom"),
			wantLine:    99,
			wantMessage: "boom",
			wantHint:    "",
		},
		{
			name:        "line zero is not recovered without an anchor",
			src:         "title: Deck\n",
			err:         errors.New("yaml: line 0: boom"),
			wantLine:    0,
			wantMessage: "boom",
			wantHint:    "",
		},
		{
			name:        "unnamed anchor is not recovered",
			src:         "title: Deck\nauthor: *\n",
			err:         errors.New("yaml: unknown anchor '' referenced"),
			wantLine:    0,
			wantMessage: "unknown anchor '' referenced",
			wantHint:    "",
		},
		{
			name:        "anchor error with no matching source line resolves no line",
			src:         "title: Deck\nauthor: Ada\n",
			err:         errors.New(anchorMsg),
			wantLine:    0,
			wantMessage: anchorMsg,
			wantHint:    "",
		},
		{
			name:        "alias-prefix recovery ignores a non-leading alias",
			src:         "title: Deck\nauthor: not-*foo\n",
			err:         errors.New(anchorMsg),
			wantLine:    0,
			wantMessage: anchorMsg,
			wantHint:    "",
		},
		{
			name:        "bare yaml prefix without a line is stripped",
			src:         "title: Deck\n",
			err:         errors.New("yaml: something went wrong"),
			wantLine:    0,
			wantMessage: "something went wrong",
			wantHint:    "",
		},
		{
			name:        "non-yaml error passes through unchanged",
			src:         "title: Deck\n",
			err:         errors.New(colonMsg),
			wantLine:    0,
			wantMessage: colonMsg,
			wantHint:    "",
		},
		{
			name:        "line without a numeric value names no line",
			src:         "a: 1\n",
			err:         errors.New("yaml: line x: bad token"),
			wantLine:    0,
			wantMessage: "line x: bad token",
			wantHint:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err
			if err == nil {
				err = yamlParseError(t, tc.src)
			}
			line, message, hint := Resolve([]byte(tc.src), err)
			if line != tc.wantLine {
				t.Errorf("line = %d, want %d", line, tc.wantLine)
			}
			if message != tc.wantMessage {
				t.Errorf("message = %q, want %q", message, tc.wantMessage)
			}
			if hint != tc.wantHint {
				t.Errorf("hint = %q, want %q", hint, tc.wantHint)
			}
		})
	}
}
