package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	pageWidth  = 1240
	pageHeight = 1754
)

type pagePlan struct {
	Number  int      `json:"number"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Prompt  string   `json:"prompt"`
	Images  []string `json:"images,omitempty"`
	Article *article `json:"article,omitempty"`
}

type pageFurniture struct {
	Header string `json:"header"`
	Footer string `json:"footer"`
}

type modulePlanner struct {
	pools   map[string][]string
	used    map[string]bool
	cursors map[string]int
}

func newModulePlanner(kit creativeKit) *modulePlanner {
	return &modulePlanner{
		pools: map[string][]string{
			"advert":  uniqueStrings(kit.Adverts),
			"back":    uniqueStrings(append(append([]string{}, kit.BackPage...), kit.Adverts...)),
			"filler":  uniqueStrings(append(append([]string{}, kit.Departments...), kit.Sidebars...)),
			"article": uniqueStrings(kit.Sidebars),
			"feature": uniqueStrings(append(append([]string{}, kit.Sidebars...), kit.Departments...)),
		},
		used:    map[string]bool{},
		cursors: map[string]int{},
	}
}

func (m *modulePlanner) next(kind string, count, page int) string {
	if m == nil || count <= 0 {
		return ""
	}
	key := kind
	if key == "back-page" {
		key = "back"
	}
	if key != "advert" && key != "back" && key != "filler" && key != "feature" {
		key = "article"
	}
	pool := m.pools[key]
	out := make([]string, 0, count)
	for attempts := 0; len(out) < count && attempts < len(pool)*2; attempts++ {
		if len(pool) == 0 {
			break
		}
		i := m.cursors[key] % len(pool)
		m.cursors[key]++
		item := strings.TrimSpace(pool[i])
		if item == "" {
			continue
		}
		id := strings.ToLower(item)
		if m.used[id] {
			continue
		}
		m.used[id] = true
		out = append(out, item)
	}
	for len(out) < count {
		item := fmt.Sprintf("page %d exclusive %s module %d", page, key, len(out)+1)
		m.used[strings.ToLower(item)] = true
		out = append(out, item)
	}
	return strings.Join(out, "; ")
}

func planMagazine(req buildRequest, style magazineStyle, kit creativeKit, issue issueContext) []pagePlan {
	pages := make([]pagePlan, 0, req.PageCount)
	title := emptyDefault(req.Title, "Untitled Magazine")
	magType := emptyDefault(req.MagazineType, "magazine")
	modules := newModulePlanner(kit)
	pages = append(pages, pagePlan{Number: 1, Kind: "cover", Title: "Cover", Prompt: coverPrompt(title, magType, style, req.Articles, issue)})
	itemIndex := 0
	itemPart := 0
	for n := 2; n <= req.PageCount; n++ {
		if n == req.PageCount {
			pages = append(pages, pagePlan{Number: n, Kind: "back-page", Title: "Back page", Prompt: genericPrompt(n, title, style, modules.next("back", 4, n), "back", "Create a strong back page: advert, subscription panel, teaser, index, or closing visual depending on the publication style.", issue)})
			continue
		}
		if itemPart == 0 && isAdvertPage(n, req.PageCount) {
			pages = append(pages, pagePlan{Number: n, Kind: "advert", Title: "Advert", Prompt: genericPrompt(n, title, style, modules.next("advert", 4, n), "advert", "Create a full-page fictional advert that belongs naturally in this publication. Use no real brands unless supplied by the article content.", issue)})
			continue
		}
		if itemIndex < len(req.Articles) {
			a := req.Articles[itemIndex]
			kind := strings.TrimSpace(a.Kind)
			if kind == "" {
				kind = "article"
			}
			if kind == "poster" {
				pages = append(pages, pagePlan{Number: n, Kind: "poster", Title: a.Title, Article: &a, Images: a.Images, Prompt: posterPrompt(title, style, a.Body, issue)})
				itemIndex++
				itemPart = 0
				continue
			}
			totalParts := normalizedArticlePages(a)
			itemPart++
			if kind != "feature" && len([]rune(a.Body)) > 1800 {
				kind = "feature"
			}
			pages = append(pages, pagePlan{Number: n, Kind: kind, Title: a.Title, Article: &a, Images: a.Images, Prompt: articlePrompt(n, title, style, modules.next(kind, 3, n), kind, a, itemPart, totalParts, issue)})
			if itemPart >= totalParts {
				itemIndex++
				itemPart = 0
			}
			continue
		}
		pages = append(pages, pagePlan{Number: n, Kind: "filler", Title: "Departments", Prompt: genericPrompt(n, title, style, modules.next("filler", 4, n), "filler", "Create a department/filler page with short recurring modules, briefs, reader notes, charts, sidebars, small adverts, image notes, and visual rhythm suited to the publication.", issue)})
	}
	return pages
}

func coverPrompt(title, magType string, style magazineStyle, articles []article, issue issueContext) string {
	return imagePromptJSON(map[string]any{
		"task": "Create the magazine cover.",
		"metadata": map[string]any{
			"publication":      title,
			"publication_type": magType,
			"page_role":        "cover",
			"language":         emptyDefault(style.Language, "English"),
			"format":           pageFormatInstruction(),
			"tone":             emptyDefault(style.Tone, "editorial"),
			"issue":            issue,
		},
		"style": map[string]any{
			"visual_system":   compact(strings.Join(filterStrings([]string{style.Core, style.Content}), " "), 700),
			"page_notes":      styleLineSpecific(style, "cover"),
			"typography":      style.Typography,
			"print_treatment": style.Print,
			"palette":         style.Palette,
		},
		"content": map[string]any{
			"masthead":     title,
			"requirements": "use the supplied issue number/date/year for issue furniture if shown, price/barcode or equivalent cover furniture, strong hierarchy",
			"cover_lines":  "story references and final page numbers are supplied at render time",
		},
		"constraints": []string{"consistent print magazine design", "avoid " + style.Avoid},
	})
}

func articlePrompt(n int, title string, style magazineStyle, modules, kind string, a article, part, totalParts int, issue issueContext) string {
	bodyText := compactPromptText(a.Body, 800)
	seriesNote := ""
	storyOverview := ""
	layoutRequired := "headline, deck, byline/source if available, readable columns, image slots, article-specific image text, pull quote/sidebar where useful"

	if totalParts > 1 {
		storyOverview = compactPromptText(a.Body, 400)

		idx := part - 1
		if idx < len(a.Sections) && strings.TrimSpace(a.Sections[idx].ImageBrief) != "" {
			bodyText = a.Sections[idx].ImageBrief
		} else {
			runes := []rune(a.Body)
			sliceLen := len(runes) / totalParts
			start := idx * sliceLen
			end := start + sliceLen
			if end > len(runes) || part == totalParts {
				end = len(runes)
			}
			bodyText = compactPromptText(string(runes[start:end]), 400)
		}

		switch {
		case part == 1:
			seriesNote = fmt.Sprintf("Page 1 of %d: opening page. Lead with a strong hero image, large headline, deck and the opening section of the story. Leave the continuation for the next page.", totalParts)
			layoutRequired = "large hero image or illustration, headline, deck, byline, opening body text, page number"
		case part == 3 && totalParts > 4:
			seriesNote = fmt.Sprintf("Page 3 of %d: visual break. Full-page or near-full-page image related to the story. Minimal text — one short caption or pull quote maximum. No headline repeat.", totalParts)
			layoutRequired = "dominant full-page image or illustration, single short caption or pull quote, page number"
		default:
			seriesNote = fmt.Sprintf("Page %d of %d: continuation. The headline, deck, byline and opening body text already appeared on earlier pages — do not repeat them. Carry the story forward with new body columns, a pull quote drawn from the continuation text, sidebar or closing visual. Use a distinct layout.", part, totalParts)
			layoutRequired = "body text columns, pull quote or sidebar, closing image or graphic, page number"
		}
	}

	content := map[string]any{
		"title":      a.Title,
		"brief_body": bodyText,
		"modules":    modules,
	}
	if seriesNote != "" {
		content["series_note"] = seriesNote
	}
	if storyOverview != "" {
		content["story_overview"] = storyOverview
	}

	constraints := []string{"avoid " + style.Avoid}
	if totalParts > 1 {
		constraints = append(constraints, fmt.Sprintf("visual style (palette, illustration approach, typography) must be consistent across all %d pages of this article", totalParts))
		if part > 1 {
			constraints = append(constraints, "do not repeat the article headline, deck, byline or any body text that appeared on a previous page of this article")
		}
	}

	return imagePromptJSON(map[string]any{
		"task": "Create a print magazine content page.",
		"metadata": map[string]any{
			"publication": title,
			"page_role":   kind,
			"language":    emptyDefault(style.Language, "English"),
			"format":      pageFormatInstruction(),
			"tone":        emptyDefault(style.Tone, "editorial"),
			"issue":       issue,
		},
		"style":   stylePromptBlock(style, kind),
		"content": content,
		"layout": map[string]any{
			"required_elements": layoutRequired,
		},
		"constraints": constraints,
	})
}

func genericPrompt(n int, title string, style magazineStyle, modules, kind, task string, issue issueContext) string {
	return imagePromptJSON(map[string]any{
		"task": task,
		"metadata": map[string]any{
			"publication": title,
			"page_role":   kind,
			"language":    emptyDefault(style.Language, "English"),
			"format":      pageFormatInstruction(),
			"tone":        emptyDefault(style.Tone, "editorial"),
			"issue":       issue,
		},
		"style": stylePromptBlock(style, kind),
		"content": map[string]any{
			"module_ideas": modules,
		},
		"constraints": []string{"fictional brands only unless supplied by article content", "avoid " + style.Avoid},
	})
}

func posterPrompt(title string, style magazineStyle, userPrompt string, issue issueContext) string {
	return imagePromptJSON(map[string]any{
		"task": "Create an interior full-page poster image for this print magazine. This is not the front cover.",
		"metadata": map[string]any{
			"publication": title,
			"page_role":   "poster",
			"placement":   "inside page, not cover",
			"language":    emptyDefault(style.Language, "English"),
			"format":      pageFormatInstruction(),
			"tone":        emptyDefault(style.Tone, "editorial"),
			"issue":       issue,
		},
		"style": posterStylePromptBlock(style),
		"content": map[string]any{
			"image_description": compactPromptText(userPrompt, 800),
		},
		"constraints": []string{
			"one continuous edge-to-edge image; no article layout, no columns, no headline block, no sidebar boxes, no pull quotes",
			"do not create a cover: no masthead, no cover lines, no barcode, no price, no issue seal, no date, no front-page furniture",
			"no running header, footer, folio, page number, wordmark or brand asset",
			"small lettering is acceptable only if it is naturally part of the poster image itself",
			"avoid " + style.Avoid,
		},
	})
}

func stylePromptBlock(style magazineStyle, kind string) map[string]any {
	return map[string]any{
		"visual_system":   compact(strings.Join(filterStrings([]string{style.Core, style.Content}), " "), 700),
		"page_notes":      styleLineSpecific(style, kind),
		"typography":      style.Typography,
		"print_treatment": style.Print,
		"palette":         style.Palette,
	}
}

func posterStylePromptBlock(style magazineStyle) map[string]any {
	block := stylePromptBlock(style, "poster")
	block["placement"] = "interior poster page"
	block["layout_exclusions"] = []string{"magazine furniture", "article grid", "masthead", "cover composition"}
	return block
}

func imagePromptJSON(v map[string]any) string {
	return limitPrompt(compactJSON(v), 3900)
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func pageFormatInstruction() string {
	return fmt.Sprintf("FORMAT: Portrait magazine page, aspect ratio %d:%d (about 1:1.414), full page visible edge to edge, no 9:16 crop.", pageWidth, pageHeight)
}

func pageSide(n int) string {
	if n%2 == 0 {
		return "left-hand page"
	}
	return "right-hand page"
}

func pageNumberSide(n int) string {
	if n%2 == 0 {
		return "left"
	}
	return "right"
}

func normalizePageCount(n int) int {
	allowed := []int{4, 8, 12, 16, 24, 32, 48, 64}
	for _, v := range allowed {
		if n == v {
			return n
		}
	}
	return 12
}

func isAdvertPage(n, total int) bool {
	return total >= 8 && (n == total/2 || n == total-2)
}

func pageListForCover(pages []pagePlan) string {
	parts := []string{}
	i := 0
	for i < len(pages) {
		p := pages[i]
		if p.Number <= 1 {
			i++
			continue
		}
		j := i + 1
		if p.Article != nil && p.Article.Pages > 1 {
			for j < len(pages) && pages[j].Article != nil && pages[j].Article.Title == p.Article.Title {
				j++
			}
		}
		pageRef := fmt.Sprintf("page %d", p.Number)
		if j > i+1 {
			pageRef = fmt.Sprintf("pages %d-%d", p.Number, pages[j-1].Number)
		}
		body := ""
		if p.Article != nil {
			body = compact(p.Article.Body, 180)
		}
		parts = append(parts, fmt.Sprintf("- %s | kind=%s | title=%s | body=%s", pageRef, p.Kind, emptyDefault(p.Title, "Untitled"), body))
		i = j
	}
	return strings.Join(parts, "\n")
}

func cleanCoverLines(lines []coverLineItem, pages []pagePlan) []coverLineItem {
	allowed := map[int]pagePlan{}
	for _, p := range pages {
		if p.Number > 1 {
			allowed[p.Number] = p
		}
	}
	out := []coverLineItem{}
	seen := map[int]bool{}
	for _, line := range lines {
		p, ok := allowed[line.Page]
		if !ok || seen[line.Page] {
			continue
		}
		line.Title = cleanText(emptyDefault(line.Title, p.Title))
		line.Label = cleanText(line.Label)
		if line.Label == "" {
			line.Label = fmt.Sprintf("page %d", line.Page)
		}
		line.Role = cleanText(line.Role)
		seen[line.Page] = true
		out = append(out, line)
		if len(out) >= 7 {
			break
		}
	}
	return out
}

func fallbackCoverPlan(style magazineStyle, pages []pagePlan) coverPlan {
	lines := []coverLineItem{}
	for _, p := range pages {
		if p.Number <= 1 || p.Kind == "advert" || p.Kind == "back-page" {
			continue
		}
		role := ""
		if len(lines) == 0 {
			role = "main"
		}
		lines = append(lines, coverLineItem{Page: p.Number, Title: p.Title, Label: fmt.Sprintf("page %d", p.Number), Role: role})
		if len(lines) >= 6 {
			break
		}
	}
	main := ""
	if len(lines) > 0 {
		main = lines[0].Title
	}
	return coverPlan{Language: emptyDefault(style.Language, "English"), MainStoryTitle: main, Lines: lines}
}

func pageBodyForFurniture(page pagePlan) string {
	if page.Article != nil {
		return compact(page.Article.Body, 700)
	}
	return compact(page.Prompt, 700)
}

func fallbackPageFurniture(style magazineStyle, page pagePlan) pageFurniture {
	language := strings.ToLower(emptyDefault(style.Language, "English"))
	if strings.Contains(language, "norwegian") || strings.Contains(language, "norsk") {
		if page.Kind == "advert" {
			return pageFurniture{Header: "Annonse", Footer: "Magasin"}
		}
		return pageFurniture{Header: emptyDefault(page.Title, "Innhold"), Footer: "Side"}
	}
	if page.Kind == "advert" {
		return pageFurniture{Header: "Advert", Footer: "Magazine"}
	}
	return pageFurniture{Header: emptyDefault(page.Title, "Feature"), Footer: "Page"}
}
