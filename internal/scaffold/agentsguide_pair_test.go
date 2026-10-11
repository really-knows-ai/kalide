package scaffold

// This file holds TestInitWritesAgentGuidePair, the cross-form unit test of the
// kalide-owned author-guide pair writer (agent-authoring,
// agent-guide-reference): `kalide init`, `kalide init <path>` (InitExternal) and
// `kalide init-library` each deliver the pair. The full AGENTS_KALIDE.md guide
// is written verbatim but ONLY when absent — a pre-existing one is author
// content left byte-for-byte untouched — and the AGENTS.md pointer is brought to
// the reference-line scheme: created carrying the embedded pointer content when
// absent, left byte-for-byte untouched when it already carries
// "Kalide Reference: ./AGENTS_KALIDE.md", and otherwise appended with that
// reference line while the author's bytes are preserved. The created
// deck/library still loads: the pair is inert.
//
// The three forms differ only in the embedded bytes and the loader that reaches
// the created artifact; the scheme itself is form-independent, so one form table
// drives the fresh/preserve/pointer/load subtests.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/template"
)

// agentGuidePairForm is one scaffolding entry point delivering the author-guide
// pair: how to scaffold a target directory, the embedded full-guide and pointer
// bytes it must deliver, and the loader that proves the created artifact (with
// the pair present) still loads.
type agentGuidePairForm struct {
	name      string
	dirName   string // base name the target directory needs for this form
	fullGuide []byte // the embedded full guide the form writes to AGENTS_KALIDE.md
	pointer   []byte // the embedded pointer the form writes to AGENTS.md
	scaffold  func(t *testing.T, dir string) error
	load      func(t *testing.T, dir string) error
}

// TestInitWritesAgentGuidePair asserts the author-guide pair rules across the
// three scaffolding forms: the full guide is written only when absent, the
// AGENTS.md pointer follows the reference-line scheme, and the created
// deck/library still loads with the pair (the pair is inert).
func TestInitWritesAgentGuidePair(t *testing.T) {
	forms := agentGuidePairForms(t)

	t.Run("fresh scaffold writes the full guide and the pointer", func(t *testing.T) {
		for _, form := range forms {
			t.Run(form.name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), form.dirName)
				if err := form.scaffold(t, dir); err != nil {
					t.Fatalf("%s: scaffold error = %v, want nil", form.name, err)
				}
				assertFullGuideEquals(t, dir, form.fullGuide)
				assertPointerEquals(t, dir, form.pointer)
				if err := form.load(t, dir); err != nil {
					t.Errorf("%s: load error = %v, want the created artifact to load with the pair", form.name, err)
				}
			})
		}
	})

	t.Run("a pre-existing full guide is left byte-for-byte untouched", func(t *testing.T) {
		for _, form := range forms {
			t.Run(form.name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), form.dirName)
				mkdir(t, dir)
				const sentinel = "# an author's own full guide\n\nkeep me byte-for-byte.\n"
				writeFile(t, filepath.Join(dir, agentGuideFullGuideName), sentinel)

				if err := form.scaffold(t, dir); err != nil {
					t.Fatalf("%s: scaffold error = %v, want nil", form.name, err)
				}

				if got := readString(t, filepath.Join(dir, agentGuideFullGuideName)); got != sentinel {
					t.Errorf("%s: AGENTS_KALIDE.md = %q, want the pre-existing full guide byte-for-byte unchanged", form.name, got)
				}
				// The full guide is written ONLY when absent; the pointer is
				// still brought to the reference-line scheme.
				assertPointerEquals(t, dir, form.pointer)
				if err := form.load(t, dir); err != nil {
					t.Errorf("%s: load error = %v, want the created artifact to load", form.name, err)
				}
			})
		}
	})

	t.Run("the AGENTS.md pointer follows the reference-line scheme", func(t *testing.T) {
		cases := []struct {
			name    string
			initial string
			want    string
		}{
			{
				name:    "author content without the reference line is appended",
				initial: "# my own guide\n\nkeep me exactly as I am.\n",
				want:    "# my own guide\n\nkeep me exactly as I am.\n" + agentGuideReferenceLine + "\n",
			},
			{
				name:    "author content not newline-terminated is appended after a separating newline",
				initial: "keep me exactly as I am",
				want:    "keep me exactly as I am\n" + agentGuideReferenceLine + "\n",
			},
			{
				name:    "an empty AGENTS.md gains only the reference line",
				initial: "",
				want:    agentGuideReferenceLine + "\n",
			},
			{
				name:    "author content already carrying the reference line is untouched",
				initial: "# my own guide\n\n" + agentGuideReferenceLine + "\nextra notes stay put.\n",
				want:    "# my own guide\n\n" + agentGuideReferenceLine + "\nextra notes stay put.\n",
			},
		}

		for _, form := range forms {
			for _, tc := range cases {
				t.Run(form.name+"/"+tc.name, func(t *testing.T) {
					dir := filepath.Join(t.TempDir(), form.dirName)
					mkdir(t, dir)
					writeFile(t, filepath.Join(dir, agentsGuideName), tc.initial)

					if err := form.scaffold(t, dir); err != nil {
						t.Fatalf("%s: scaffold error = %v, want nil", form.name, err)
					}

					if got := readString(t, filepath.Join(dir, agentsGuideName)); got != tc.want {
						t.Errorf("%s: AGENTS.md = %q, want %q", form.name, got, tc.want)
					}
					// The full guide is written when absent.
					assertFullGuideEquals(t, dir, form.fullGuide)
					if err := form.load(t, dir); err != nil {
						t.Errorf("%s: load error = %v, want the created artifact to load", form.name, err)
					}
				})
			}
		}
	})
}

