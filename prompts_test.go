package main

import (
	"strings"
	"testing"
)

func TestIsAdvertPage(t *testing.T) {
	cases := []struct {
		n, total int
		want     bool
	}{
		{6, 12, true},  // total/2
		{10, 12, true}, // total-2
		{2, 12, false},
		{5, 8, false},
		{4, 8, true},  // total/2
		{6, 8, true},  // total-2
		{3, 4, false}, // total < 8, no advert slots
		{2, 4, false},
	}
	for _, tc := range cases {
		if got := isAdvertPage(tc.n, tc.total); got != tc.want {
			t.Errorf("isAdvertPage(%d, %d) = %v, want %v", tc.n, tc.total, got, tc.want)
		}
	}
}

func TestNormalizePageCount(t *testing.T) {
	for _, valid := range []int{4, 8, 12, 16, 24, 32, 48, 64} {
		if got := normalizePageCount(valid); got != valid {
			t.Errorf("normalizePageCount(%d) = %d, want same", valid, got)
		}
	}
	if got := normalizePageCount(10); got != 12 {
		t.Errorf("normalizePageCount(10) = %d, want 12", got)
	}
	if got := normalizePageCount(0); got != 12 {
		t.Errorf("normalizePageCount(0) = %d, want 12", got)
	}
}

func TestPlanMagazineCoverAndBack(t *testing.T) {
	req := buildRequest{Title: "Test", PageCount: 8}
	issue := issueContext{Number: 1, Year: 2025, Date: "2025-01-01", Label: "Issue 1, 2025"}
	pages := planMagazine(req, fallbackStyle("", ""), fallbackCreativeKit(req), issue)

	if len(pages) != 8 {
		t.Fatalf("expected 8 pages, got %d", len(pages))
	}
	if pages[0].Kind != "cover" {
		t.Errorf("page 1 should be cover, got %q", pages[0].Kind)
	}
	if pages[7].Kind != "back-page" {
		t.Errorf("last page should be back-page, got %q", pages[7].Kind)
	}
}

func TestPlanMagazineAdvertSlots(t *testing.T) {
	req := buildRequest{Title: "Test", PageCount: 12}
	issue := issueContext{Number: 1, Year: 2025, Date: "2025-01-01", Label: "Issue 1, 2025"}
	pages := planMagazine(req, fallbackStyle("", ""), fallbackCreativeKit(req), issue)

	advertPages := map[int]bool{}
	for _, p := range pages {
		if p.Kind == "advert" {
			advertPages[p.Number] = true
		}
	}
	// For total=12: advert at 6 (total/2) and 10 (total-2)
	if !advertPages[6] {
		t.Errorf("expected advert at page 6 for 12-page magazine, got kinds: %v", pageKinds(pages))
	}
	if !advertPages[10] {
		t.Errorf("expected advert at page 10 for 12-page magazine, got kinds: %v", pageKinds(pages))
	}
}

func TestModulePlannerNoCrossKindRepeat(t *testing.T) {
	kit := fallbackCreativeKit(buildRequest{})
	m := newModulePlanner(kit)

	seen := map[string]bool{}
	for i := 2; i <= 20; i++ {
		result := m.next("filler", 2, i)
		parts := strings.Split(result, "; ")
		for _, part := range parts {
			if seen[part] {
				t.Errorf("module %q repeated at page %d", part, i)
			}
			seen[part] = true
		}
	}
}

func TestStyleLineContainsExpectedFields(t *testing.T) {
	style := fallbackStyle("tech magazine", "")
	line := styleLine(style, "article")

	for _, want := range []string{"Language:", "Tone:", "Avoid:"} {
		if !strings.Contains(line, want) {
			t.Errorf("styleLine missing %q: %s", want, line)
		}
	}
	if len([]rune(line)) > 900 {
		t.Errorf("styleLine exceeds 900 runes: %d", len([]rune(line)))
	}
}

func pageKinds(pages []pagePlan) []string {
	kinds := make([]string, len(pages))
	for i, p := range pages {
		kinds[i] = p.Kind
	}
	return kinds
}
