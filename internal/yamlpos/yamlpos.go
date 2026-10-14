// Package yamlpos resolves the offending source position of a yaml.v3 parse
// error (requirements.requirement.shared-yaml-error-position): the 1-based line
// the error names, the message with that location prefix stripped, and the
// plain-scalar quoting hint when the offending line is a plain scalar YAML
// mis-read. An undefined-alias error carries no line, so its line is recovered
// from the source instead.
//
// It is the one implementation shared by the three loaders that render their own
// positioned error form — template.parseLibraryErrorLine
// (internal/template/yamlerror.go), slide.yamlErrorWithLine
// (internal/slide/yamlerror.go) and deck.yamlErrorWithLine
// (internal/deck/yamlerror.go) — each keeping its own positioned error type and
// rendered form. It is a leaf package: it imports only the standard library.
package yamlpos

import (
	"regexp"
	"strconv"
	"strings"
)

// yamlErrorLinePattern extracts the 1-based source line yaml.v3 names in its
// parse errors ("yaml: line N: <problem>"). It matches the first such line, so
// a multi-line unmarshal error ("yaml: unmarshal errors:\n  line N: …") also
// resolves to its first offending line.
var yamlErrorLinePattern = regexp.MustCompile(`\bline (\d+):`)

// yamlErrorPrefixPattern matches the location prefix of a yaml.v3 parse error,
// with or without its leading "yaml: " ("yaml: line N: " or "line N: "). It is
// stripped from the message because the callers render the file/path and line
// themselves, so keeping the prefix would render the line twice.
var yamlErrorPrefixPattern = regexp.MustCompile(`^(?:yaml: )?line \d+: `)

// yamlUnknownAnchorPattern extracts the anchor name a yaml.v3 undefined-alias
// error names ("yaml: unknown anchor 'foo' referenced"). Unlike a scanner error
// this text carries no "line N:", so the offending line cannot be read off the
// error and is recovered from the source instead (see unknownAnchorLine).
var yamlUnknownAnchorPattern = regexp.MustCompile(`unknown anchor '([^']*)'`)

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

// Resolve resolves a yaml.v3 parse error (source is the exact bytes that failed
// to parse) into the pieces each loader renders in its own positioned error
// form:
//
//   - line is the 1-based offending source line, or 0 when none can be
//     resolved. It is read from err's "yaml: line N: " text; when yaml.v3 names
//     no line but the failure is an undefined alias ("unknown anchor 'name'
//     referenced"), it is recovered from source via unknownAnchorLine, so the
//     plain-value `*name` case is still positioned.
//   - message is err's text without yaml.v3's own "yaml: line N: " prefix, so a
//     caller that renders the file/path and line itself does not render the
//     line twice.
//   - hint is the plain-scalar quoting hint (yamlQuotingHint) when the line at
//     the resolved position contains a plain scalar YAML mis-read, and "" when
//     it does not. The mis-reads are a `:` inside a plain scalar — the shape
//     yaml.v3 rejects as "mapping values are not allowed in this context" (for
//     example `description: Hello: world`, where the value is read as a mapping
//     rather than text) — and a plain scalar whose first character is one of
//     `*`, `&`, `%` or `@`, which YAML reads as an alias, anchor, directive or
//     reserved indicator rather than literal text (for example
//     `author: @presenter`).
//
// It wires no caller itself; the three loaders call it and each keeps its own
// positioned error type and rendered form.
func Resolve(source []byte, err error) (line int, message, hint string) {
	text := err.Error()
	if m := yamlErrorLinePattern.FindStringSubmatch(text); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			line = n
		}
	}
	if line == 0 {
		line = unknownAnchorLine(source, text)
	}
	message = yamlErrorMessage(text)
	if line > 0 && plainScalarNeedsQuoting(lineAt(source, line)) {
		hint = yamlQuotingHint
	}
	return line, message, hint
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
// carriage return, or "" when the line does not exist. It counts the same "\n"
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

// unknownAnchorLine returns the 1-based line of the first non-comment source
// line whose first value token is the alias `*<name>`, where name is the
// undefined anchor errText names ("unknown anchor '<name>' referenced"), or 0
// when errText is not an undefined-alias error or no such line exists.
//
// yaml.v3 reports an undefined alias (`description: *foo`) as the lineless
// "yaml: unknown anchor 'foo' referenced", so the line cannot be read off the
// error and is recovered from the source instead. A value is matched by its
// `*`+name prefix rather than exact equality because the error names only the
// anchor token: `title: *foo bar` reports `unknown anchor 'foo'` while the
// line's value token is `*foo bar`.
func unknownAnchorLine(source []byte, errText string) int {
	m := yamlUnknownAnchorPattern.FindStringSubmatch(errText)
	if m == nil || m[1] == "" {
		return 0
	}
	alias := "*" + m[1]
	for i, raw := range strings.Split(string(source), "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		if strings.HasPrefix(yamlValueText(line), alias) {
			return i + 1
		}
	}
	return 0
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
