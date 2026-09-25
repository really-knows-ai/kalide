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

func TestRegistry(t *testing.T) {
	t.Run("default resolves", func(t *testing.T) {
		got, err := Builtin().Lookup(DefaultName)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", DefaultName, err)
		}
		if got.Name != DefaultName {
			t.Fatalf("Lookup(%q).Name = %q, want %q", DefaultName, got.Name, DefaultName)
		}
		if got.Stylesheet != defaultStylesheet {
			t.Fatalf("built-in theme Stylesheet = %q, want %q", got.Stylesheet, defaultStylesheet)
		}

		// The built-in default is the phase-1 embedded stylesheet, so its CSS
		// must be readable and non-empty.
		css, err := got.CSS()
		if err != nil {
			t.Fatalf("default theme CSS(): %v", err)
		}
		if len(css) == 0 {
			t.Fatalf("default theme CSS is empty")
		}

		if def := Default(); def.Name != DefaultName || def.Stylesheet != defaultStylesheet {
			t.Fatalf("Default() = %+v, want Name %q Stylesheet %q", def, DefaultName, defaultStylesheet)
		}
	})

	t.Run("built-in registry lists only default", func(t *testing.T) {
		if got, want := Names(), []string{DefaultName}; !reflect.DeepEqual(got, want) {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	})

	t.Run("package-level Lookup resolves default", func(t *testing.T) {
		got, err := Lookup(DefaultName)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", DefaultName, err)
		}
		if got.Name != DefaultName {
			t.Fatalf("Lookup(%q).Name = %q", DefaultName, got.Name)
		}
	})

	t.Run("test-registered theme resolves without template changes", func(t *testing.T) {
		reg := NewRegistry()
		if err := reg.Register(Default()); err != nil {
			t.Fatalf("Register(default): %v", err)
		}
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

		// The isolated registry must not leak into the built-in one.
		if _, err := Builtin().Lookup("solarized"); err == nil {
			t.Fatalf("Builtin().Lookup(solarized) succeeded; isolated registry leaked")
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
	if err := reg.Register(Default()); err != nil {
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
	_, err := Builtin().Lookup("defalt")
	var unknown *UnknownThemeError
	if !errors.As(err, &unknown) {
		t.Fatalf("error type = %T, want *UnknownThemeError", err)
	}

	positioned := unknown.WithPosition("eypres.yaml", 3)
	want := `eypres.yaml:3: unknown theme "defalt": did you mean "default"? (available themes: default)`
	if got := positioned.Error(); got != want {
		t.Fatalf("positioned error = %q, want %q", got, want)
	}

	// WithPosition must not mutate the original.
	if strings.Contains(err.Error(), "eypres.yaml") {
		t.Fatalf("WithPosition mutated the original error: %q", err.Error())
	}

	// A file with unknown line omits the line number.
	noLine := unknown.WithPosition("eypres.yaml", 0)
	want = `eypres.yaml: unknown theme "defalt": did you mean "default"? (available themes: default)`
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

	t.Run("default is already registered in the builtin registry", func(t *testing.T) {
		if err := Register(Default()); err == nil {
			t.Fatalf("Register(default) succeeded, want duplicate error")
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
