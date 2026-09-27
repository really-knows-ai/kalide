package render

import (
	"bytes"
	htmltmpl "html/template"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
)

// TestDeckContext pins render.deckContext's contract (template-context,
// deck-data-in-templates, deck-properties): title/author/date are plain
// strings, author and date empty when omitted; properties is always a non-nil
// map whose values keep their YAML type. A string property executes through
// html/template as escaped, literal text — never raw HTML and never
// Markdown-rendered — while a number or boolean stays a typed non-string
// value.
func TestDeckContext(t *testing.T) {
	t.Run("nil config is a well-formed empty context", func(t *testing.T) {
		ctx := deckContext(nil)
		if ctx["title"] != "" || ctx["author"] != "" || ctx["date"] != "" {
			t.Errorf("nil cfg context = %#v, want empty title/author/date", ctx)
		}
		props, ok := ctx["properties"].(map[string]any)
		if !ok || props == nil {
			t.Fatalf("nil cfg properties = %#v, want a non-nil map[string]any", ctx["properties"])
		}
		if len(props) != 0 {
			t.Errorf("nil cfg properties = %#v, want empty", props)
		}
	})

	t.Run("title, author, date and properties resolve", func(t *testing.T) {
		cfg := &deck.Config{
			Title:  "Deck",
			Author: "Ada",
			Date:   "2026-09-25",
			Properties: map[string]any{
				"audience": "exec",
			},
		}
		ctx := deckContext(cfg)
		if ctx["title"] != "Deck" || ctx["author"] != "Ada" || ctx["date"] != "2026-09-25" {
			t.Errorf("context = %#v, want title/author/date resolved", ctx)
		}
		props, ok := ctx["properties"].(map[string]any)
		if !ok {
			t.Fatalf("properties = %#v, want map[string]any", ctx["properties"])
		}
		if v, ok := props["audience"].(string); !ok || v != "exec" {
			t.Errorf("audience = %#v, want string exec", props["audience"])
		}
	})

	t.Run("values keep their YAML type, never a display string", func(t *testing.T) {
		cfg := &deck.Config{Properties: map[string]any{
			"slides": 42,
			"ratio":  1.5,
			"draft":  true,
		}}
		props := deckContext(cfg)["properties"].(map[string]any)
		if v, ok := props["slides"].(int); !ok || v != 42 {
			t.Errorf("slides = %#v, want int 42", props["slides"])
		}
		if v, ok := props["ratio"].(float64); !ok || v != 1.5 {
			t.Errorf("ratio = %#v, want float64 1.5", props["ratio"])
		}
		if v, ok := props["draft"].(bool); !ok || !v {
			t.Errorf("draft = %#v, want bool true", props["draft"])
		}
	})

	t.Run("string properties escape and are never Markdown-rendered", func(t *testing.T) {
		cfg := &deck.Config{
			Title: "Deck",
			Properties: map[string]any{
				"html":   "<b>bold</b>",
				"angle":  "<",
				"markup": "**stars**",
				"count":  42,
				"draft":  true,
			},
		}
		ctx := deckContext(cfg)

		tmpl := htmltmpl.Must(htmltmpl.New("context").Parse(
			`{{.deck.title}}|{{.deck.author}}|{{.deck.date}}|` +
				`{{.deck.properties.html}}|{{.deck.properties.angle}}|{{.deck.properties.markup}}|` +
				`{{printf "%T" .deck.properties.count}}={{.deck.properties.count}}|` +
				`{{printf "%T" .deck.properties.draft}}={{.deck.properties.draft}}`))
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, map[string]any{"deck": ctx}); err != nil {
			t.Fatalf("execute context template: %v", err)
		}
		got := buf.String()

		want := `Deck|||&lt;b&gt;bold&lt;/b&gt;|&lt;|**stars**|int=42|bool=true`
		if got != want {
			t.Errorf("rendered context = %q, want %q", got, want)
		}
		if strings.Contains(got, "<") {
			t.Errorf("rendered context %q contains a raw <, want escaped/literal text only", got)
		}
		for _, markdown := range []string{"<strong>", "<em>", "<code>"} {
			if strings.Contains(got, markdown) {
				t.Errorf("rendered context %q contains Markdown-rendered %q", got, markdown)
			}
		}
	})
}
