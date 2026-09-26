package theme

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// mapFS returns a one-file fake filesystem holding css, so a registry-only
// test theme can carry a stylesheet without touching internal/assets.
func mapFS(name, css string) fstest.MapFS {
	return fstest.MapFS{name: &fstest.MapFile{Data: []byte(css)}}
}

// loadDirRegistry is a small helper building a *Registry from an in-memory
// templates/themes directory via LoadDir, the only way a project registry is
// built now that there is no compiled-in theme.Builtin()/Default().
func loadDirRegistry(t *testing.T, files map[string]string) *Registry {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(data)}
	}
	reg, err := LoadDir(fsys, "templates/themes")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	return reg
}

func TestRegistry(t *testing.T) {
	t.Run("default resolves via LoadDir", func(t *testing.T) {
		reg := loadDirRegistry(t, map[string]string{
			"templates/themes/default/theme.css": "body{}",
		})
		got, err := reg.Lookup(DefaultName)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", DefaultName, err)
		}
		if got.Name != DefaultName {
			t.Fatalf("Lookup(%q).Name = %q, want %q", DefaultName, got.Name, DefaultName)
		}
		if got.Stylesheet != Stylesheet {
			t.Fatalf("theme Stylesheet = %q, want %q", got.Stylesheet, Stylesheet)
		}

		css, err := got.CSS()
		if err != nil {
			t.Fatalf("default theme CSS(): %v", err)
		}
		if len(css) == 0 {
			t.Fatalf("default theme CSS is empty")
		}
	})

	t.Run("registry from LoadDir lists exactly the loaded themes", func(t *testing.T) {
		reg := loadDirRegistry(t, map[string]string{
			"templates/themes/default/theme.css": "body{}",
		})
		if got, want := reg.Names(), []string{DefaultName}; !reflect.DeepEqual(got, want) {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	})

	t.Run("test-registered theme resolves without template changes", func(t *testing.T) {
		reg := loadDirRegistry(t, map[string]string{
			"templates/themes/default/theme.css": "body{}",
		})
		// Registration alone is the extension point: no template or renderer
		// change is involved.
		custom := Theme{
			Name:       "solarized",
			Stylesheet: "solarized.css",
			Assets:     mapFS("solarized.css", ":root{--bg:#002b36}"),
		}
		if err := reg.Register(custom); err != nil {
			t.Fatalf("Register(%q): %v", custom.Name, err)
		}

		got, err := reg.Lookup("solarized")
		if err != nil {
			t.Fatalf("Lookup(solarized): %v", err)
		}
		if got.Name != "solarized" || got.Stylesheet != "solarized.css" {
			t.Fatalf("Lookup(solarized) = %+v, want Name solarized Stylesheet solarized.css", got)
		}
		css, err := got.CSS()
		if err != nil {
			t.Fatalf("solarized CSS(): %v", err)
		}
		if string(css) != ":root{--bg:#002b36}" {
			t.Fatalf("solarized CSS = %q", css)
		}

		if want := []string{DefaultName, "solarized"}; !reflect.DeepEqual(reg.Names(), want) {
			t.Fatalf("Names() = %v, want %v", reg.Names(), want)
		}

		// Another registry built independently must not see it.
		other := NewRegistry()
		if _, err := other.Lookup("solarized"); err == nil {
			t.Fatalf("independent registry resolved solarized; registries must be isolated")
		}
	})

	t.Run("zero-value registry is ready to use", func(t *testing.T) {
		var reg Registry
		custom := Theme{Name: "plain", Assets: mapFS("plain.css", "x")}
		custom.Stylesheet = "plain.css"
		if err := reg.Register(custom); err != nil {
			t.Fatalf("zero-value Register: %v", err)
		}
		if _, err := reg.Lookup("plain"); err != nil {
			t.Fatalf("zero-value Lookup: %v", err)
		}
	})
}

