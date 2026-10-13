package template

import (
	"regexp"
	"strconv"
	"strings"
)

// This file resolves the offending source line of a YAML parse error and, when
// the failure is the classic plain-scalar quoting problem, attaches the quoting
// hint (requirements.requirement.yaml-error-messages). It is the one
// implementation loadLibraryMeta (library.yaml, step 1) and loadLibraryTemplate
// (template.yaml, step 3) share, so a malformed manifest reads as
// templates/<path>:<line>: <problem> (<hint>) exactly like every other
// LibraryError.

// yamlErrorLinePattern extracts the 1-based source line yaml.v3 names in its
// parse errors ("yaml: line N: <problem>"). It matches the first such line, so
// a multi-line unmarshal error ("yaml: unmarshal errors:\n  line N: …") also
// resolves to its first offending line.
var yamlErrorLinePattern = regexp.MustCompile(`\bline (\d+):`)

// yamlErrorPrefixPattern matches the location prefix of a yaml.v3 parse error,
// with or without its leading "yaml: " ("yaml: line N: " or "line N: "). It is
// stripped from the message because the returned *LibraryError renders the path
// and line itself, so keeping the prefix would render the line twice.
var yamlErrorPrefixPattern = regexp.MustCompile(`^(?:yaml: )?line \d+: `)

// yamlPlainForbiddenStart holds the characters that make YAML read a plain
// scalar as something other than the literal text the author wrote — an alias
// (`*`), an anchor (`&`), a directive (`%`) or a reserved indicator (`@`). A
// value beginning with one of them must be quoted to stay a plain scalar.
const yamlPlainForbiddenStart = "*&%@"

// yamlQuotingHint is the remediation a plain-scalar parse failure carries: the
// author quotes the offending value so YAML keeps it a plain scalar. It is the
// wording the field checker already uses for the same fix (checkvalues.go,
// "quote the value as a string"), kept identical so a quoting hint reads the
// same wherever it appears.
const yamlQuotingHint = "quote the value as a string"

// parseLibraryErrorLine builds the *LibraryError for a yaml.v3 parse failure on
// a library.yaml / template.yaml document. path is the offending file's display
// path (templates/…), source is the exact bytes that failed to parse, and err
// is the error yaml.Unmarshal returned.
//
// It resolves the offending source line from err's "line N:" text (line 0 when
// yaml.v3 names none). When the line at that position contains a plain scalar
// YAML mis-read, it additionally attaches yamlQuotingHint:
//
//   - a `:` inside a plain scalar, the shape yaml.v3 rejects as "mapping values
//     are not allowed in this context" (for example `description: Hello: world`,
//     where the value `Hello: world` is read as a mapping rather than text); or
//   - a plain scalar whose first character is one of `*`, `&`, `%` or `@`, which
//     YAML reads as an alias, anchor, directive or reserved indicator rather
//     than literal text (for example `description: @example.com`).
//
// The message is err's text without yaml.v3's own "yaml: line N: " prefix, so a
// rendered LibraryError carries the path and line once:
//
//	templates/library.yaml:2: mapping values are not allowed in this context (quote the value as a string)
//
// It is the single helper the step-1 (library.yaml) and step-3 (template.yaml)
// parse-error sites call; it wires no caller itself.
func parseLibraryErrorLine(path string, source []byte, err error) *LibraryError {
	text := err.Error()
	line := 0
	if m := yamlErrorLinePattern.FindStringSubmatch(text); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			line = n
		}
	}
	e := &LibraryError{Path: path, Line: line, Message: yamlErrorMessage(text)}
	if line > 0 && plainScalarNeedsQuoting(lineAt(source, line)) {
		e.Hint = yamlQuotingHint
	}
	return e
}

// yamlErrorMessage returns a yaml.v3 error's text without its location prefix:
// "yaml: line N: <problem>" becomes "<problem>", and a bare "yaml: <problem>"
// becomes "<problem>". Any other text is returned unchanged.
func yamlErrorMessage(text string) string {
	if loc := yamlErrorPrefixPattern.FindStringIndex(text); loc != nil {
		return text[loc[1]:]
	}
	return strings.TrimPrefix(text, "yaml: ")
}

// lineAt returns the text of source's 1-based line without its trailing
// carriage return, and whether the line exists. It counts the same "\n"
// separators yaml.v3 counts, so a "\r\n" file still resolves.
func lineAt(source []byte, line int) string {
	if line < 1 {
		return ""
	}
	lines := strings.Split(string(source), "\n")
	if line > len(lines) {
		return ""
	}
	return strings.TrimSuffix(lines[line-1], "\r")
}

// plainScalarNeedsQuoting reports whether line contains a plain scalar YAML
// mis-read, so quoting the value fixes the parse. It inspects only the first
// value token on the line — past a sequence indicator and a key separator — and
// ignores a value that is already quoted or a flow/block collection, whose
// internal `:` needs no quoting.
func plainScalarNeedsQuoting(line string) bool {
	scalar := yamlValueText(line)
	if scalar == "" {
		return false
	}
	if strings.ContainsRune(yamlPlainForbiddenStart, rune(scalar[0])) {
		return true
	}
	switch scalar[0] {
	case '"', '\'', '[', '{', '|', '>':
		return false
	}
	return strings.Contains(scalar, ": ") || strings.HasSuffix(scalar, ":")
}

// yamlValueText returns the text of the first value token on a YAML line: the
// line's own trimmed text, or, when the line carries one, the text after a
// leading sequence indicator "- " and the first key separator (a `:` followed
// by a space). A trailing " #" comment and a bare comment line are dropped,
// since a `:` inside a comment is not part of the value.
func yamlValueText(line string) string {
	s := strings.TrimLeft(line, " \t")
	if rest, ok := strings.CutPrefix(s, "- "); ok {
		s = strings.TrimLeft(rest, " \t")
	}
	if i := strings.Index(s, ": "); i >= 0 {
		s = strings.TrimLeft(s[i+2:], " \t")
	} else if strings.HasSuffix(s, ":") {
		s = ""
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = strings.TrimRight(s[:i], " \t")
	}
	if s == "" || s[0] == '#' {
		return ""
	}
	return s
}
