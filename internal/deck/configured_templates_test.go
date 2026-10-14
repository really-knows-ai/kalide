package deck

import (
	"testing"
	"testing/fstest"
)

// This file is the unit-test deliverable for plan.phase-03.task-10. It covers
// deck.ConfiguredTemplates — the one raw top-level `templates:` read over an
// fs.FS shared by the reload pipeline, the file watcher and the CLI
// (external-template-library, layered-template-library).
//
// ConfiguredTemplates deliberately does not validate: it returns the value
// exactly as written, and degrades to "" whenever the key cannot be read at all
// — a missing kalide.yaml, undecodable YAML, an absent key, or a non-string
// value. The empty value selects the deck-root templates/ layer. It runs
// in-process over a fake fs.FS, so it holds under -short; configFS and ConfigFile
// come from deck_test.go.
func TestConfiguredTemplates(t *testing.T) {
	t.Run("missing kalide.yaml is empty", func(t *testing.T) {
		if got := ConfiguredTemplates(fstest.MapFS{}); got != "" {
			t.Errorf("ConfiguredTemplates(no kalide.yaml) = %q, want empty", got)
		}
	})

	t.Run("undecodable YAML is empty", func(t *testing.T) {
		for _, yaml := range []string{
			"title: [\n",
			"templates: [\n",
			"title: Deck\ndate: Hello: world\n",
		} {
			if got := ConfiguredTemplates(configFS(yaml)); got != "" {
				t.Errorf("ConfiguredTemplates(%q) = %q, want empty", yaml, got)
			}
		}
	})

	t.Run("absent key is empty", func(t *testing.T) {
		for _, yaml := range []string{
			"",
			"title: D\n",
			"title: D\nauthor: Ada\n",
		} {
			if got := ConfiguredTemplates(configFS(yaml)); got != "" {
				t.Errorf("ConfiguredTemplates(%q) = %q, want empty", yaml, got)
			}
		}
	})

	t.Run("non-string value is empty", func(t *testing.T) {
		// A number, boolean, list or mapping, and a null value, are all read as
		// "no external layer configured" rather than as a type error: the whole
		// deck validator reports the malformed key afterwards.
		for _, yaml := range []string{
			"title: D\ntemplates: 42\n",
			"title: D\ntemplates: true\n",
			"title: D\ntemplates: [a, b]\n",
			"title: D\ntemplates: {path: lib}\n",
			"title: D\ntemplates:\n",
		} {
			if got := ConfiguredTemplates(configFS(yaml)); got != "" {
				t.Errorf("ConfiguredTemplates(%q) = %q, want empty", yaml, got)
			}
		}
	})

	t.Run("valid string is returned as written", func(t *testing.T) {
		// The raw value is captured verbatim — never cleaned, normalised or
		// resolved — so the loader can resolve it against the deck root later.
		for _, tt := range []struct {
			name string
			yaml string
			want string
		}{
			{"relative path", "title: D\ntemplates: ../shared-lib\n", "../shared-lib"},
			{"absolute path", "title: D\ntemplates: /opt/shared-lib\n", "/opt/shared-lib"},
			{"uncleaned path", "title: D\ntemplates: ./libs/../shared/\n", "./libs/../shared/"},
			{"quoted path", "title: D\ntemplates: \"../shared-lib\"\n", "../shared-lib"},
			{"empty string", "title: D\ntemplates: \"\"\n", ""},
		} {
			t.Run(tt.name, func(t *testing.T) {
				if got := ConfiguredTemplates(configFS(tt.yaml)); got != tt.want {
					t.Errorf("ConfiguredTemplates(%q) = %q, want %q", tt.yaml, got, tt.want)
				}
			})
		}
	})
}
