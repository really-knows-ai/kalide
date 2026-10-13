package slide

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// This file resolves the offending source line of a slide or section
// frontmatter YAML parse error and, when the failure is the classic
// plain-scalar quoting problem, attaches the quoting hint
// (requirements.requirement.yaml-error-messages). It mirrors
// template.parseLibraryErrorLine (internal/template/yamlerror.go) in a
// slide-local form — internal/slide cannot import internal/template — but
// produces the slide error type (*ParseError) and keeps the block's
// containment path, so a malformed frontmatter reads as
// <file>:<line>[ › <path>]: invalid YAML: <problem> (<hint>) exactly like the
// parser's other positioned errors. parseBlock (parse.go) is its one caller.

// yamlErrorLinePattern extracts the 1-based source line yaml.v3 names in its
// parse errors ("yaml: line N: <problem>"). It matches the first such line, so
// a multi-line unmarshal error ("yaml: unmarshal errors:\n  line N: …") also
// resolves to its first offending line.
var yamlErrorLinePattern = regexp.MustCompile(`\bline (\d+):`)

// yamlErrorPrefixPattern matches the location prefix of a yaml.v3 parse error,
// with or without its leading "yaml: " ("yaml: line N: " or "line N: "). It is
// stripped from the message because the returned *ParseError renders the file
// and line itself, so keeping the prefix would render the line twice.
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
// same wording the field checker and the template loader use for the same fix
// (checkvalues.go and internal/template/yamlerror.go, "quote the value as a
// string"), kept identical so a quoting hint reads the same wherever it
// appears.
const yamlQuotingHint = "quote the value as a string"

// yamlErrorWithLine builds the *ParseError for a yaml.v3 parse failure on a
// slide or section frontmatter block. file is the slide's identity used in
// errors, source is the exact (frontmatter-padded) bytes that failed to parse,
// path is the offending block's containment path ("" for the slide
// frontmatter, the section instance's chain otherwise), and err is the error
// yaml.Unmarshal returned.
//
// It resolves the offending source line from err's "line N:" text (line 0 when
// yaml.v3 names none). When yaml.v3 names none but the failure is an undefined
// alias ("unknown anchor 'name' referenced"), it recovers the line from source
// via unknownAnchorLine; because parseBlock pads source with the frontmatter's
// line offset, the recovered line is likewise absolute within the file. When
// the line at that position contains a plain scalar YAML mis-read, it
// additionally attaches yamlQuotingHint:
//
//   - a `:` inside a plain scalar, the shape yaml.v3 rejects as "mapping values
//     are not allowed in this context" (for example `description: Hello: world`,
//     where the value `Hello: world` is read as a mapping rather than text); or
//   - a plain scalar whose first character is one of `*`, `&`, `%` or `@`, which
//     YAML reads as an alias, anchor, directive or reserved indicator rather
//     than literal text (for example `description: @example.com`).
//
// The message is err's text without yaml.v3's own "yaml: line N: " prefix, so
// a rendered ParseError carries the file and line once:
//
//	slides/3-team.md:3 › columns[0]: invalid YAML: mapping values are not allowed in this context (quote the value as a string)
//
// It wires no caller itself; parseBlock calls it for the frontmatter it
// decodes.
func yamlErrorWithLine(file string, source []byte, path string, err error) *ParseError {
	text := err.Error()
	line := 0
	if m := yamlErrorLinePattern.FindStringSubmatch(text); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			line = n
		}
	}
	if line == 0 {
		line = unknownAnchorLine(source, text)
	}
	msg := fmt.Sprintf("invalid YAML: %s", yamlErrorMessage(text))
	if line > 0 && plainScalarNeedsQuoting(plainScalarLine(source, line)) {
		msg = fmt.Sprintf("%s (%s)", msg, yamlQuotingHint)
	}
	return &ParseError{File: file, Line: line, Path: path, Msg: msg}
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

// plainScalarLine returns the text of source's 1-based line without its
// trailing carriage return, or "" when the line does not exist. It counts the
// same "\n" separators yaml.v3 counts, so a "\r\n" file still resolves.
func plainScalarLine(source []byte, line int) string {
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
// line's value token is `*foo bar`. source is the frontmatter-padded bytes
// parseBlock hands over, so the returned line is already absolute within the
// file.
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
		if strings.HasPrefix(yamlScalarText(line), alias) {
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
	scalar := yamlScalarText(line)
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

// yamlScalarText returns the text of the first value token on a YAML line: the
// line's own trimmed text, or, when the line carries one, the text after a
// leading sequence indicator "- " and the first key separator (a `:` followed
// by a space). A trailing " #" comment and a bare comment line are dropped,
// since a `:` inside a comment is not part of the value.
func yamlScalarText(line string) string {
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
