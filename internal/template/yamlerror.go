package template

import (
	"github.com/really-knows-ai/kalide/internal/yamlpos"
)

// This file resolves the offending source line of a YAML parse error and, when
// the failure is the classic plain-scalar quoting problem, attaches the quoting
// hint (requirements.requirement.yaml-error-messages). It is the one
// implementation loadLibraryMeta (library.yaml, step 1) and loadLibraryTemplate
// (template.yaml, step 3) share, so a malformed manifest reads as
// templates/<path>:<line>: <problem> (<hint>) exactly like every other
// LibraryError.

// parseLibraryErrorLine builds the *LibraryError for a yaml.v3 parse failure on
// a library.yaml / template.yaml document. path is the offending file's display
// path (templates/…), source is the exact bytes that failed to parse, and err
// is the error yaml.Unmarshal returned.
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
// The message carries no location prefix, so a rendered LibraryError carries
// the path and line once:
//
//	templates/library.yaml:2: mapping values are not allowed in this context (quote the value as a string)
//
// It is the single helper the step-1 (library.yaml) and step-3 (template.yaml)
// parse-error sites call; it wires no caller itself.
func parseLibraryErrorLine(path string, source []byte, err error) *LibraryError {
	line, message, hint := yamlpos.Resolve(source, err)
	return &LibraryError{Path: path, Line: line, Message: message, Hint: hint}
}
