package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type colorPalette struct {
	Primary    string `json:"primary"`
	Secondary  string `json:"secondary"`
	Accent     string `json:"accent"`
	Background string `json:"background"`
	Text       string `json:"text"`
}

type magazineStyle struct {
	Name          string       `json:"name"`
	Language      string       `json:"language"`
	Tone          string       `json:"tone"`
	Core          string       `json:"core"`
	Cover         string       `json:"cover"`
	Content       string       `json:"content"`
	Feature       string       `json:"feature"`
	Short         string       `json:"short"`
	Advert        string       `json:"advert"`
	Filler        string       `json:"filler"`
	Back          string       `json:"back"`
	ArticleLength string       `json:"articleLength"`
	Typography    string       `json:"typography"`
	Color         string       `json:"color"`
	Print         string       `json:"print"`
	Avoid         string       `json:"avoid"`
	Palette       colorPalette `json:"palette"`
}

type styleBrief struct {
	Language      string `json:"language"`
	Format        string `json:"format"`
	Tone          string `json:"tone"`
	ArticleLength string `json:"articleLength"`
	Notes         string `json:"notes"`
}

type creativeKit struct {
	Departments []string `json:"departments"`
	Adverts     []string `json:"adverts"`
	Sidebars    []string `json:"sidebars"`
	BackPage    []string `json:"backPage"`
}

