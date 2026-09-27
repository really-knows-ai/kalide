package deck

import (
	"errors"
	"path"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/really-knows-ai/kalide/internal/theme"
)

// TestDeck covers internal/deck's two loaders through their real exported API:
// LoadConfig (deck-config rules) and LoadSlides (slide-file-ordering,
// deck-slide-identity, vertical-slides, unique-slide-labels). Both tiers run
// in-process on a fake filesystem, so they hold under -short.
func TestDeck(t *testing.T) {
	t.Run("constants", testDeckConstants)
	t.Run("config", testLoadConfig)
	t.Run("slides", testLoadSlides)
}

// testDeckConstants pins the fixed names and navigation vocabulary the loaders
// rely on.
func testDeckConstants(t *testing.T) {
	if ConfigFile != "kalide.yaml" {
		t.Errorf("ConfigFile = %q, want %q", ConfigFile, "kalide.yaml")
	}
	if SlidesDir != "slides" {
		t.Errorf("SlidesDir = %q, want %q", SlidesDir, "slides")
	}
	if NavigationDefault != "default" || NavigationLinear != "linear" || NavigationGrid != "grid" {
		t.Errorf("Navigation constants = %q, %q, %q; want default, linear, grid",
			NavigationDefault, NavigationLinear, NavigationGrid)
	}
}

// configFS returns a fake deck filesystem whose kalide.yaml holds yaml.
func configFS(yaml string) fstest.MapFS {
	return fstest.MapFS{ConfigFile: &fstest.MapFile{Data: []byte(yaml)}}
}

// testThemes returns a small in-memory theme registry with "default" and one
// named project theme ("sunset"), the fixture testLoadConfig and
// TestLoadConfigThemeRegistry resolve the `theme` key against.
func testThemes(t *testing.T) *theme.Registry {
	t.Helper()
	reg := theme.NewRegistry()
	if err := reg.Register(theme.Theme{Name: theme.DefaultName, Stylesheet: "theme.css"}); err != nil {
		t.Fatalf("register default theme: %v", err)
	}
	if err := reg.Register(theme.Theme{Name: "sunset", Stylesheet: "theme.css"}); err != nil {
		t.Fatalf("register sunset theme: %v", err)
	}
	return reg
}

// wantErr fails unless err is non-nil and its message contains every want
// substring.
func wantErr(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("got nil error, want an error")
	}
	for _, w := range wants {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), w)
		}
	}
}

