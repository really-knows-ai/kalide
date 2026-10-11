// This file implements the kalide-owned author-guide pair writer: the full
// guide AGENTS_KALIDE.md plus its AGENTS.md pointer
// (requirements.requirement.agent-authoring,
// requirements.requirement.agent-guide-reference,
// solution.constraint.agent-guide-unbranded-inert).
//
// Every scaffolding command delivers the pair: `kalide init` and
// `kalide init <path>` at the deck root, `kalide init-library` at the library
// root. The two forms share this one implementation and differ only in the
// embedded bytes handed to it. The deck form's sources are embedded here
// (seed/AGENTS_KALIDE.md and seed/AGENTS.md); the library form's sources are
// embedded in libraryguide.go (libraryguide/AGENTS_KALIDE.md and
// libraryguide/AGENTS.md) and are read from there, so this file adds no
// directive that duplicates an existing embed.
//
// writeAgentGuidePair and ensureAgentGuidePointer are deliberately generic
// over the guide bytes. This file does NOT wire them into Init, InitExternal or
// InitLibrary: those callers pass their form's embedded bytes.
package scaffold

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// agentGuideFullGuideName is the file name of the full guide the pair writes,
// and the target of the pointer's fixed reference line below. The pointer's own
// file name is agentsGuideName ("AGENTS.md"), already declared in refresh.go.
const agentGuideFullGuideName = "AGENTS_KALIDE.md"

// agentGuideReferenceLine is the exact reference line the AGENTS.md pointer
// carries (agent-guide-reference): its presence is what marks a pointer as
// already referencing the full guide, and it is the line appended to an author
// AGENTS.md that does not carry it.
const agentGuideReferenceLine = "Kalide Reference: ./AGENTS_KALIDE.md"

// The embedded deck author-guide pair: the full guide `kalide init` /
// `kalide init <path>` writes verbatim, and the short pointer that references
// it. The library form's pair is embedded in libraryguide.go, so the deck and
// library sources never share an embed variable here.
//
//go:embed seed/AGENTS_KALIDE.md
var deckAgentGuideFull []byte

//go:embed seed/AGENTS.md
var deckAgentGuidePointer []byte

// writeAgentGuidePair writes a kalide-owned author-guide pair into dir: the
// full guide fullGuide at AGENTS_KALIDE.md and, through
// ensureAgentGuidePointer, the pointer pointer at AGENTS.md.
//
// The full guide is written verbatim, but ONLY when no AGENTS_KALIDE.md exists
// at dir: a pre-existing one is treated as author content and left byte-for-byte
// untouched (upgrade-never-clobbers). The pointer is then created, left
// untouched or appended per the agent-guide-reference scheme. dir is created as
// needed; nothing else is written.
func writeAgentGuidePair(dir string, fullGuide, pointer []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("scaffold: create %s: %w", dir, err)
	}

	fullTarget := filepath.Join(dir, agentGuideFullGuideName)
	_, err := os.Lstat(fullTarget)
	switch {
	case err == nil:
		// Present: leave the full guide byte-for-byte untouched.
	case errors.Is(err, fs.ErrNotExist):
		if writeErr := os.WriteFile(fullTarget, fullGuide, 0o644); writeErr != nil {
			return fmt.Errorf("scaffold: write %s: %w", agentGuideFullGuideName, writeErr)
		}
	default:
		return fmt.Errorf("scaffold: inspect %s: %w", agentGuideFullGuideName, err)
	}

	return ensureAgentGuidePointer(dir, pointer)
}

// ensureAgentGuidePointer brings dir's AGENTS.md to the agent-guide-reference
// scheme, preserving the author's bytes:
//
//   - absent — created carrying the fixed embedded pointer content (pointer);
//   - present and already containing the reference line
//     "Kalide Reference: ./AGENTS_KALIDE.md" — left byte-for-byte untouched;
//   - present without the reference line — the reference line is appended,
//     with the author's bytes preserved and a newline separating them from the
//     appended line (see appendAgentGuideReference).
//
// Presence is tested with os.Lstat, so a file, a directory or a symlink all
// count as present, matching the rest of the package's "present even if
// dangling" rule. A present non-file (a directory) fails on read rather than
// being replaced.
func ensureAgentGuidePointer(dir string, pointer []byte) error {
	target := filepath.Join(dir, agentsGuideName)

	_, err := os.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if writeErr := os.WriteFile(target, pointer, 0o644); writeErr != nil {
			return fmt.Errorf("scaffold: write %s: %w", agentsGuideName, writeErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("scaffold: inspect %s: %w", agentsGuideName, err)
	}

	existing, readErr := os.ReadFile(target)
	if readErr != nil {
		return fmt.Errorf("scaffold: read %s: %w", agentsGuideName, readErr)
	}
	if bytes.Contains(existing, []byte(agentGuideReferenceLine)) {
		return nil
	}
	if writeErr := os.WriteFile(target, appendAgentGuideReference(existing), 0o644); writeErr != nil {
		return fmt.Errorf("scaffold: write %s: %w", agentsGuideName, writeErr)
	}
	return nil
}

// appendAgentGuideReference returns existing with the pointer's reference line
// appended: the author's bytes are preserved verbatim and a single newline
// separates them from the appended line, which is itself newline-terminated. An
// empty file gains only the reference line.
func appendAgentGuideReference(existing []byte) []byte {
	line := []byte(agentGuideReferenceLine)
	out := make([]byte, 0, len(existing)+len(line)+2)
	out = append(out, existing...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, line...)
	out = append(out, '\n')
	return out
}
