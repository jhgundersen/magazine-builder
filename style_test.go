package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestEnhanceStyleRetainsOriginalRequest(t *testing.T) {
	s, readPrompt := promptTestServer(t, `{"name":"Wrong name","language":"Norwegian","core":"Woodcut","palette":{"primary":"#ABCDEF"}}`)
	request := "Norsk tresnitt med fiolette marger, ingen fotografier; korte gåter fra 1930-tallet."
	got, err := s.enhanceStyle(context.Background(), "Gåtebladet", request, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Gåtebladet" {
		t.Fatalf("publication name drifted: %q", got.Name)
	}
	prompt := readPrompt()
	for _, want := range []string{request, "original user request takes precedence", "Field responsibilities", "per-page character range", "where palette roles are used"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing style instruction %q", want)
		}
	}
}

func TestNormalizeStyleRepairsPalettePerRole(t *testing.T) {
	got := normalizeStyle(magazineStyle{Palette: colorPalette{Primary: "#ABC", Secondary: "not-a-color", Accent: "#123456", Background: "#ff00zz", Text: ""}}).Palette
	fallback := fallbackStyle("", "").Palette
	if got.Primary != "#aabbcc" || got.Accent != "#123456" || got.Secondary != fallback.Secondary || got.Background != fallback.Background || got.Text != fallback.Text {
		t.Fatalf("palette normalization lost valid roles or retained invalid colors: %#v", got)
	}
}

func TestStyleFieldsReachCorrectPageKinds(t *testing.T) {
	style := magazineStyle{Core: "woodcut texture", Content: "two-column article", Short: "compact briefs", Cover: "large masthead", Feature: "hero and sidebar", Advert: "fictional advert", Filler: "reader letters", Back: "closing gag", Color: "accent only on headings", Typography: "serif body", Print: "rough paper", Palette: colorPalette{Primary: "#112233"}}
	for kind, notes := range map[string]string{"cover": style.Cover, "article": style.Content, "short": style.Short, "feature": style.Feature, "advert": style.Advert, "filler": style.Filler, "back-page": style.Back} {
		block := stylePromptBlock(style, kind)
		if block["page_notes"] != notes || block["visual_system"] != style.Core || block["color_usage"] != style.Color || block["palette"] != style.Palette {
			t.Fatalf("%s does not receive its guide fields: %#v", kind, block)
		}
	}
	for _, kind := range []string{"poster", "brand-assets"} {
		raw := compactJSON(stylePromptBlock(style, kind))
		if strings.Contains(raw, style.Content) || strings.Contains(raw, style.Feature) || strings.Contains(raw, style.Cover) {
			t.Fatalf("%s inherited an unrelated layout: %s", kind, raw)
		}
	}
	var cover map[string]any
	if err := json.Unmarshal([]byte(coverPrompt("Test", "magazine", style, nil, issueContext{})), &cover); err != nil {
		t.Fatal(err)
	}
	block := cover["style"].(map[string]any)
	if block["color_usage"] != style.Color {
		t.Fatal("cover omitted palette usage")
	}
}

func TestCreativeKitReceivesModuleSpecificGuide(t *testing.T) {
	s, readPrompt := promptTestServer(t, `{"departments":["letters"],"adverts":["invented soap"],"sidebars":["timeline"],"backPage":["gag"]}`)
	style := magazineStyle{Filler: "FILLER_RULE", Advert: "ADVERT_RULE", Short: "SHORT_RULE", Back: "BACK_RULE"}
	if _, err := s.generateCreativeKit(context.Background(), buildRequest{}, style, issueContext{Number: 1, Year: 2026, Date: "2026-01-01"}); err != nil {
		t.Fatal(err)
	}
	prompt := readPrompt()
	for _, rule := range []string{style.Filler, style.Advert, style.Short, style.Back} {
		if !strings.Contains(prompt, rule) {
			t.Fatalf("missing module rule %s", rule)
		}
	}
}
