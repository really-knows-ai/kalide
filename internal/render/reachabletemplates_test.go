package render

import (
	htmltmpl "html/template"
	"strings"
	"testing"

	"github.com/really-knows-ai/kalide/internal/deck"
	"github.com/really-knows-ai/kalide/internal/slide"
	"github.com/really-knows-ai/kalide/internal/template"
)

// This file complements TestRenderSlideSectionHelperReachable: that test pins a
// one-level helper-only target. Here the helper-only target is a CHAIN — a
// slide whose layout calls `chainmid`, which itself calls `chainleaf` — and no
// template in the chain is a declared child, a section-template field or an
// accepted name of any other. Every edge is a `{{ section … }}` call, so
// reachableTemplates must follow helper edges transitively, not just one hop,
// and must do so from the registry's memoised template.Registry.HelperTargets
// rather than by re-parsing every reachable layout through Section.HelperRefs
// on each RenderSlide.
//
// Observation note (cross-package): package render cannot read
// template.Registry's unexported helperTargets map, and there is no hook that
// counts Section.HelperRefs invocations, so the memoisation is not directly
// visible here. TestReachableTemplatesConsumesMemoisedHelperTargets observes it
// indirectly, without a production hook, by pre-warming the cache and then
// changing the root layout: the registry documents the cache as keyed by name
// and never invalidated (registered templates are immutable), so a changed
// layout still follows the cached helper edge if and only if reachableTemplates
// reads the memo instead of re-parsing Layout.Text. What cannot be observed
// cross-package is the identity of the helperTargets map itself (its keys, and
// that a hit avoids a parse call); the tests infer consumption from the stale
// edge and from the exported accessor's stable backing array.
const (
	// chainSlideLayout reaches chainmid only through the helper call: it
	// declares no sections and no section-template fields.
	chainSlideLayout = `<section class="chain-slide">{{section "chainmid" (dict "title" "Mid")}}</section>`

	// chainMidLayout reaches chainleaf the same way, so the helper-only chain
	// is at least two edges deep with no accepted child anywhere.
	chainMidLayout = `<div class="chain-mid" data-template="{{.item.template}}">{{.title}}{{section "chainleaf" (dict "title" "Leaf")}}</div>`

	chainLeafLayout  = `<span class="chain-leaf" data-template="{{.item.template}}">{{.title}}</span>`
	chainDecoyLayout = `<span class="chain-decoy" data-template="{{.item.template}}">{{.title}}</span>`
)

// mustHelperChainRegistry builds an in-code registry (no content filesystem, so
// it runs under -short) whose slide template references a two-deep chain of
// section templates through `{{ section … }}` calls alone. chaindecoy is
// registered but never called by the original layouts; the memoisation
// observation swaps the root layout to call it.
func mustHelperChainRegistry(t *testing.T) (*template.Registry, htmltmpl.FuncMap) {
	t.Helper()
	reg := template.NewRegistry(nil)
	for _, tmpl := range []*template.Template{
		{
			Name:   "chainslide",
			Usage:  template.UsageSlide,
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: chainSlideLayout},
		},
		{
			Name:   "chainmid",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "title", Type: template.FieldText}},
			Body:   template.BodyRule{Mode: template.BodyOptional},
			Layout: template.Layout{Text: chainMidLayout},
		},
		{
			Name:   "chainleaf",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "title", Type: template.FieldText}},
			Body:   template.BodyRule{Mode: template.BodyDisallowed},
			Layout: template.Layout{Text: chainLeafLayout},
		},
		{
			Name:   "chaindecoy",
			Usage:  template.UsageSection,
			Fields: []template.Field{{Name: "title", Type: template.FieldText}},
			Body:   template.BodyRule{Mode: template.BodyDisallowed},
			Layout: template.Layout{Text: chainDecoyLayout},
		},
	} {
		if err := reg.Register(tmpl); err != nil {
			t.Fatalf("register template %q: %v", tmpl.Name, err)
		}
	}
	return reg, template.LayoutFuncMap(nil, "")
}

