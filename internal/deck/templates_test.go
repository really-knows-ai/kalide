package deck

import "testing"

// This file is the unit-test deliverable for plan.phase-01.task-9: the optional
// top-level `templates:` key on a deck's kalide.yaml — Config.Templates,
// LoadConfig's parsing of it, and the deck-side accessor TemplatesPath.
//
// It exercises LoadConfig in-process over a fake fs.FS, so it runs under
// -short. It reuses configFS, testThemes and wantErr from deck_test.go.

// TestTemplatesKey covers the `templates:` deck key (external-template-library):
// it is accepted and stored exactly as written (relative and absolute), is ""
// when the key is absent or empty, is a positioned type error when present with
// a non-string value, and is now one of the keys an unknown-key closest-match
// suggestion can name.
func TestTemplatesKey(t *testing.T) {
	themes := testThemes(t)

	t.Run("relative path is stored as written", func(t *testing.T) {
		cfg, err := LoadConfig(configFS("title: D\ntemplates: ../shared-lib\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Templates != "../shared-lib" {
			t.Errorf("Config.Templates = %q, want %q", cfg.Templates, "../shared-lib")
		}
		if got := TemplatesPath(cfg); got != "../shared-lib" {
			t.Errorf("TemplatesPath() = %q, want %q", got, "../shared-lib")
		}
	})

	t.Run("absolute path is stored as written", func(t *testing.T) {
		const abs = "/opt/shared-lib"
		cfg, err := LoadConfig(configFS("title: D\ntemplates: "+abs+"\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Templates != abs {
			t.Errorf("Config.Templates = %q, want %q", cfg.Templates, abs)
		}
		if got := TemplatesPath(cfg); got != abs {
			t.Errorf("TemplatesPath() = %q, want %q", got, abs)
		}
	})

	t.Run("path is stored verbatim, not cleaned or normalised", func(t *testing.T) {
		// The loader resolves and cleans the path later
		// (template.ResolveLibraryRoot); LoadConfig must not.
		const written = "./libs/../shared/"
		cfg, err := LoadConfig(configFS("title: D\ntemplates: "+written+"\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Templates != written {
			t.Errorf("Config.Templates = %q, want it kept exactly as written %q", cfg.Templates, written)
		}
	})

	t.Run("absent key is empty", func(t *testing.T) {
		cfg, err := LoadConfig(configFS("title: D\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Templates != "" {
			t.Errorf("Config.Templates = %q, want empty when the key is absent", cfg.Templates)
		}
		if got := TemplatesPath(cfg); got != "" {
			t.Errorf("TemplatesPath() = %q, want empty", got)
		}
	})

	t.Run("null or empty value is empty", func(t *testing.T) {
		for _, yaml := range []string{
			"title: D\ntemplates:\n",
			"title: D\ntemplates: \"\"\n",
		} {
			cfg, err := LoadConfig(configFS(yaml), ConfigFile, themes)
			if err != nil {
				t.Fatalf("LoadConfig(%q): %v", yaml, err)
			}
			if cfg.Templates != "" {
				t.Errorf("Config(%q).Templates = %q, want empty", yaml, cfg.Templates)
			}
		}
	})

	t.Run("wrong type is a positioned error", func(t *testing.T) {
		tests := []struct {
			name string
			yaml string
			want string
		}{
			{"number", "title: D\ntemplates: 42\n", "a number"},
			{"boolean", "title: D\ntemplates: true\n", "a boolean"},
			{"list", "title: D\ntemplates: [a, b]\n", "a list"},
			{"mapping", "title: D\ntemplates: {path: lib}\n", "a mapping"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := LoadConfig(configFS(tt.yaml), ConfigFile, themes)
				wantErr(t, err, "kalide.yaml:2:", `key "templates"`, "expected a string, got "+tt.want)
			})
		}
	})

	t.Run("misspelled templates key suggests templates", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\ntempltes: ../shared-lib\n"), ConfigFile, themes)
		wantErr(t, err, `kalide.yaml:2: unknown key "templtes": did you mean "templates"?`)
	})

	t.Run("singular template key suggests templates", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\ntemplate: ../shared-lib\n"), ConfigFile, themes)
		wantErr(t, err, `unknown key "template": did you mean "templates"?`)
	})

	t.Run("TemplatesPath on a nil config is empty", func(t *testing.T) {
		if got := TemplatesPath(nil); got != "" {
			t.Errorf("TemplatesPath(nil) = %q, want empty", got)
		}
	})
}