func testLoadConfig(t *testing.T) {
	themes := testThemes(t)

	t.Run("full config resolves every key", func(t *testing.T) {
		cfg, err := LoadConfig(configFS(
			"title: EY Deck\n"+
				"author: Ada Lovelace\n"+
				"date: 2026-09-25\n"+
				"theme: default\n"+
				"navigation: grid\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		// Config now carries a map field, so it is not comparable with ==;
		// reflect.DeepEqual also pins the always-non-nil Properties contract
		// (a nil map is not deeply equal to the empty non-nil one).
		want := Config{
			Title:      "EY Deck",
			Author:     "Ada Lovelace",
			Date:       "2026-09-25",
			Theme:      "default",
			Navigation: NavigationGrid,
			Properties: map[string]any{},
		}
		if !reflect.DeepEqual(*cfg, want) {
			t.Errorf("Config = %+v, want %+v", *cfg, want)
		}
	})

	t.Run("only title is required", func(t *testing.T) {
		cfg, err := LoadConfig(configFS("title: Minimal\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		want := Config{
			Title:      "Minimal",
			Theme:      theme.DefaultName,
			Navigation: NavigationDefault,
			Properties: map[string]any{},
		}
		if !reflect.DeepEqual(*cfg, want) {
			t.Errorf("Config = %+v, want %+v", *cfg, want)
		}
	})

	t.Run("author and date are optional", func(t *testing.T) {
		cfg, err := LoadConfig(configFS("title: Minimal\nauthor: Ada\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Author != "Ada" {
			t.Errorf("Author = %q, want %q", cfg.Author, "Ada")
		}
		if cfg.Date != "" {
			t.Errorf("Date = %q, want empty when omitted", cfg.Date)
		}
	})

	t.Run("title required when file is empty", func(t *testing.T) {
		_, err := LoadConfig(configFS(""), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:1:", `missing required key "title"`)
	})

	t.Run("title required when omitted but other keys present", func(t *testing.T) {
		_, err := LoadConfig(configFS("author: Ada\n"), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:1:", `missing required key "title"`)
	})

	t.Run("blank title is missing", func(t *testing.T) {
		_, err := LoadConfig(configFS("author: Ada\ntitle: \"  \"\n"), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:2:", `missing required key "title"`)
	})

	t.Run("null title is missing", func(t *testing.T) {
		_, err := LoadConfig(configFS("title:\n"), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:1:", `missing required key "title"`)
	})

	t.Run("valid dates are kept", func(t *testing.T) {
		for _, date := range []string{"2026-09-25", "2000-01-01", "1999-12-31"} {
			cfg, err := LoadConfig(configFS("title: D\ndate: "+date+"\n"), ConfigFile, themes)
			if err != nil {
				t.Fatalf("LoadConfig(date %s): %v", date, err)
			}
			if cfg.Date != date {
				t.Errorf("Date = %q, want %q", cfg.Date, date)
			}
		}
	})

	t.Run("invalid date reports YYYY-MM-DD", func(t *testing.T) {
		for _, date := range []string{"25/09/2026", "2026-13-01", "2026-09-25T10:00", "Sep 25 2026"} {
			_, err := LoadConfig(configFS("title: D\ndate: "+date+"\n"), ConfigFile, themes)
			wantErr(t, err, `key "date"`, "not a valid date, expected YYYY-MM-DD")
		}
	})

	t.Run("invalid date carries file:line", func(t *testing.T) {
		_, err := LoadConfig(configFS(
			"title: Deck\n"+
				"author: Ada\n"+
				"date: 25/09/2026\n"), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:3:")
	})

	t.Run("non-string scalar date is rejected", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\ndate: 2026\n"), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:2:", `key "date"`, "not a valid date")
	})

	t.Run("empty or null date is allowed", func(t *testing.T) {
		for _, yaml := range []string{"title: D\ndate: \"\"\n", "title: D\ndate:\n"} {
			cfg, err := LoadConfig(configFS(yaml), ConfigFile, themes)
			if err != nil {
				t.Fatalf("LoadConfig(%q): %v", yaml, err)
			}
			if cfg.Date != "" {
				t.Errorf("Date = %q, want empty", cfg.Date)
			}
		}
	})

	t.Run("navigation values accepted", func(t *testing.T) {
		for _, mode := range []string{NavigationDefault, NavigationLinear, NavigationGrid} {
			cfg, err := LoadConfig(configFS("title: D\nnavigation: "+mode+"\n"), ConfigFile, themes)
			if err != nil {
				t.Fatalf("LoadConfig(navigation %s): %v", mode, err)
			}
			if cfg.Navigation != mode {
				t.Errorf("Navigation = %q, want %q", cfg.Navigation, mode)
			}
		}
	})

	t.Run("navigation omitted or empty defaults", func(t *testing.T) {
		for _, yaml := range []string{"title: D\n", "title: D\nnavigation:\n", "title: D\nnavigation: \"\"\n"} {
			cfg, err := LoadConfig(configFS(yaml), ConfigFile, themes)
			if err != nil {
				t.Fatalf("LoadConfig(%q): %v", yaml, err)
			}
			if cfg.Navigation != NavigationDefault {
				t.Errorf("Navigation = %q, want %q", cfg.Navigation, NavigationDefault)
			}
		}
	})

	t.Run("unknown navigation mode is rejected", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\nnavigation: diagonal\n"), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:2:", `unknown navigation mode "diagonal"`, "default, linear, grid")
	})

	t.Run("navigation wrong type is rejected", func(t *testing.T) {
		for _, yaml := range []string{"title: D\nnavigation: true\n", "title: D\nnavigation: [grid]\n"} {
			_, err := LoadConfig(configFS(yaml), ConfigFile, themes)
			wantErr(t, err, `key "navigation"`, "expected a string")
		}
	})

	t.Run("title wrong type is rejected", func(t *testing.T) {
		tests := []struct {
			name string
			yaml string
			want string
		}{
			{"number", "title: 42\n", "a number"},
			{"boolean", "title: true\n", "a boolean"},
			{"list", "title: [a, b]\n", "a list"},
			{"mapping", "title: {a: b}\n", "a mapping"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := LoadConfig(configFS(tt.yaml), ConfigFile, themes)
				wantErr(t, err, "kalide.yaml:1:", `key "title"`, "expected a string, got "+tt.want)
			})
		}
	})

	t.Run("author wrong type is rejected", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\nauthor: 42\n"), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:2:", `key "author"`, "expected a string, got a number")
	})

	t.Run("theme wrong type is rejected", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\ntheme: 42\n"), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:2:", `key "theme"`, "expected a string, got a number")
	})

	t.Run("theme omitted, empty or null resolves to default", func(t *testing.T) {
		for _, yaml := range []string{
			"title: D\n",
			"title: D\ntheme: default\n",
			"title: D\ntheme: \"\"\n",
			"title: D\ntheme:\n",
		} {
			cfg, err := LoadConfig(configFS(yaml), ConfigFile, themes)
			if err != nil {
				t.Fatalf("LoadConfig(%q): %v", yaml, err)
			}
			if cfg.Theme != theme.DefaultName {
				t.Errorf("Theme = %q, want %q", cfg.Theme, theme.DefaultName)
			}
		}
	})

	t.Run("named project theme resolves", func(t *testing.T) {
		cfg, err := LoadConfig(configFS("title: D\ntheme: sunset\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Theme != "sunset" {
			t.Errorf("Theme = %q, want %q", cfg.Theme, "sunset")
		}
	})

	t.Run("unknown theme is positioned and suggests", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\ntheme: defalt\n"), ConfigFile, themes)
		wantErr(t, err,
			`kalide.yaml:2: unknown theme "defalt": did you mean "default"? (available themes: default, sunset)`)

		var unknown *theme.UnknownThemeError
		if !errors.As(err, &unknown) {
			t.Fatalf("error type = %T, want *theme.UnknownThemeError", err)
		}
		if unknown.Name != "defalt" || unknown.Suggestion != theme.DefaultName {
			t.Errorf("unknown = %+v, want Name defalt Suggestion default", *unknown)
		}
		if unknown.Position.File != ConfigFile || unknown.Position.Line != 2 {
			t.Errorf("Position = %+v, want %s:2", unknown.Position, ConfigFile)
		}
	})

	t.Run("properties scalar values resolve typed", func(t *testing.T) {
		cfg, err := LoadConfig(configFS(
			"title: D\n"+
				"properties:\n"+
				"  audience: exec\n"+
				"  slides: 42\n"+
				"  ratio: 1.5\n"+
				"  draft: true\n"+
				"  presented_on: 2026-09-25\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Properties == nil {
			t.Fatal("Properties = nil, want a non-nil map")
		}
		if got := len(cfg.Properties); got != 5 {
			t.Fatalf("len(Properties) = %d, want 5", got)
		}
		if v, ok := cfg.Properties["audience"].(string); !ok || v != "exec" {
			t.Errorf("audience = %#v, want string %q", cfg.Properties["audience"], "exec")
		}
		if v, ok := cfg.Properties["slides"].(int); !ok || v != 42 {
			t.Errorf("slides = %#v, want int 42", cfg.Properties["slides"])
		}
		if v, ok := cfg.Properties["ratio"].(float64); !ok || v != 1.5 {
			t.Errorf("ratio = %#v, want float64 1.5", cfg.Properties["ratio"])
		}
		if v, ok := cfg.Properties["draft"].(bool); !ok || !v {
			t.Errorf("draft = %#v, want bool true", cfg.Properties["draft"])
		}
		if v, ok := cfg.Properties["presented_on"].(time.Time); !ok {
			t.Fatalf("presented_on = %#v (%T), want time.Time",
				cfg.Properties["presented_on"], cfg.Properties["presented_on"])
		} else if want := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC); !v.Equal(want) {
			t.Errorf("presented_on = %v, want %v", v, want)
		}
	})

	t.Run("properties mapping or sequence value names the key", func(t *testing.T) {
		tests := []struct {
			name string
			yaml string
			want string
		}{
			{"mapping", "title: D\nproperties:\n  team:\n    name: x\n", "a mapping"},
			{"sequence", "title: D\nproperties:\n  team:\n    - a\n    - b\n", "a list"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := LoadConfig(configFS(tt.yaml), ConfigFile, themes)
				wantErr(t, err, "kalide.yaml:3:", `key "team"`,
					"expected a string, number, boolean or date, got "+tt.want)
			})
		}
	})

	t.Run("properties absent, null or empty is a non-nil empty map", func(t *testing.T) {
		for _, yaml := range []string{
			"title: D\n",
			"title: D\nproperties:\n",
			"title: D\nproperties: {}\n",
		} {
			cfg, err := LoadConfig(configFS(yaml), ConfigFile, themes)
			if err != nil {
				t.Fatalf("LoadConfig(%q): %v", yaml, err)
			}
			if cfg.Properties == nil {
				t.Errorf("Config(%q).Properties = nil, want a non-nil empty map", yaml)
				continue
			}
			if len(cfg.Properties) != 0 {
				t.Errorf("Config(%q).Properties = %#v, want empty", yaml, cfg.Properties)
			}
		}
	})

	t.Run("misspelled config key suggests closest", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\ntitel: Nope\n"), ConfigFile, themes)
		wantErr(t, err, `kalide.yaml:2: unknown key "titel": did you mean "title"?`)
	})

	t.Run("misspelled navigation key suggests navigation", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\nnavigaton: grid\n"), ConfigFile, themes)
		wantErr(t, err, `unknown key "navigaton": did you mean "navigation"?`)
	})

	t.Run("misspelled properties key suggests properties", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\npropertes: {}\n"), ConfigFile, themes)
		wantErr(t, err, `kalide.yaml:2: unknown key "propertes": did you mean "properties"?`)
	})

	t.Run("far-off unknown key has no suggestion", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\nzzzzzzzz: x\n"), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:2:", `unknown key "zzzzzzzz"`)
		if strings.Contains(err.Error(), "did you mean") {
			t.Errorf("error = %q, want no suggestion", err.Error())
		}
	})

	t.Run("non-mapping root is rejected", func(t *testing.T) {
		_, err := LoadConfig(configFS("- one\n- two\n"), ConfigFile, themes)
		wantErr(t, err, "kalide.yaml:1:", "expected a mapping of config keys, got a list")
	})

	t.Run("malformed yaml reports the file", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: [\n"), ConfigFile, themes)
		wantErr(t, err, ConfigFile)
	})

	t.Run("missing config file", func(t *testing.T) {
		_, err := LoadConfig(fstest.MapFS{}, ConfigFile, themes)
		wantErr(t, err, ConfigFile)
	})

	t.Run("dir with only eypres.yaml is treated as no deck config", func(t *testing.T) {
		fsys := fstest.MapFS{
			"eypres.yaml": &fstest.MapFile{Data: []byte("title: Old Deck\n")},
		}
		_, err := LoadConfig(fsys, ConfigFile, themes)
		if err == nil {
			t.Fatal("LoadConfig succeeded on dir with only eypres.yaml, want error")
		}
		wantErr(t, err, ConfigFile)
	})

	t.Run("nil theme registry is rejected", func(t *testing.T) {
		_, err := LoadConfig(configFS("title: D\n"), ConfigFile, nil)
		wantErr(t, err, ConfigFile, "no theme registry given")
	})
}