type brandAsset struct {
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Image     string `json:"image"`
	PublicURL string `json:"publicUrl,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
}

type issueContext struct {
	Number int    `json:"number"`
	Year   int    `json:"year"`
	Date   string `json:"date"`
	Label  string `json:"label"`
}

func newIssueContext(now time.Time) issueContext {
	now = now.Local()
	year := now.Year() - cryptoRandInt(8)
	if cryptoRandInt(5) == 0 {
		year = now.Year() + 1
	}
	day := 1 + cryptoRandInt(365)
	date := time.Date(year, 1, 1, 12, 0, 0, 0, now.Location()).AddDate(0, 0, day-1)
	number := 1 + cryptoRandInt(240)
	return issueContext{
		Number: number,
		Year:   year,
		Date:   date.Format("2006-01-02"),
		Label:  fmt.Sprintf("Issue %d, %d", number, year),
	}
}

func normalizeIssueContext(issue issueContext, now time.Time) issueContext {
	if issue.Number <= 0 && issue.Year <= 0 && strings.TrimSpace(issue.Date) == "" && strings.TrimSpace(issue.Label) == "" {
		return newIssueContext(now)
	}
	if issue.Year <= 0 {
		if parsed, err := time.Parse("2006-01-02", strings.TrimSpace(issue.Date)); err == nil {
			issue.Year = parsed.Year()
		} else {
			issue.Year = now.Local().Year()
		}
	}
	if issue.Number <= 0 {
		if parsed, err := time.Parse("2006-01-02", strings.TrimSpace(issue.Date)); err == nil {
			issue.Number = parsed.YearDay()
		} else {
			issue.Number = now.Local().YearDay()
		}
	}
	if strings.TrimSpace(issue.Date) == "" {
		issue.Date = now.Local().Format("2006-01-02")
	}
	if strings.TrimSpace(issue.Label) == "" {
		issue.Label = fmt.Sprintf("Issue %d, %d", issue.Number, issue.Year)
	}
	return issue
}

func issueContextLine(issue issueContext) string {
	issue = normalizeIssueContext(issue, time.Now())
	return fmt.Sprintf("%s; number %d; year %d; date %s", issue.Label, issue.Number, issue.Year, issue.Date)
}

func parseStyle(raw string) magazineStyle {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallbackStyle("", "")
	}
	style, err := decodeStyle(raw)
	if err == nil {
		return style
	}
	return fallbackStyle(raw, "")
}

func decodeStyle(text string) (magazineStyle, error) {
	var style magazineStyle
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &style); err != nil {
		return style, err
	}
	return normalizeStyle(style), nil
}

func normalizeStyle(style magazineStyle) magazineStyle {
	fallback := fallbackStyle("", "")
	if style.Name == "" {
		style.Name = fallback.Name
	}
	if style.Language == "" {
		style.Language = fallback.Language
	}
	if style.Tone == "" {
		style.Tone = fallback.Tone
	}
	if style.Core == "" {
		style.Core = fallback.Core
	}
	if style.Cover == "" {
		style.Cover = fallback.Cover
	}
	if style.Content == "" {
		style.Content = fallback.Content
	}
	if style.Feature == "" {
		style.Feature = fallback.Feature
	}
	if style.Short == "" {
		style.Short = fallback.Short
	}
	if style.Advert == "" {
		style.Advert = fallback.Advert
	}
	if style.Filler == "" {
		style.Filler = fallback.Filler
	}
	if style.Back == "" {
		style.Back = fallback.Back
	}
	if style.ArticleLength == "" {
		style.ArticleLength = fallback.ArticleLength
	}
	if style.Typography == "" {
		style.Typography = fallback.Typography
	}
	if style.Color == "" {
		style.Color = fallback.Color
	}
	if style.Print == "" {
		style.Print = fallback.Print
	}
	if style.Avoid == "" {
		style.Avoid = fallback.Avoid
	}
	if style.Palette.Primary == "" {
		style.Palette = fallback.Palette
	}
	return style
}

func fallbackStyle(style, referencePath string) magazineStyle {
	base := emptyDefault(style, "A polished general-interest magazine with clear editorial hierarchy")
	if referencePath != "" {
		base += ". Use the reference image URL as visual inspiration for palette, texture, typography mood, and image treatment."
	}
	out := magazineStyle{
		Name:          "Custom magazine",
		Language:      "English",
		Tone:          "clear, magazine-like, factual and polished",
		Core:          compact(base, 210),
		Cover:         "large masthead, confident cover lines, one dominant image, date/price/barcode if fitting",
		Content:       "consistent grid, clear folios, restrained page furniture, modular image and text rhythm",
		Feature:       "more generous opening image, pull quote, sidebar, longer headline and stronger hierarchy",
		Short:         "compact brief layout, small image, dense but readable columns, one small sidebar",
		Advert:        "fictional full-page advert using the same print world, distinct from editorial pages",
		Filler:        "departments, briefs, charts, reader notes, small classifieds and recurring modules",
		Back:          "closing advert, subscription panel, teaser or striking single visual",
		ArticleLength: "700-1300 chars; concise magazine paragraphs with enough editorial texture to feel finished",
		Typography:    "strong masthead, readable serif or humanist body, compact image notes and section labels",
		Color:         "limited coherent palette with one warm and one cool accent",
		Print:         "tactile paper, realistic print texture",
		Avoid:         "generic web UI, floating app cards, unreadable logo, mismatched styles, real brands unless provided",
		Palette: colorPalette{
			Primary:    "#1c2340",
			Secondary:  "#c9a96e",
			Accent:     "#c0392b",
			Background: "#f8f5f0",
			Text:       "#1a1a1a",
		},
	}
	lower := strings.ToLower(base)
	if strings.Contains(lower, "tegneserie") || strings.Contains(lower, "norsk") || strings.Contains(lower, "guttetur") || strings.Contains(lower, "skogen") {
		out.Language = "Norwegian Bokmal"
	}
	if strings.Contains(lower, "tegneserie") || strings.Contains(lower, "pyton") || strings.Contains(lower, "mad") || strings.Contains(lower, "comic") || strings.Contains(lower, "cartoon") {
		out.Tone = "hysterisk, voksen tegneseriehumor med skarp timing og tydelige punchlines"
		out.Content = "tegneseriesider med paneler, snakkebobler, korte tekstblokker, visuelle gags og gjentagende figurer"
		out.Feature = "helside med mange paneler, store punchlines, absurde detaljer, små sidegags og tydelig tegneserieramme"
		out.Short = "kort tegneseriebeat med snakkebobler, lydeffekter, image text og rask punchline"
		out.Filler = "vitser, falske annonser, leserbrev, absurde faktabokser, quiz og små tegneseriestriper"
		out.Back = "baksidegag, falsk annonse, teaser eller stor avsluttende tegneserierute"
		out.ArticleLength = "220-650 chars; short panel beats, speech balloons, gag fragments and comic text instead of long prose"
		out.Typography = "bold comic masthead, hand-lettered accents, readable compact body, loud section labels"
		out.Color = "high-contrast comic palette with warm paper, harsh black ink and one loud accent color"
		out.Avoid = "generic web UI, respectable corporate magazine tone, long essay prose, unreadable logo, real brands unless provided"
		out.Palette = colorPalette{
			Primary:    "#1a1a1a",
			Secondary:  "#f5e6c8",
			Accent:     "#e63946",
			Background: "#fdf6e3",
			Text:       "#1a1a1a",
		}
	}
	return out
}

func fallbackCreativeKit(req buildRequest) creativeKit {
	return creativeKit{
		Departments: []string{"editor's note", "short briefs", "reader mail", "local listings", "numbers panel", "what's next", "staff picks", "calendar strip", "reader poll", "corrections box", "market notes", "archive corner", "field report", "mini interview", "glossary block", "resource list", "event diary", "trend meter"},
		Adverts:     []string{"small classified ad", "fictional supplier advert", "subscription offer", "event notice", "service directory", "training course ad", "local shop panel", "conference notice", "mail-order coupon", "patron thank-you"},
		Sidebars:    []string{"key facts", "timeline", "quote box", "how it works", "recommended next read", "source notes", "before and after", "checklist", "map inset", "numbers to know", "pros and cons", "mini profile", "method box", "field notes", "reader tip", "myth versus fact", "toolbox", "quick glossary"},
		BackPage:    []string{"subscription panel", "single bold advert", "teaser for next issue", "index and closing note", "reader challenge", "classified strip", "next-month calendar", "sponsor panel", "credits block", "closing image note"},
	}
}

func decodeCreativeKit(text string) (creativeKit, error) {
	var kit creativeKit
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &kit); err != nil {
		return kit, err
	}
	fallback := fallbackCreativeKit(buildRequest{})
	if len(kit.Departments) == 0 {
		kit.Departments = fallback.Departments
	}
	if len(kit.Adverts) == 0 {
		kit.Adverts = fallback.Adverts
	}
	if len(kit.Sidebars) == 0 {
		kit.Sidebars = fallback.Sidebars
	}
	if len(kit.BackPage) == 0 {
		kit.BackPage = fallback.BackPage
	}
	return kit, nil
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	best := ""
	start := -1
	depth := 0
	inString := false
	escaped := false
	for i, r := range text {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					best = text[start : i+1]
					start = -1
				}
			}
		}
	}
	if best != "" {
		return best
	}
	return text
}

func styleLine(style magazineStyle, kind string) string {
	specific := styleLineSpecific(style, kind)
	return compact(strings.Join([]string{"Language: " + emptyDefault(style.Language, "English"), "Tone: " + emptyDefault(style.Tone, "editorial"), style.Core, style.Typography, style.Color, style.Print, specific, "Avoid: " + style.Avoid}, " "), 900)
}

func styleLineSpecific(style magazineStyle, kind string) string {
	switch kind {
	case "cover":
		return style.Cover
	case "feature":
		return style.Feature
	case "poster":
		if strings.TrimSpace(style.Feature) != "" {
			return "Interior poster image treatment: " + style.Feature
		}
		return style.Content
	case "short", "article":
		return style.Short
	case "advert":
		return style.Advert
	case "filler":
		return style.Filler
	case "back", "back-page":
		return style.Back
	default:
		return style.Content
	}
}
