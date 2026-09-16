package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactTruncatesAtBoundary(t *testing.T) {
	s := strings.Repeat("x", 100)
	if got := compact(s, 100); got != s {
		t.Fatalf("compact at exact boundary should not truncate: %q", got)
	}
	if got := compact(s, 99); !strings.HasSuffix(got, "...") || len([]rune(got)) != 102 {
		t.Fatalf("compact over boundary should truncate and append ellipsis: %q", got)
	}
}

func TestCompactPromptTextStopsAtSentenceBoundaryWithoutEllipsis(t *testing.T) {
	input := "First sentence is useful. Second sentence is also useful. Third sentence should be removed because it is too long."
	got := compactPromptText(input, 70)
	if got != "First sentence is useful. Second sentence is also useful." {
		t.Fatalf("unexpected prompt text: %q", got)
	}
	if strings.Contains(got, "...") {
		t.Fatalf("prompt text should not use ellipsis: %q", got)
	}
}

func TestCompactPromptTextFallsBackToWordBoundaryWithoutEllipsis(t *testing.T) {
	input := "abcdef ghijkl mnopqr stuvwx yz"
	got := compactPromptText(input, 20)
	if got != "abcdef ghijkl" {
		t.Fatalf("unexpected prompt text: %q", got)
	}
	if strings.Contains(got, "...") {
		t.Fatalf("prompt text should not use ellipsis: %q", got)
	}
}

func TestSmartLimitImagePromptPreservesJSON(t *testing.T) {
	brief := strings.Repeat("B", 500)
	prompt := map[string]any{
		"task": "render page",
		"style": map[string]any{
			"visual_system": brief,
		},
	}
	b, _ := json.Marshal(prompt)
	raw := string(b)

	max := len([]rune(raw)) - 100
	got := smartLimitImagePrompt(raw, max)

	if len([]rune(got)) > max {
		t.Fatalf("result exceeds max: %d > %d", len([]rune(got)), max)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, got)
	}
	style, _ := out["style"].(map[string]any)
	trimmed, _ := style["visual_system"].(string)
	if len([]rune(trimmed)) >= len([]rune(brief)) {
		t.Fatalf("visual_system was not trimmed")
	}
}

func TestSmartLimitImagePromptFallsBackToHardCut(t *testing.T) {
	raw := strings.Repeat("x", 500)
	got := smartLimitImagePrompt(raw, 100)
	if len([]rune(got)) != 100 {
		t.Fatalf("expected hard cut to 100 runes, got %d", len([]rune(got)))
	}
}

func TestUniqueStringsDeduplicatesCaseInsensitive(t *testing.T) {
	in := []string{"Apple", "apple", "APPLE", "Banana", "banana"}
	got := uniqueStrings(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 unique strings, got %v", got)
	}
}

func TestEmptyDefaultReturnsDefault(t *testing.T) {
	if got := emptyDefault("", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}
	if got := emptyDefault("  ", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback for whitespace, got %q", got)
	}
	if got := emptyDefault("value", "fallback"); got != "value" {
		t.Fatalf("expected value, got %q", got)
	}
}

func TestSmartLimitPreservesEssentialContentWhenBudgetCannotFit(t *testing.T) {
	raw := compactJSON(map[string]any{
		"content":     map[string]any{"brief_body": strings.Repeat("Story text. ", 100)},
		"constraints": []string{"no invented quotes"},
	})
	got := smartLimitImagePrompt(raw, 100)
	if !json.Valid([]byte(got)) || got != raw {
		t.Fatal("essential content must remain intact for the render caller to reject an insufficient budget")
	}
}

func TestPromptBudgetDropsExtrasBeforeStyleOrCopy(t *testing.T) {
	essential := map[string]any{
		"task":           "Render",
		"style":          map[string]any{"visual_system": strings.Repeat("Norsk tegneserie. ", 30), "palette": map[string]any{"primary": "#123456"}},
		"metadata":       map[string]any{"issue": 23},
		"page_furniture": map[string]any{"header": "Småstoff"},
		"constraints":    []string{"Preserve source facts"},
		"content":        map[string]any{"brief_body": "Ærlig brødtekst", "image_brief": "Tegn en figur"},
	}
	expected := compactJSON(essential)
	content := essential["content"].(map[string]any)
	content["modules"] = strings.Repeat("Optional sidebar. ", 100)
	content["story_overview"] = strings.Repeat("Repeated overview. ", 100)
	got := smartLimitImagePrompt(compactJSON(essential), len([]rune(expected)))
	if got != expected {
		t.Fatalf("budgeting altered essential data:\n%s\nwant:\n%s", got, expected)
	}
}