// TestLoadConfigThemeRegistry covers LoadConfig's theme resolution against an
// in-memory project theme registry (theme-selection, deck-config): an
// omitted `theme` key resolves to "default"; a named project theme resolves;
// an unknown name is a positioned UnknownThemeError with a closest-match
// suggestion; and a registry lacking "default" with no `theme` key is a
// positioned error rather than a panic.
func TestLoadConfigThemeRegistry(t *testing.T) {
	t.Run("no theme key resolves to default", func(t *testing.T) {
		themes := testThemes(t)
		cfg, err := LoadConfig(configFS("title: D\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Theme != theme.DefaultName {
			t.Errorf("Theme = %q, want %q", cfg.Theme, theme.DefaultName)
		}
	})

	t.Run("named project theme resolves", func(t *testing.T) {
		themes := testThemes(t)
		cfg, err := LoadConfig(configFS("title: D\ntheme: sunset\n"), ConfigFile, themes)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Theme != "sunset" {
			t.Errorf("Theme = %q, want %q", cfg.Theme, "sunset")
		}
	})

	t.Run("unknown theme is positioned with closest-match suggestion", func(t *testing.T) {
		themes := testThemes(t)
		_, err := LoadConfig(configFS("title: D\ntheme: sunet\n"), ConfigFile, themes)
		var unknown *theme.UnknownThemeError
		if !errors.As(err, &unknown) {
			t.Fatalf("error type = %T, want *theme.UnknownThemeError", err)
		}
		if unknown.Name != "sunet" || unknown.Suggestion != "sunset" {
			t.Errorf("unknown = %+v, want Name sunet Suggestion sunset", *unknown)
		}
		if unknown.Position.File != ConfigFile || unknown.Position.Line != 2 {
			t.Errorf("Position = %+v, want %s:2", unknown.Position, ConfigFile)
		}
	})

	t.Run("registry lacking default and no theme key is a positioned error", func(t *testing.T) {
		empty := theme.NewRegistry()
		if err := empty.Register(theme.Theme{Name: "sunset"}); err != nil {
			t.Fatalf("register sunset theme: %v", err)
		}
		_, err := LoadConfig(configFS("title: D\n"), ConfigFile, empty)
		if err == nil {
			t.Fatal("LoadConfig: got nil error, want an unknown-default-theme error")
		}
		var unknown *theme.UnknownThemeError
		if !errors.As(err, &unknown) {
			t.Fatalf("error type = %T, want *theme.UnknownThemeError", err)
		}
		if unknown.Name != theme.DefaultName {
			t.Errorf("unknown.Name = %q, want %q", unknown.Name, theme.DefaultName)
		}
		if unknown.Position.File != ConfigFile || unknown.Position.Line != 1 {
			t.Errorf("Position = %+v, want %s:1", unknown.Position, ConfigFile)
		}
	})
}

// slideFS returns a fake deck filesystem with the named files under slides/.
func slideFS(names ...string) fstest.MapFS {
	m := make(fstest.MapFS, len(names))
	for _, n := range names {
		m[path.Join(SlidesDir, n)] = &fstest.MapFile{Data: []byte("# " + n + "\n")}
	}
	return m
}

// deckNumbers returns the ordered horizontal slide numbers.
func deckNumbers(d *Deck) []int {
	nums := make([]int, len(d.Stacks))
	for i, s := range d.Stacks {
		nums[i] = s.Slide.Number
	}
	return nums
}

// verticalLetters returns the ordered vertical letters of one stack.
func verticalLetters(s Stack) []string {
	letters := make([]string, len(s.Vertical))
	for i, v := range s.Vertical {
		letters[i] = v.Letter
	}
	return letters
}

func testLoadSlides(t *testing.T) {
	t.Run("filename pattern parses position and label", func(t *testing.T) {
		d, err := LoadSlides(slideFS("10-team_update.md", "2-main.md", "2a-Detail.md"), SlidesDir)
		if err != nil {
			t.Fatalf("LoadSlides: %v", err)
		}
		if got := deckNumbers(d); !equalInts(got, []int{2, 10}) {
			t.Fatalf("numbers = %v, want [2 10]", got)
		}
		stack := d.Stacks[0]
		if stack.Slide.Path != "slides/2-main.md" || stack.Slide.Label != "main" || stack.Slide.Letter != "" {
			t.Errorf("horizontal slide = %+v, want slides/2-main.md label main no letter", stack.Slide)
		}
		if len(stack.Vertical) != 1 {
			t.Fatalf("vertical slides = %d, want 1", len(stack.Vertical))
		}
		v := stack.Vertical[0]
		if v.Path != "slides/2a-Detail.md" || v.Letter != "a" || v.Label != "Detail" {
			t.Errorf("vertical slide = %+v, want slides/2a-Detail.md letter a label Detail", v)
		}
		if d.Stacks[1].Slide.Label != "team_update" {
			t.Errorf("label = %q, want team_update", d.Stacks[1].Slide.Label)
		}
	})

	t.Run("non-matching filenames are rejected", func(t *testing.T) {
		for _, name := range []string{
			"intro.md",
			"1.md",
			"1-.md",
			"1intro.md",
			"x-intro.md",
			"1-intro.MD",
			"01-intro.txt",
		} {
			_, err := LoadSlides(slideFS(name), SlidesDir)
			wantErr(t, err, path.Join(SlidesDir, name), "filename must be <number>[letter]-<label>.md")
			if strings.Contains(err.Error(), ":0") {
				t.Errorf("filename error %q must be positioned by path only", err.Error())
			}
		}
	})

	t.Run("over-large slide number is rejected", func(t *testing.T) {
		_, err := LoadSlides(slideFS("99999999999999999999-big.md"), SlidesDir)
		wantErr(t, err, path.Join(SlidesDir, "99999999999999999999-big.md"), "too large")
	})

	t.Run("numeric ordering puts 10 after 9", func(t *testing.T) {
		d, err := LoadSlides(slideFS("9-nine.md", "10-ten.md", "2-two.md", "1-one.md"), SlidesDir)
		if err != nil {
			t.Fatalf("LoadSlides: %v", err)
		}
		if got := deckNumbers(d); !equalInts(got, []int{1, 2, 9, 10}) {
			t.Errorf("numbers = %v, want [1 2 9 10]", got)
		}
	})

	t.Run("gaps in numbering are allowed", func(t *testing.T) {
		d, err := LoadSlides(slideFS("1-one.md", "5-five.md", "9-nine.md"), SlidesDir)
		if err != nil {
			t.Fatalf("LoadSlides: %v", err)
		}
		if got := deckNumbers(d); !equalInts(got, []int{1, 5, 9}) {
			t.Errorf("numbers = %v, want [1 5 9]", got)
		}
	})

	t.Run("letter slides nest under their number", func(t *testing.T) {
		d, err := LoadSlides(slideFS("1-intro.md", "1a-detail.md", "1b-more.md"), SlidesDir)
		if err != nil {
			t.Fatalf("LoadSlides: %v", err)
		}
		if len(d.Stacks) != 1 {
			t.Fatalf("stacks = %d, want 1", len(d.Stacks))
		}
		if got := verticalLetters(d.Stacks[0]); !equalStrings(got, []string{"a", "b"}) {
			t.Errorf("vertical letters = %v, want [a b]", got)
		}
	})

	t.Run("vertical slides are ordered by letter regardless of file order", func(t *testing.T) {
		d, err := LoadSlides(slideFS("1b-second.md", "1a-first.md", "1-main.md"), SlidesDir)
		if err != nil {
			t.Fatalf("LoadSlides: %v", err)
		}
		if got := verticalLetters(d.Stacks[0]); !equalStrings(got, []string{"a", "b"}) {
			t.Errorf("vertical letters = %v, want [a b]", got)
		}
		if d.Stacks[0].Vertical[0].Path != "slides/1a-first.md" {
			t.Errorf("first vertical path = %q, want slides/1a-first.md", d.Stacks[0].Vertical[0].Path)
		}
	})

	t.Run("uppercase letter is lower-cased", func(t *testing.T) {
		d, err := LoadSlides(slideFS("1-main.md", "1A-Detail.md"), SlidesDir)
		if err != nil {
			t.Fatalf("LoadSlides: %v", err)
		}
		if got := verticalLetters(d.Stacks[0]); !equalStrings(got, []string{"a"}) {
			t.Errorf("vertical letters = %v, want [a]", got)
		}
		if d.Stacks[0].Vertical[0].Label != "Detail" {
			t.Errorf("label = %q, want Detail (case preserved)", d.Stacks[0].Vertical[0].Label)
		}
	})

	t.Run("multiple stacks keep their own verticals", func(t *testing.T) {
		d, err := LoadSlides(slideFS(
			"1-main.md", "1a-one.md", "1b-two.md",
			"2-next.md", "2a-three.md",
		), SlidesDir)
		if err != nil {
			t.Fatalf("LoadSlides: %v", err)
		}
		if got := deckNumbers(d); !equalInts(got, []int{1, 2}) {
			t.Fatalf("numbers = %v, want [1 2]", got)
		}
		if got := verticalLetters(d.Stacks[0]); !equalStrings(got, []string{"a", "b"}) {
			t.Errorf("stack 1 verticals = %v, want [a b]", got)
		}
		if got := verticalLetters(d.Stacks[1]); !equalStrings(got, []string{"a"}) {
			t.Errorf("stack 2 verticals = %v, want [a]", got)
		}
	})

	t.Run("duplicate number names both files", func(t *testing.T) {
		_, err := LoadSlides(slideFS("1-alpha.md", "1-beta.md"), SlidesDir)
		wantErr(t, err, "slides/1-beta.md", "duplicate slide number 1", "also in slides/1-alpha.md")
	})

	t.Run("duplicate number+letter names both files", func(t *testing.T) {
		_, err := LoadSlides(slideFS("1-main.md", "1a-x.md", "1a-y.md"), SlidesDir)
		wantErr(t, err, "slides/1a-y.md", "duplicate vertical slide 1a", "also in slides/1a-x.md")
	})

	t.Run("orphan letter names the file", func(t *testing.T) {
		_, err := LoadSlides(slideFS("2-intro.md", "1a-detail.md"), SlidesDir)
		wantErr(t, err, "slides/1a-detail.md", "vertical slide 1a has no numbered slide 1")
	})

	t.Run("duplicate label names both files", func(t *testing.T) {
		_, err := LoadSlides(slideFS("1-team.md", "2-team.md"), SlidesDir)
		wantErr(t, err, "slides/2-team.md", `duplicate label "team"`, "also in slides/1-team.md")
	})

	t.Run("duplicate label across vertical and horizontal names both files", func(t *testing.T) {
		_, err := LoadSlides(slideFS("1-main.md", "1a-team.md", "2-team.md"), SlidesDir)
		wantErr(t, err, "slides/2-team.md", `duplicate label "team"`, "also in slides/1a-team.md")
	})

	t.Run("nested directory entries are ignored", func(t *testing.T) {
		m := fstest.MapFS{
			path.Join(SlidesDir, "1-main.md"):           &fstest.MapFile{Data: []byte("x")},
			path.Join(SlidesDir, "sub", "10-nested.md"): &fstest.MapFile{Data: []byte("x")},
		}
		d, err := LoadSlides(m, SlidesDir)
		if err != nil {
			t.Fatalf("LoadSlides: %v", err)
		}
		if got := deckNumbers(d); !equalInts(got, []int{1}) {
			t.Errorf("numbers = %v, want [1]", got)
		}
	})

	t.Run("missing slides directory", func(t *testing.T) {
		_, err := LoadSlides(fstest.MapFS{}, SlidesDir)
		wantErr(t, err, SlidesDir)
	})

	t.Run("fs.ReadDir error is wrapped with the directory", func(t *testing.T) {
		_, err := LoadSlides(fstest.MapFS{}, "nope")
		wantErr(t, err, "nope")
	})
}

// equalInts reports whether a and b hold the same ints in order.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalStrings reports whether a and b hold the same strings in order.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