// agentGuidePairForms returns the three scaffolding entry points that deliver
// the author-guide pair, each paired with the embedded bytes it must write and a
// loader that reaches the created artifact.
func agentGuidePairForms(t *testing.T) []agentGuidePairForm {
	t.Helper()

	deckFull := mustReadSeed(agentGuideFullGuideName)
	deckPointer := mustReadSeed(agentsGuideName)
	libraryFull := mustReadLibraryFullGuide()
	libraryPointer := mustReadLibraryGuide()

	return []agentGuidePairForm{
		{
			name:      "init",
			dirName:   "deck",
			fullGuide: deckFull,
			pointer:   deckPointer,
			scaffold: func(t *testing.T, dir string) error {
				return Init(dir)
			},
			load: func(t *testing.T, dir string) error {
				_, err := template.LoadLibrary(os.DirFS(dir), template.TemplatesDir)
				return err
			},
		},
		{
			name:      "init <path>",
			dirName:   "deck",
			fullGuide: deckFull,
			pointer:   deckPointer,
			scaffold: func(t *testing.T, dir string) error {
				writeExternalLibrary(t, filepath.Join(filepath.Dir(dir), "shared-lib"), "shared-lib")
				return InitExternal(dir, "../shared-lib")
			},
			load: func(t *testing.T, dir string) error {
				_, templates := externalConfigOf(t, dir)
				resolved := filepath.Clean(filepath.Join(dir, templates))
				_, err := template.LoadLibraryAt(resolved)
				return err
			},
		},
		{
			name:      "init-library",
			dirName:   "my-lib",
			fullGuide: libraryFull,
			pointer:   libraryPointer,
			scaffold: func(t *testing.T, dir string) error {
				return InitLibrary(dir)
			},
			load: func(t *testing.T, dir string) error {
				_, err := template.LoadLibrary(os.DirFS(dir), ".")
				return err
			},
		},
	}
}

// assertFullGuideEquals asserts dir's AGENTS_KALIDE.md is byte-for-byte the
// embedded full guide the form delivers, and that the embedded full guide is
// non-empty (so the check cannot pass vacuously).
func assertFullGuideEquals(t *testing.T, dir string, want []byte) {
	t.Helper()

	if len(want) == 0 {
		t.Fatal("embedded full guide is empty, want non-empty bytes")
	}
	if got := readString(t, filepath.Join(dir, agentGuideFullGuideName)); got != string(want) {
		t.Errorf("%s differs from the embedded full guide:\n got %q\nwant %q", agentGuideFullGuideName, got, want)
	}
}

// assertPointerEquals asserts dir's AGENTS.md is byte-for-byte the embedded
// pointer the form delivers and that the pointer carries the exact reference
// line the scheme keys on.
func assertPointerEquals(t *testing.T, dir string, want []byte) {
	t.Helper()

	if len(want) == 0 {
		t.Fatal("embedded pointer is empty, want non-empty bytes")
	}
	if !strings.Contains(string(want), agentGuideReferenceLine) {
		t.Fatalf("embedded pointer does not carry %q", agentGuideReferenceLine)
	}
	got := readString(t, filepath.Join(dir, agentsGuideName))
	if got != string(want) {
		t.Errorf("%s differs from the embedded pointer:\n got %q\nwant %q", agentsGuideName, got, want)
	}
	if !strings.Contains(got, agentGuideReferenceLine) {
		t.Errorf("%s = %q, want it to carry the reference line %q", agentsGuideName, got, agentGuideReferenceLine)
	}
}
