package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type articleSection struct {
	Body       string `json:"body"`
	ImageBrief string `json:"image_brief"`
}

type article struct {
	Title    string           `json:"title"`
	Body     string           `json:"body"`
	Sections []articleSection `json:"sections,omitempty"`
	Images   []string         `json:"images"`
	Source   string           `json:"source,omitempty"`
	Enhanced bool             `json:"enhanced,omitempty"`
	Kind     string           `json:"kind,omitempty"`
	Pages    int              `json:"pages,omitempty"`
}

type articleLengthGuidance struct {
	Range     string
	Guidance  string
	MaxTokens int
}

type articleLogEntry struct {
	Index      int      `json:"index"`
	Title      string   `json:"title"`
	Source     string   `json:"source,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Enhanced   bool     `json:"enhanced,omitempty"`
	BodyChars  int      `json:"bodyChars"`
	Body       string   `json:"body,omitempty"`
	BodySample string   `json:"bodySample,omitempty"`
	Images     []string `json:"images,omitempty"`
	Error      string   `json:"error,omitempty"`
}

func (g articleLengthGuidance) shorter() articleLengthGuidance {
	switch g.Range {
	case "220-650":
		g.Range = "160-420"
		g.Guidance += " The user supplied very little material, so keep it especially compact."
		g.MaxTokens = 1200
	case "450-900":
		g.Range = "320-700"
		g.MaxTokens = 1200
	case "700-1300":
		g.Range = "550-1000"
		g.MaxTokens = 1200
	default:
		g.Range = "750-1300"
		g.MaxTokens = 1200
	}
	return g
}

func (g articleLengthGuidance) withJSONBudget() articleLengthGuidance {
	if g.MaxTokens < 4000 {
		g.MaxTokens = 4000
	}
	return g
}

func manualArticleLengthForStyle(style magazineStyle, a article) articleLengthGuidance {
	if strings.TrimSpace(style.ArticleLength) != "" {
		return articleLengthGuidanceFromStyle(style.ArticleLength).withJSONBudget()
	}
	text := strings.ToLower(strings.Join([]string{
		style.Name,
		style.Tone,
		style.Core,
		style.Content,
		style.Feature,
		style.Short,
		style.Typography,
	}, " "))
	if strings.Contains(text, "comic") || strings.Contains(text, "cartoon") || strings.Contains(text, "panel") || strings.Contains(text, "manga") || strings.Contains(text, "tegneserie") {
		return articleLengthGuidance{
			Range:     "220-650",
			Guidance:  "Use short balloons, captions, panel beats, labels, or gag fragments rather than long prose.",
			MaxTokens: 1200,
		}.withJSONBudget()
	}
	if strings.Contains(text, "tabloid") || strings.Contains(text, "satire") || strings.Contains(text, "parody") || strings.Contains(text, "humor") || strings.Contains(text, "puzzle") || strings.Contains(text, "quiz") {
		return articleLengthGuidance{
			Range:     "450-900",
			Guidance:  "Use punchy short sections, headlines, boxes, jokes, prompts, or quick-hit copy rather than a long essay.",
			MaxTokens: 1200,
		}.withJSONBudget()
	}
	if strings.Contains(text, "literary") || strings.Contains(text, "longform") || strings.Contains(text, "essay") || strings.Contains(text, "feature") || strings.Contains(text, "technical") || strings.Contains(text, "analysis") {
		return articleLengthGuidance{
			Range:     "1200-2200",
			Guidance:  "Use a fuller article structure with paragraphs, detail, transitions and a developed editorial arc.",
			MaxTokens: 1500,
		}.withJSONBudget()
	}
	return articleLengthGuidance{
		Range:     "700-1300",
		Guidance:  "Use a normal magazine article length with concise paragraphs and enough texture to feel finished.",
		MaxTokens: 1200,
	}.withJSONBudget()
}

func articleLengthGuidanceFromStyle(raw string) articleLengthGuidance {
	raw = compact(raw, 220)
	min, max := firstIntRange(raw)
	if min <= 0 || max <= 0 {
		min, max = 700, 1300
	}
	if max > 1600 {
		max = 1600
	}
	if min > max {
		min = max * 2 / 3
	}
	return articleLengthGuidance{
		Range:     fmt.Sprintf("%d-%d", min, max),
		Guidance:  raw,
		MaxTokens: maxTokensForCharTarget(max),
	}
}

// articleBodyMaxChars returns the max body chars for the style, scaled by the
// number of pages the article occupies. The per-page cap is 1600; multi-page
// articles get proportionally more so the full body can cover all pages.
func articleBodyMaxChars(style magazineStyle, a article) int {
	perPage := 1600
	if strings.TrimSpace(style.ArticleLength) != "" {
		_, max := firstIntRange(style.ArticleLength)
		if max > 0 && max < perPage {
			perPage = max
		}
	}
	pages := normalizedArticlePages(a)
	if pages < 1 {
		pages = 1
	}
	return perPage * pages
}

func firstIntRange(raw string) (int, int) {
	m := regexp.MustCompile(`(\d{2,5})\D+(\d{2,5})`).FindStringSubmatch(raw)
	if len(m) != 3 {
		return 0, 0
	}
	min, _ := strconv.Atoi(m[1])
	max, _ := strconv.Atoi(m[2])
	if min > max {
		min, max = max, min
	}
	return min, max
}

func maxTokensForCharTarget(chars int) int {
	switch {
	case chars <= 800:
		return 4000
	case chars <= 1600:
		return 5000
	case chars <= 3200:
		return 6000
	default:
		return 8000
	}
}

func normalizedArticlePages(a article) int {
	if a.Pages < 1 {
		return 1
	}
	if a.Pages > 8 {
		return 8
	}
	return a.Pages
}

func cleanArticles(in []article) []article {
	out := make([]article, 0, len(in))
	for _, a := range in {
		a.Title = cleanText(a.Title)
		a.Body = cleanText(a.Body)
		a.Kind = strings.TrimSpace(a.Kind)
		if a.Kind == "" {
			a.Kind = "article"
		}
		a.Pages = normalizedArticlePages(a)
		if a.Title == "" && a.Body == "" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func articleLogEntries(articles []article, includeBody bool) []articleLogEntry {
	out := make([]articleLogEntry, 0, len(articles))
	for i, a := range articles {
		out = append(out, articleLogEntryFromArticle(i, a, includeBody))
	}
	return out
}

func articleLogEntryFromArticle(i int, a article, includeBody bool) articleLogEntry {
	body := ""
	bodySample := compact(a.Body, 1000)
	if includeBody {
		body = a.Body
		bodySample = ""
	}
	return articleLogEntry{
		Index:      i,
		Title:      a.Title,
		Source:     a.Source,
		Kind:       a.Kind,
		Enhanced:   a.Enhanced,
		BodyChars:  len([]rune(a.Body)),
		Body:       body,
		BodySample: bodySample,
		Images:     a.Images,
	}
}

func countEnhancedArticles(articles []article) int {
	n := 0
	for _, a := range articles {
		if a.Enhanced {
			n++
		}
	}
	return n
}

func articleList(articles []article) string {
	if len(articles) == 0 {
		return "No articles yet; generate broadly useful departments and filler."
	}
	parts := make([]string, 0, len(articles))
	for _, a := range articles {
		parts = append(parts, "- "+emptyDefault(a.Title, "Untitled")+": "+compact(a.Body, 260))
	}
	return strings.Join(parts, "\n")
}

func articleTitles(articles []article, max int) string {
	parts := []string{}
	for _, a := range articles {
		if a.Title != "" {
			parts = append(parts, a.Title)
		}
		if len(parts) >= max {
			break
		}
	}
	if len(parts) == 0 {
		return "invent suitable cover lines for the concept"
	}
	return strings.Join(parts, "; ")
}
