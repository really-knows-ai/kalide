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