// TestReachableTemplatesHelperOnlyChain asserts reachableTemplates follows
// helper-call edges transitively: chainmid is reached only from chainslide and
// chainleaf only from chainmid, neither is a declared child or a
// section-template field of any template, and the original layouts never call
// chaindecoy — so a reachable set containing chainleaf proves the helper edges
// are walked at least two deep (section-helper).
func TestReachableTemplatesHelperOnlyChain(t *testing.T) {
	reg, _ := mustHelperChainRegistry(t)
	root, ok := reg.Lookup("chainslide")
	if !ok {
		t.Fatal("chainslide is not registered")
	}

	got := reachableNames(root, reg)
	want := []string{"chainslide", "chainmid", "chainleaf"}
	if !sameNames(got, want) {
		t.Errorf("reachableTemplates(chainslide) = %v, want the helper-only chain %v", got, want)
	}
}

// TestReachableTemplatesConsumesMemoisedHelperTargets observes that
// reachableTemplates reads the registry's memoised HelperTargets instead of
// re-parsing Layout.Text through Section.HelperRefs. It pre-warms the cache,
// then swaps the root layout for one calling an uncached target. Because the
// registry documents its cache as never invalidated, the stale edge must still
// be followed — and the swapped-in edge must stay invisible — only if
// reachableTemplates consults the memo (section-helper).
func TestReachableTemplatesConsumesMemoisedHelperTargets(t *testing.T) {
	reg, _ := mustHelperChainRegistry(t)
	root, ok := reg.Lookup("chainslide")
	if !ok {
		t.Fatal("chainslide is not registered")
	}

	// Populate the registry's lazily memoised entry for chainslide, and confirm
	// the exported accessor really memoises: repeated calls return one shared
	// backing array, which is the property reachableTemplates consumes.
	warm := reg.HelperTargets("chainslide")
	if !sameNames(warm, []string{"chainmid"}) {
		t.Fatalf("HelperTargets(chainslide) = %v, want [chainmid]", warm)
	}
	again := reg.HelperTargets("chainslide")
	if len(warm) == 0 || len(again) == 0 || &warm[0] != &again[0] {
		t.Fatalf("HelperTargets(chainslide) did not return the identical memoised slice: %v then %v", warm, again)
	}

	// Swap the root layout for one calling the uncached decoy. A re-parse would
	// follow chaindecoy and drop chainmid; a memo read keeps chainmid.
	root.Layout.Text = `<section class="chain-slide">{{section "chaindecoy" (dict "title" "Decoy")}}</section>`

	got := make(map[string]bool)
	for _, name := range reachableNames(root, reg) {
		got[name] = true
	}
	if !got["chainmid"] {
		t.Errorf("reachableTemplates did not follow the memoised HelperTargets edge to chainmid: %v", got)
	}
	if got["chaindecoy"] {
		t.Errorf("reachableTemplates re-parsed the changed layout instead of consuming the memoised HelperTargets: %v", got)
	}
}

// TestRenderSlideSectionHelperOnlyChain drives the helper-only chain through
// the real RenderSlide pipeline: parseLayouts must have parsed every chain
// layout into the shared namespace for the nested call to execute, so the
// rendered fragment contains chainleaf nested inside chainmid inside the
// slide. Rendering twice with the same registry must still work and produce the
// same output, since reachableTemplates consumes the memoised helper edges
// (section-helper, template-language).
func TestRenderSlideSectionHelperOnlyChain(t *testing.T) {
	reg, funcMap := mustHelperChainRegistry(t)
	cfg := &deck.Config{Title: "Chain Deck"}
	meta := deck.Slide{Number: 1, Label: "chain"}
	parsed := &slide.Slide{File: "slides/1-chain.md", Template: "chainslide"}

	got := renderSlideString(t, parsed, "chain", cfg, meta, 1, reg, funcMap)

	want := `<div class="chain-mid" data-template="chainmid">Mid<span class="chain-leaf" data-template="chainleaf">Leaf</span></div>`
	if !strings.Contains(got, want) {
		t.Errorf("rendered slide does not contain the two-deep helper-only chain %q:\n%s", want, got)
	}

	second := renderSlideString(t, parsed, "chain", cfg, meta, 1, reg, funcMap)
	if second != got {
		t.Errorf("second RenderSlide with the same registry differs:\nfirst:  %s\nsecond: %s", got, second)
	}
}

// reachableNames returns the names reachableTemplates walks, in order.
func reachableNames(root *template.Template, reg *template.Registry) []string {
	out := reachableTemplates(root, reg)
	names := make([]string, len(out))
	for i, tmpl := range out {
		names[i] = tmpl.Name
	}
	return names
}

// sameNames reports whether got and want hold the same names in the same order.
func sameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
