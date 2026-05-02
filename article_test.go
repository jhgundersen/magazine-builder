package main

import (
	"testing"
)

func TestArticleLengthGuidanceFromStyleParsesRange(t *testing.T) {
	cases := []struct {
		raw      string
		wantMin  int
		wantMax  int
	}{
		{"700-1300 chars; concise paragraphs", 700, 1300},
		{"220-650 chars; short comic beats", 220, 650},
		{"1200-2200 chars; essay", 1200, 1600}, // capped at 1600
		{"no numbers here", 700, 1300},          // fallback
	}
	for _, tc := range cases {
		g := articleLengthGuidanceFromStyle(tc.raw)
		min, max := firstIntRange(g.Range)
		if min != tc.wantMin || max != tc.wantMax {
			t.Errorf("articleLengthGuidanceFromStyle(%q) range=%q, got min=%d max=%d, want min=%d max=%d",
				tc.raw, g.Range, min, max, tc.wantMin, tc.wantMax)
		}
	}
}

func TestArticleBodyMaxCharsScalesByPages(t *testing.T) {
	style := fallbackStyle("", "")
	single := articleBodyMaxChars(style, article{Pages: 1})
	double := articleBodyMaxChars(style, article{Pages: 2})
	if double != single*2 {
		t.Errorf("2-page article should have 2x body max: single=%d double=%d", single, double)
	}
}

func TestCleanArticlesDropsEmpty(t *testing.T) {
	in := []article{
		{Title: "A", Body: "content"},
		{Title: "", Body: ""},
		{Title: "B", Body: "more"},
	}
	out := cleanArticles(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 articles after cleaning, got %d", len(out))
	}
}

func TestCleanArticlesDefaultsKind(t *testing.T) {
	in := []article{{Title: "X", Body: "y"}}
	out := cleanArticles(in)
	if out[0].Kind != "article" {
		t.Errorf("expected kind=article, got %q", out[0].Kind)
	}
}

func TestNormalizedArticlePagesClamps(t *testing.T) {
	if got := normalizedArticlePages(article{Pages: 0}); got != 1 {
		t.Errorf("pages=0 should clamp to 1, got %d", got)
	}
	if got := normalizedArticlePages(article{Pages: 9}); got != 8 {
		t.Errorf("pages=9 should clamp to 8, got %d", got)
	}
	if got := normalizedArticlePages(article{Pages: 3}); got != 3 {
		t.Errorf("pages=3 should stay 3, got %d", got)
	}
}

func TestArticleLengthGuidanceShorter(t *testing.T) {
	g := articleLengthGuidance{Range: "700-1300", MaxTokens: 4000}
	shorter := g.shorter()
	if shorter.Range != "550-1000" {
		t.Errorf("shorter 700-1300 should give 550-1000, got %q", shorter.Range)
	}
	if shorter.MaxTokens != 1200 {
		t.Errorf("shorter should set MaxTokens=1200, got %d", shorter.MaxTokens)
	}
}