func TestLookupUnknownTheme(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Theme{Name: DefaultName}); err != nil {
		t.Fatalf("Register(default): %v", err)
	}

	t.Run("errors with available themes and closest-match suggestion", func(t *testing.T) {
		_, err := reg.Lookup("defalt")
		if err == nil {
			t.Fatalf("Lookup(defalt) succeeded, want error")
		}

		var unknown *UnknownThemeError
		if !errors.As(err, &unknown) {
			t.Fatalf("Lookup(defalt) error type = %T, want *UnknownThemeError", err)
		}
		if unknown.Name != "defalt" {
			t.Fatalf("error Name = %q, want %q", unknown.Name, "defalt")
		}
		if want := []string{DefaultName}; !reflect.DeepEqual(unknown.Available, want) {
			t.Fatalf("error Available = %v, want %v", unknown.Available, want)
		}
		if unknown.Suggestion != DefaultName {
			t.Fatalf("error Suggestion = %q, want %q", unknown.Suggestion, DefaultName)
		}

		want := `unknown theme "defalt": did you mean "default"? (available themes: default)`
		if got := err.Error(); got != want {
			t.Fatalf("error message = %q, want %q", got, want)
		}

		// The message must actually name both the suggestion and the list.
		for _, wantSub := range []string{"did you mean", DefaultName, "available themes"} {
			if !strings.Contains(err.Error(), wantSub) {
				t.Fatalf("error message %q missing %q", err.Error(), wantSub)
			}
		}
	})

	t.Run("available list is sorted and suggestion names a registered theme", func(t *testing.T) {
		multi := NewRegistry()
		for _, name := range []string{"zebra", DefaultName, "solarized"} {
			if err := multi.Register(Theme{Name: name}); err != nil {
				t.Fatalf("Register(%q): %v", name, err)
			}
		}
		_, err := multi.Lookup("solrized")
		var unknown *UnknownThemeError
		if !errors.As(err, &unknown) {
			t.Fatalf("error type = %T, want *UnknownThemeError", err)
		}
		if want := []string{DefaultName, "solarized", "zebra"}; !reflect.DeepEqual(unknown.Available, want) {
			t.Fatalf("Available = %v, want sorted %v", unknown.Available, want)
		}
		if unknown.Suggestion != "solarized" {
			t.Fatalf("Suggestion = %q, want %q", unknown.Suggestion, "solarized")
		}
		want := `unknown theme "solrized": did you mean "solarized"? (available themes: default, solarized, zebra)`
		if got := err.Error(); got != want {
			t.Fatalf("error message = %q, want %q", got, want)
		}
	})

	t.Run("no suggestion when nothing is close", func(t *testing.T) {
		_, err := reg.Lookup("zzzzzzzzz")
		var unknown *UnknownThemeError
		if !errors.As(err, &unknown) {
			t.Fatalf("error type = %T, want *UnknownThemeError", err)
		}
		if unknown.Suggestion != "" {
			t.Fatalf("Suggestion = %q, want empty", unknown.Suggestion)
		}
		if strings.Contains(err.Error(), "did you mean") {
			t.Fatalf("error message %q must not contain a suggestion", err.Error())
		}
		want := `unknown theme "zzzzzzzzz" (available themes: default)`
		if got := err.Error(); got != want {
			t.Fatalf("error message = %q, want %q", got, want)
		}
	})
}

func TestUnknownThemeErrorWithPosition(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Theme{Name: DefaultName}); err != nil {
		t.Fatalf("Register(default): %v", err)
	}

	_, err := reg.Lookup("defalt")
	var unknown *UnknownThemeError
	if !errors.As(err, &unknown) {
		t.Fatalf("error type = %T, want *UnknownThemeError", err)
	}

	positioned := unknown.WithPosition("kalide.yaml", 3)
	want := `kalide.yaml:3: unknown theme "defalt": did you mean "default"? (available themes: default)`
	if got := positioned.Error(); got != want {
		t.Fatalf("positioned error = %q, want %q", got, want)
	}

	// WithPosition must not mutate the original.
	if strings.Contains(err.Error(), "kalide.yaml") {
		t.Fatalf("WithPosition mutated the original error: %q", err.Error())
	}

	// A file with unknown line omits the line number.
	noLine := unknown.WithPosition("kalide.yaml", 0)
	want = `kalide.yaml: unknown theme "defalt": did you mean "default"? (available themes: default)`
	if got := noLine.Error(); got != want {
		t.Fatalf("positioned error (no line) = %q, want %q", got, want)
	}
}

