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

func TestSmartLimitImagePromptPreservesJSON(t *testing.T) {
	brief := strings.Repeat("B", 500)
	prompt := map[string]any{
		"task": "render page",
		"style": map[string]any{
			"visual_brief": brief,
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
	trimmed, _ := style["visual_brief"].(string)
	if len([]rune(trimmed)) >= len([]rune(brief)) {
		t.Fatalf("visual_brief was not trimmed")
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
