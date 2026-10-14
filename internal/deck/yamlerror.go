package deck

import (
	"fmt"

	"github.com/really-knows-ai/kalide/internal/yamlpos"
)

// This file resolves the offending source line of a kalide.yaml YAML parse
// error and, when the failure is the classic plain-scalar quoting problem,
// attaches the quoting hint (requirements.requirement.yaml-error-messages). It
// delegates position resolution to yamlpos.Resolve, the one resolver the three
// loaders share, while keeping the deck package's positioned error form — the
// same file:line: message that deck.LoadConfig uses for every other config
// error — so a malformed kalide.yaml reads as <file>:<line>: <problem> (<hint>)
// exactly like the loader's key/type errors. LoadConfig is its one caller; it
// wires no caller itself.

// yamlErrorWithLine builds the positioned error for a yaml.v3 parse failure on
// a kalide.yaml document. file is the config file's identity used in errors
// (typically ConfigFile), source is the exact bytes that failed to parse, and
// err is the error yaml.Unmarshal returned.
//
// It delegates position resolution to yamlpos.Resolve, which returns the
// 1-based offending source line (line 0 when none can be resolved — reading
// err's "line N:" text, else recovering a lineless undefined-alias line from
// source), the message with yaml.v3's own "yaml: line N: " prefix stripped, and
// the plain-scalar quoting hint when the line at that position contains a plain
// scalar YAML mis-read:
//
//   - a `:` inside a plain scalar, the shape yaml.v3 rejects as "mapping values
//     are not allowed in this context" (for example `title: Hello: world`, where
//     the value `Hello: world` is read as a mapping rather than text); or
//   - a plain scalar whose first character is one of `*`, `&`, `%` or `@`, which
//     YAML reads as an alias, anchor, directive or reserved indicator rather
//     than literal text (for example `author: @presenter`).
//
// The message carries no location prefix, so the positioned error carries the
// file and line once:
//
//	kalide.yaml:3: mapping values are not allowed in this context (quote the value as a string)
//
// It is the single helper LoadConfig's parse-error site calls; it wires no
// caller itself.
func yamlErrorWithLine(file string, source []byte, err error) error {
	line, message, hint := yamlpos.Resolve(source, err)
	if hint != "" {
		message = fmt.Sprintf("%s (%s)", message, hint)
	}
	return positioned(file, line, "%s", message)
}