func TestRegister(t *testing.T) {
	t.Run("empty name is rejected", func(t *testing.T) {
		err := NewRegistry().Register(Theme{})
		if err == nil {
			t.Fatalf("Register(empty name) succeeded, want error")
		}
		if !strings.Contains(err.Error(), "name must not be empty") {
			t.Fatalf("error = %q, want mention of empty name", err)
		}
	})

	t.Run("duplicate name is rejected", func(t *testing.T) {
		reg := NewRegistry()
		if err := reg.Register(Theme{Name: "dupe"}); err != nil {
			t.Fatalf("first Register: %v", err)
		}
		err := reg.Register(Theme{Name: "dupe"})
		if err == nil {
			t.Fatalf("second Register(dupe) succeeded, want error")
		}
		if !strings.Contains(err.Error(), "already registered") {
			t.Fatalf("error = %q, want mention of already registered", err)
		}
	})
}

func TestThemeCSS(t *testing.T) {
	t.Run("nil assets", func(t *testing.T) {
		_, err := Theme{Name: "noassets", Stylesheet: "theme.css"}.CSS()
		if err == nil {
			t.Fatalf("CSS() with nil Assets succeeded, want error")
		}
		if !strings.Contains(err.Error(), "no asset filesystem") {
			t.Fatalf("error = %q, want mention of asset filesystem", err)
		}
	})

	t.Run("empty stylesheet", func(t *testing.T) {
		_, err := Theme{Name: "nostylesheet", Assets: mapFS("x.css", "x")}.CSS()
		if err == nil {
			t.Fatalf("CSS() with empty Stylesheet succeeded, want error")
		}
		if !strings.Contains(err.Error(), "no stylesheet") {
			t.Fatalf("error = %q, want mention of stylesheet", err)
		}
	})

	t.Run("missing stylesheet file", func(t *testing.T) {
		th := Theme{Name: "missing", Stylesheet: "missing.css", Assets: mapFS("other.css", "x")}
		_, err := th.CSS()
		if err == nil {
			t.Fatalf("CSS() with missing file succeeded, want error")
		}
		if !strings.Contains(err.Error(), `theme "missing"`) || !strings.Contains(err.Error(), "missing.css") {
			t.Fatalf("error = %q, want theme and stylesheet named", err)
		}
	})
}

// TestRegistryFromLoadDir proves a project's theme Registry comes from
// LoadDir over its templates/themes directory: a project registry holding
// only project-defined themes resolves them and does not carry any
// compiled-in theme unless the project itself defines one with that name.
func TestRegistryFromLoadDir(t *testing.T) {
	fsys := fstest.MapFS{
		"templates/themes/plain/theme.css": &fstest.MapFile{Data: []byte("body{margin:0}")},
	}
	reg, err := LoadDir(fsys, "templates/themes")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	if want := []string{"plain"}; !reflect.DeepEqual(reg.Names(), want) {
		t.Fatalf("Names() = %v, want %v", reg.Names(), want)
	}

	// The project registry only has what its templates/themes directory
	// defines: no "default" unless the project defines one.
	if _, err := reg.Lookup(DefaultName); err == nil {
		t.Fatalf("project registry resolved %q, which it never defined", DefaultName)
	}

	plain, err := reg.Lookup("plain")
	if err != nil {
		t.Fatalf("Lookup(plain): %v", err)
	}
	css, err := plain.CSS()
	if err != nil || string(css) != "body{margin:0}" {
		t.Fatalf("plain.CSS() = (%q, %v), want (\"body{margin:0}\", nil)", css, err)
	}
}
