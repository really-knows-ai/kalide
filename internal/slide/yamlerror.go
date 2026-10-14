package slide

import (
	"fmt"

	"github.com/really-knows-ai/kalide/internal/yamlpos"
)

// This file builds the *ParseError for a slide or section frontmatter YAML
// parse failure, resolving the offending source line and, when the failure is
// the classic plain-scalar quoting problem, the quoting hint
// (requirements.requirement.yaml-error-messages). It delegates position
// resolution to yamlpos.Resolve, the one resolver the three loaders share,
// while keeping the slide error type (*ParseError) and the block's containment
// path, so a malformed frontmatter reads as
// <file>:<line>[ › <path>]: invalid YAML: <problem> (<hint>) exactly like the
// parser's other positioned errors. parseBlock (parse.go) is its one caller.

// yamlErrorWithLine builds the *ParseError for a yaml.v3 parse failure on a
// slide or section frontmatter block. file is the slide's identity used in
// errors, source is the exact (frontmatter-padded) bytes that failed to parse,
// path is the offending block's containment path ("" for the slide
// frontmatter, the section instance's chain otherwise), and err is the error
// yaml.Unmarshal returned.
//
// It delegates position resolution to yamlpos.Resolve, which returns the
// 1-based offending source line (line 0 when none can be resolved — reading
// err's "line N:" text, else recovering a lineless undefined-alias line from
// source), the message with yaml.v3's own "yaml: line N: " prefix stripped, and
// the plain-scalar quoting hint when the line at that position contains a plain
// scalar YAML mis-read:
//
//   - a `:` inside a plain scalar, the shape yaml.v3 rejects as "mapping values
//     are not allowed in this context" (for example `description: Hello: world`,
//     where the value `Hello: world` is read as a mapping rather than text); or
//   - a plain scalar whose first character is one of `*`, `&`, `%` or `@`, which
//     YAML reads as an alias, anchor, directive or reserved indicator rather
//     than literal text (for example `description: @example.com`).
//
// Because parseBlock pads source with the frontmatter's line offset, the
// resolved line is absolute within the file even for a lineless undefined-alias
// error recovered from source. The message carries no location prefix, so a
// rendered ParseError carries the file and line once:
//
//	slides/3-team.md:3 › columns[0]: invalid YAML: mapping values are not allowed in this context (quote the value as a string)
//
// It wires no caller itself; parseBlock calls it for the frontmatter it decodes.
func yamlErrorWithLine(file string, source []byte, path string, err error) *ParseError {
	line, message, hint := yamlpos.Resolve(source, err)
	msg := fmt.Sprintf("invalid YAML: %s", message)
	if hint != "" {
		msg = fmt.Sprintf("%s (%s)", msg, hint)
	}
	return &ParseError{File: file, Line: line, Path: path, Msg: msg}
}
