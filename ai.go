package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type generatedImage struct {
	Image     string
	PublicURL string
}

func (s *server) enhanceStyle(ctx context.Context, title, style, referencePath string) (magazineStyle, error) {
	brief, err := s.generateStyleBrief(ctx, title, style)
	if err != nil {
		return magazineStyle{}, err
	}
	prompt := "Return only valid compact JSON. No markdown, no prose. Required keys: name, language, tone, core, cover, content, feature, short, advert, filler, back, articleLength, typography, color, print, avoid, palette. Keep every string value under 220 characters. name must be exactly " + strconv.Quote(emptyDefault(title, "Untitled Magazine")) + ". Use this compact brief as the source of truth. Preserve strong formats such as comics, satire, tabloids, puzzles or parody; do not normalize them into a generic magazine. Make articleLength practical for article rewrite prompts. The palette key must be a JSON object with exactly five keys: primary, secondary, accent, background, text — each a CSS hex color string (e.g. \"#1a2b3c\") that reflects the publication's visual identity derived from the style brief.\n\nBRIEF JSON:\n" + compactJSON(brief)
	if referencePath != "" {
		prompt += "\n\nReference image URL: " + referencePath + "\nUse it only as visual inspiration for palette, typography mood, texture and layout feeling."
	}
	var parsed magazineStyle
	if err := s.runDefapiTextJSON(ctx, prompt, 6000, &parsed); err != nil {
		return magazineStyle{}, err
	}
	return normalizeStyle(parsed), nil
}

func (s *server) generateStyleBrief(ctx context.Context, title, style string) (styleBrief, error) {
	prompt := "Return only valid compact JSON. No markdown, no prose. Required keys: language, format, tone, articleLength, notes. Infer the intended publication format from the user's style request. articleLength must include a character range and structure note, such as '220-650 chars; short comic panel beats' or '1200-2200 chars; essay paragraphs'. Keep each value under 180 characters.\n\nPUBLICATION NAME: " + strconv.Quote(emptyDefault(title, "Untitled Magazine")) + "\nUSER STYLE:\n" + emptyDefault(style, "clean contemporary general-interest magazine")
	var brief styleBrief
	if err := s.runDefapiTextJSON(ctx, prompt, 3000, &brief); err != nil {
		return styleBrief{}, err
	}
	return brief, nil
}

func (s *server) generateIssueContext(ctx context.Context, req buildRequest, style magazineStyle) (issueContext, error) {
	now := time.Now()
	seed := randomHex(8)
	originalStylePrompt := emptyDefault(req.StylePrompt, req.Style)
	prompt := fmt.Sprintf("Return only valid compact JSON with keys number, year, date, label. Choose a specific issue number and publication date for this magazine issue. Use the original user style prompt and publication concept to pick details that feel intentional: retro concepts may use older years, current affairs may be recent, timeless fictional magazines may use any plausible year. Add variety; do not default to today's date or issue number unless the style clearly asks for a current issue. The date must be Gregorian YYYY-MM-DD. The number must be a positive integer. The label must be short and localized if obvious, such as \"Issue 42, 1998\" or a natural equivalent. Use random seed %s.\n\nTODAY: %s\nPUBLICATION: %s\nPUBLICATION TYPE: %s\nLANGUAGE: %s\nSTYLE JSON SUMMARY: %s\nORIGINAL USER STYLE PROMPT:\n%s", seed, now.Format("2006-01-02"), emptyDefault(req.Title, emptyDefault(style.Name, "Untitled Magazine")), emptyDefault(req.MagazineType, "magazine"), emptyDefault(style.Language, "English"), styleLine(style, "content"), compact(originalStylePrompt, 1800))
	text, err := s.runDefapiText(ctx, prompt, 400)
	if err != nil {
		return issueContext{}, err
	}
	var issue issueContext
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &issue); err != nil {
		return issueContext{}, err
	}
	issue = normalizeIssueContext(issue, now)
	parsedDate, err := time.Parse("2006-01-02", issue.Date)
	if err != nil {
		return issueContext{}, fmt.Errorf("invalid issue date %q: %w", issue.Date, err)
	}
	issue.Year = parsedDate.Year()
	if !strings.Contains(issue.Label, strconv.Itoa(issue.Number)) || !strings.Contains(issue.Label, strconv.Itoa(issue.Year)) {
		issue.Label = fmt.Sprintf("Issue %d, %d", issue.Number, issue.Year)
	}
	if issue.Number <= 0 || issue.Year <= 0 || strings.TrimSpace(issue.Label) == "" {
		return issueContext{}, errors.New("incomplete issue context")
	}
	return issue, nil
}

func (s *server) generateCreativeKit(ctx context.Context, req buildRequest, style magazineStyle, issue issueContext) (creativeKit, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON. Required keys: departments, adverts, sidebars, backPage. Make departments and sidebars arrays of 18-24 unique short strings each. Make adverts and backPage arrays of 10-16 unique short strings each. Every string must describe one specific reusable page element, never a duplicate or near-duplicate. Do not include reusable image text or image-text labels in this issue-wide kit; image text must be derived from each article at page-render time. Prepare issue-wide generic page elements for a %s called %q. Match this style and tone: %s. Issue context: %s. Use that exact issue number/date/year when an element needs issue metadata; otherwise omit issue metadata. Do not invent a different issue number, year or date. Avoid copyrighted brands unless supplied by the user.\n\nArticles:\n%s", emptyDefault(req.MagazineType, "magazine"), emptyDefault(req.Title, "Untitled Magazine"), styleLine(style, "content"), issueContextLine(issue), articleList(req.Articles))
	text, err := s.runDefapiText(ctx, prompt, 3000)
	if err != nil {
		return creativeKit{}, err
	}
	kit, err := decodeCreativeKit(text)
	if err != nil {
		return creativeKit{}, err
	}
	return kit, nil
}

func (s *server) generateBrandAssets(ctx context.Context, workspace string, req buildRequest, style magazineStyle, issue issueContext) ([]brandAsset, error) {
	title := emptyDefault(req.Title, emptyDefault(style.Name, "Untitled Magazine"))
	issueLabel := fmt.Sprintf("No. %d", issue.Number)
	prompt := imagePromptJSON(map[string]any{
		"task": "Create one clean unlabeled magazine brand asset board.",
		"metadata": map[string]any{
			"publication": title,
			"language":    emptyDefault(style.Language, "English"),
			"tone":        emptyDefault(style.Tone, "editorial"),
		},
		"style": stylePromptBlock(style, "cover"),
		"content": map[string]any{
			"assets_to_draw": []string{
				"large cover masthead — publication name only, in the publication's headline typeface",
				"small horizontal running-header wordmark — publication name only, compact, for page headers",
				fmt.Sprintf("small issue number mark — draw only %q, no date or year", issueLabel),
				"one horizontal rule or divider motif consistent with the typography",
			},
			"layout": "single flat asset board on a plain light background, generous whitespace between each asset, left-aligned, no mockup pages, no article photos",
			"text":   "Only draw text that is literally part of the asset itself: the publication name in the masthead/wordmark, the issue label in the number mark. Do not add any other labels, captions, annotations, arrows, headings or explanatory text.",
		},
		"constraints": []string{"unlabeled board except for the asset text itself", "simple clean marks", "high contrast", "print magazine identity system", "avoid " + style.Avoid},
	})
	image, err := s.runDefapiImageWithRetry(ctx, workspace, 0, prompt, nil)
	if err != nil {
		return nil, err
	}
	return []brandAsset{{
		Kind:      "brand-sheet",
		Label:     "Masthead, running-header wordmark, issue number mark, divider and color palette",
		Image:     image.Image,
		PublicURL: image.PublicURL,
		Prompt:    prompt,
	}}, nil
}

func (s *server) rewriteArticleForStyle(ctx context.Context, a article, style magazineStyle) (article, error) {
	pages := normalizedArticlePages(a)
	bodyMax := articleBodyMaxChars(style, a)
	bodyRange := fmt.Sprintf("900-%d", bodyMax)
	bodySample := compact(a.Body, 3200*pages)
	maxTokens := maxTokensForCharTarget(bodyMax)
	if maxTokens < 4000 {
		maxTokens = 4000
	}
	if a.Kind == "podcast" {
		bodyRange = fmt.Sprintf("1800-%d", max(2800, bodyMax))
		bodySample = sampleLongText(a.Body, 6500)
		maxTokens = max(maxTokens, 3000)
	}
	lengthNote := strings.TrimSpace(style.ArticleLength)
	if lengthNote == "" {
		lengthNote = "Use coherent paragraphs or short page-ready chunks as the style demands."
	}
	if pages > 1 {
		return s.rewriteArticleForStyleMultiPage(ctx, a, style, pages, bodyRange, bodyMax, bodySample, lengthNote, maxTokens)
	}
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and body. Rewrite this imported source into print-ready magazine copy matching the publication concept, style and text tone. If the publication is comic-led, satirical, tabloid, puzzle-like, literary, technical, or otherwise strongly formatted, make the copy sound and structure fit that format. Keep facts, names, chronology, arguments, concrete examples and useful nuance. Remove web/navigation language, links, embeds, YouTube mentions, newsletter prompts, transcript mechanics and SEO clutter. Title should fit the publication voice. Body length and structure should match this style guidance: %s. Body should be %s characters unless the guidance clearly requires a shorter visual format.\n\nSTYLE AND TONE: %s\n\nSOURCE TITLE: %s\nSOURCE BODY: %s", lengthNote, bodyRange, styleLine(style, "article"), a.Title, bodySample)
	var out struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := s.runDefapiTextJSON(ctx, prompt, maxTokens, &out); err != nil {
		return a, err
	}
	if strings.TrimSpace(out.Title) != "" {
		a.Title = cleanText(out.Title)
	}
	if strings.TrimSpace(out.Body) != "" {
		a.Body = compact(cleanText(out.Body), articleBodyMaxChars(style, a))
	}
	a.Enhanced = true
	return a, nil
}

func (s *server) rewriteArticleForStyleMultiPage(ctx context.Context, a article, style magazineStyle, pages int, bodyRange string, bodyMax int, bodySample, lengthNote string, maxTokens int) (article, error) {
	pageDescs := make([]string, pages)
	perPage := bodyMax / pages
	pageDescs[0] = fmt.Sprintf("page 1: opening — headline intro, deck, byline and opening section (~%d chars)", perPage)
	for i := 1; i < pages; i++ {
		if i == 2 && pages > 4 {
			pageDescs[i] = fmt.Sprintf("page %d: visual break — one striking image caption or pull quote only (~200 chars)", i+1)
		} else {
			pageDescs[i] = fmt.Sprintf("page %d: continuation — body columns, pull quotes, closing (~%d chars)", i+1, perPage)
		}
	}
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and sections. sections is an array of exactly %d objects, one per magazine page, each with keys body and image_brief. body is the print-ready article text for that page in the publication style. image_brief is 1-3 sentences describing what to draw or photograph for that page — write it as a visual instruction to an image generator, not as prose. Rewrite this source to fit this %d-page layout: %s. Body length guidance per page: %s. Style: %s. Remove web/navigation language, links, embeds and SEO clutter.\n\nSTYLE AND TONE: %s\n\nSOURCE TITLE: %s\nSOURCE BODY: %s",
		pages, pages, strings.Join(pageDescs, "; "), bodyRange, lengthNote, styleLine(style, "article"), a.Title, bodySample)
	var out struct {
		Title    string           `json:"title"`
		Sections []articleSection `json:"sections"`
	}
	if err := s.runDefapiTextJSON(ctx, prompt, maxTokens, &out); err != nil {
		return a, err
	}
	if strings.TrimSpace(out.Title) != "" {
		a.Title = cleanText(out.Title)
	}
	if len(out.Sections) > 0 {
		bodies := make([]string, 0, len(out.Sections))
		for i := range out.Sections {
			out.Sections[i].Body = compact(cleanText(out.Sections[i].Body), perPage+200)
			out.Sections[i].ImageBrief = compact(cleanText(out.Sections[i].ImageBrief), 300)
			if out.Sections[i].Body != "" {
				bodies = append(bodies, out.Sections[i].Body)
			}
		}
		a.Sections = out.Sections
		a.Body = strings.Join(bodies, "\n\n")
	}
	a.Enhanced = true
	return a, nil
}

func (s *server) rewriteManualArticleForStyle(ctx context.Context, a article, style magazineStyle) (article, error) {
	bodySample := compact(a.Body, 3200)
	length := manualArticleLengthForStyle(style, a)
	maxTokens := length.MaxTokens
	if len([]rune(a.Body)) < 700 {
		length = length.shorter()
		maxTokens = length.MaxTokens
	}
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and body. Turn this manually entered rough or partial article material into finished print-ready magazine copy. Make the title and body strongly match the publication concept, style and text tone; the body should be at least as transformed as the title. Do not merely clean up the source. Develop fragments, notes and plain prose into a coherent article in the magazine's voice, structure and rhythm. If the publication is comic-led, satirical, tabloid, puzzle-like, literary, technical, nostalgic, niche, or otherwise strongly formatted, make the body unmistakably fit that format. Preserve the user's concrete intent, facts, names and constraints, but you may add plausible connective tissue, framing, transitions, departments, jokes, asides, service boxes or editorial texture when the input is thin. Length and structure must match the magazine format: %s Body should be %s characters.\n\nSTYLE AND TONE: %s\n\nUSER TITLE OR TOPIC: %s\nUSER ROUGH MATERIAL: %s", length.Guidance, length.Range, styleLine(style, "article"), a.Title, bodySample)
	var out struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := s.runDefapiTextJSON(ctx, prompt, maxTokens, &out); err != nil {
		return a, err
	}
	if strings.TrimSpace(out.Title) != "" {
		a.Title = cleanText(out.Title)
	}
	if strings.TrimSpace(out.Body) != "" {
		a.Body = compact(cleanText(out.Body), articleBodyMaxChars(style, a))
	}
	a.Enhanced = true
	return a, nil
}

func (s *server) rewriteFeatureForStyle(ctx context.Context, a article, style magazineStyle) (article, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and body. Turn this requested magazine feature page into a precise image-generation brief matching the publication style. The feature can be a crossword, comments page, quiz, TV/program listings, puzzle page, letters, classifieds, calendar, chart, or any other non-article department. Preserve the user's intent, but make it print-ready and specific about sections/modules. Body should be 700-1400 characters and describe what the page should contain.\n\nSTYLE: %s\n\nFEATURE TITLE: %s\nFEATURE REQUEST: %s", styleLine(style, "filler"), a.Title, compact(a.Body, 2400))
	text, err := s.runDefapiText(ctx, prompt, 2000)
	if err != nil {
		return a, err
	}
	var out struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &out); err != nil {
		return a, err
	}
	if strings.TrimSpace(out.Title) != "" {
		a.Title = cleanText(out.Title)
	}
	if strings.TrimSpace(out.Body) != "" {
		a.Body = cleanText(out.Body)
	}
	a.Kind = "feature"
	a.Enhanced = true
	return a, nil
}

func (s *server) summarizePodcastForImport(ctx context.Context, a article, style magazineStyle) (article, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and body. Summarize this podcast episode source material into detailed print-editorial notes for a later magazine article rewrite. Preserve concrete facts, people, works, chronology, arguments, opinions, disagreements, examples, recommendations and useful chapter structure. Prefer substance over polish. Do not mention transcripts, timestamps, RSS, show notes or source mechanics. Title should be a concise episode/article title. Body should be 2600-4200 characters, factual and balanced, not finished prose.\n\nSTYLE CONTEXT: %s\n\nEPISODE TITLE: %s\nSOURCE MATERIAL SAMPLE:\n%s", styleLine(style, "article"), a.Title, sampleLongText(a.Body, 18000))
	text, err := s.runDefapiText(ctx, prompt, 4000)
	if err != nil {
		return a, err
	}
	var out struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &out); err != nil {
		return a, err
	}
	if strings.TrimSpace(out.Title) != "" {
		a.Title = cleanText(out.Title)
	}
	if strings.TrimSpace(out.Body) != "" {
		a.Body = cleanText(out.Body)
	}
	return a, nil
}

func (s *server) generateArticles(ctx context.Context, title string, style magazineStyle, count int) ([]article, error) {
	var out struct {
		Articles []article `json:"articles"`
	}
	prompt := fmt.Sprintf("Return only valid compact JSON with key articles, an array of exactly %d objects. Each object must have title and body. Generate original fictional-but-plausible magazine articles that fit this publication. Do not use real copyrighted brands unless generic/current facts are unavoidable. Vary article types: one feature, one short news item, one practical/service piece, one opinion/interview/list if count allows. Body length 900-1500 characters each, ready for print layout.\n\nPUBLICATION: %s\nSTYLE: %s", count, emptyDefault(title, "Untitled Magazine"), styleLine(style, "article"))
	maxTokens := max(7000, count*2200)
	if maxTokens > 12000 {
		maxTokens = 12000
	}
	if err := s.runDefapiTextJSON(ctx, prompt, maxTokens, &out); err != nil {
		return nil, err
	}
	cleaned := cleanArticles(out.Articles)
	for i := range cleaned {
		cleaned[i].Enhanced = true
	}
	if len(cleaned) > count {
		cleaned = cleaned[:count]
	}
	if len(cleaned) == 0 {
		return nil, errors.New("defapi text returned no usable generated articles")
	}
	return cleaned, nil
}

func (s *server) generateCoverPlan(ctx context.Context, title string, style magazineStyle, pages []pagePlan, issue issueContext) (coverPlan, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with keys language, mainStoryTitle, lines. The lines value must be an array of 4-7 objects with keys page, title, label and role. Choose the strongest cover lines from the final page order. Identify exactly one main story using mainStoryTitle and role=\"main\" on that line. Translate page labels into the publication language; do not use English-only shorthand like p2 unless that is natural for the language. Use the final page numbers exactly as supplied. Use this exact issue context if issue metadata appears: %s. Do not invent a different issue number, year or date. Avoid adverts unless the issue has too few editorial pages.\n\nPUBLICATION: %s\nLANGUAGE: %s\nSTYLE: %s\nFINAL PAGES:\n%s", issueContextLine(issue), emptyDefault(title, style.Name), emptyDefault(style.Language, "English"), styleLine(style, "cover"), pageListForCover(pages))
	text, err := s.runDefapiText(ctx, prompt, 4000)
	if err != nil {
		return coverPlan{}, err
	}
	var out coverPlan
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &out); err != nil {
		return coverPlan{}, err
	}
	if strings.TrimSpace(out.Language) == "" {
		out.Language = emptyDefault(style.Language, "English")
	}
	out.Lines = cleanCoverLines(out.Lines, pages)
	if len(out.Lines) == 0 {
		return fallbackCoverPlan(style, pages), nil
	}
	if strings.TrimSpace(out.MainStoryTitle) == "" {
		out.MainStoryTitle = out.Lines[0].Title
		out.Lines[0].Role = "main"
	}
	return out, nil
}

func (s *server) generatePageFurniture(ctx context.Context, style magazineStyle, page pagePlan, issue issueContext) (pageFurniture, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with keys header and footer. Write very short localized magazine page furniture in %s. Tone: %s.\n\nHeader: a section or department slug, 2-4 words maximum — e.g. \"Features\", \"Interview\", \"In Brief\", or the equivalent in the publication language. The header is a section label, never issue metadata.\n\nFooter: 2-5 words to sit beside the page number — typically the publication name or a short section label. Never repeat the year, issue number, or date in the footer unless the publication style explicitly uses issue info in footers.\n\nUse the page kind and article title to pick an appropriate section slug. Match this publication style: %s.\n\nPAGE KIND: %s\nPAGE TITLE: %s\nPAGE BODY: %s", emptyDefault(style.Language, "English"), emptyDefault(style.Tone, "editorial"), styleLine(style, page.Kind), page.Kind, page.Title, pageBodyForFurniture(page))
	var out pageFurniture
	if err := s.runDefapiTextJSON(ctx, prompt, 2000, &out); err != nil {
		return pageFurniture{}, err
	}
	out.Header = compact(cleanText(out.Header), 60)
	out.Footer = compact(cleanText(out.Footer), 70)
	if out.Header == "" || out.Footer == "" {
		return pageFurniture{}, errors.New("empty page furniture")
	}
	return out, nil
}

func (s *server) pagePromptWithFurniture(ctx context.Context, style magazineStyle, page pagePlan, issue issueContext) string {
	if strings.EqualFold(page.Kind, "cover") || strings.EqualFold(page.Kind, "poster") {
		return page.Prompt
	}
	workspace, _ := ctx.Value(workspaceContextKey{}).(string)
	var copy pageFurniture
	var err error
	for attempt := range 3 {
		copy, err = s.generatePageFurniture(ctx, style, page, issue)
		if err == nil {
			break
		}
		s.workspaceLog(workspace, "furniture: page=%d attempt=%d failed: %v", page.Number, attempt+1, err)
	}
	if err != nil {
		s.workspaceLog(workspace, "furniture: page=%d all attempts failed, using fallback", page.Number)
		copy = fallbackPageFurniture(style, page)
	}
	side := pageSide(page.Number)
	outer := pageNumberSide(page.Number)
	payload := map[string]any{
		"header":         copy.Header,
		"footer":         copy.Footer,
		"side":           side,
		"page":           page.Number,
		"folio_position": fmt.Sprintf("%s outer footer edge", outer),
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(page.Prompt), &decoded); err == nil {
		decoded["page_furniture"] = payload
		return compactJSON(decoded)
	}
	return page.Prompt + "\n\nPAGE FURNITURE JSON: " + compactJSON(payload)
}

func (s *server) runDefapiText(ctx context.Context, prompt string, maxTokens int) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, s.cfg.DefapiTextTimeout)
	defer cancel()
	args := commandArgs(s.cfg.DefapiTextCategory, textModelFromContext(ctx, s.cfg.DefapiTextModel), "--stream=false", "-max-tokens", strconv.Itoa(maxTokens), prompt)
	cmd := exec.CommandContext(cctx, s.cfg.DefapiTextCmd, args...)
	cmd.Env = commandEnv(ctx)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("defapi text timed out after %s: %w", s.cfg.DefapiTextTimeout, err)
		}
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (s *server) runDefapiTextJSON(ctx context.Context, prompt string, maxTokens int, out any) error {
	var lastErr error
	current := prompt
	workspace, _ := ctx.Value(workspaceContextKey{}).(string)
	for attempt := 0; attempt < 2; attempt++ {
		attemptTokens := maxTokens
		if attempt > 0 {
			attemptTokens = max(maxTokens*2, 4000)
			if attemptTokens > 12000 {
				attemptTokens = 12000
			}
		}
		text, err := s.runDefapiText(ctx, current, attemptTokens)
		if err != nil {
			lastErr = err
			s.workspaceLog(workspace, "defapi text-json: attempt=%d max_tokens=%d error=%v", attempt+1, attemptTokens, err)
		} else if strings.TrimSpace(text) == "" {
			lastErr = errors.New("empty defapi text response")
			s.workspaceLog(workspace, "defapi text-json: attempt=%d max_tokens=%d empty response", attempt+1, attemptTokens)
		} else if err := json.Unmarshal([]byte(extractJSONObject(text)), out); err != nil {
			lastErr = fmt.Errorf("%w: response=%q", err, compact(text, 500))
			s.workspaceLog(workspace, "defapi text-json: attempt=%d max_tokens=%d invalid json error=%v raw_output:\n%s", attempt+1, attemptTokens, err, compact(text, 4000))
		} else {
			if attempt > 0 {
				s.workspaceLog(workspace, "defapi text-json: attempt=%d max_tokens=%d recovered with valid JSON", attempt+1, attemptTokens)
			}
			return nil
		}
		current = prompt + "\n\nYour previous response was not valid JSON. Return only one valid compact JSON object. No markdown, no prose, no code fence."
	}
	return lastErr
}

func (s *server) runDefapiImageWithRetry(ctx context.Context, workspace string, pageNumber int, prompt string, images []string) (generatedImage, error) {
	var lastErr error
	currentPrompt := prompt
	for attempt := 0; attempt <= s.cfg.DefapiImageRetries; attempt++ {
		if attempt > 0 {
			s.workspaceLog(workspace, "defapi image-retry: page=%d attempt=%d/%d", pageNumber, attempt, s.cfg.DefapiImageRetries)
		}
		image, err := s.runDefapiImage(ctx, workspace, pageNumber, currentPrompt, images)
		if err == nil {
			return image, nil
		}
		lastErr = err
		s.workspaceLog(workspace, "defapi image-retry: page=%d attempt=%d failed: %v", pageNumber, attempt+1, err)
		if attempt < s.cfg.DefapiImageRetries {
			timer := time.NewTimer(time.Duration(attempt+1) * 4 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return generatedImage{}, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return generatedImage{}, lastErr
}

func (s *server) runDefapiImage(ctx context.Context, workspace string, pageNumber int, prompt string, images []string) (generatedImage, error) {
	cctx, cancel := context.WithTimeout(ctx, s.cfg.DefapiImageTimeout)
	defer cancel()
	filename := fmt.Sprintf("page-%02d-%d.jpg", pageNumber, time.Now().UnixNano())
	out := filepath.Join(s.workspaceDir(workspace), "renders", filename)
	args := []string{
		"-format", s.cfg.DefapiImageFormat,
		"-size", s.cfg.DefapiImageSize,
		"-quality", s.cfg.DefapiImageQuality,
		"-background", s.cfg.DefapiImageBackground,
		"-output", out,
	}
	refs := s.defapiImageRefs(cctx, workspace, images)
	for _, ref := range refs {
		args = append(args, "-image", ref)
	}
	s.workspaceLog(workspace, "defapi image: page=%d prompt_chars=%d input_refs=%d accepted_refs=%d", pageNumber, len([]rune(prompt)), len(images), len(refs))
	args = append(commandArgs(s.cfg.DefapiImageCategory, imageModelFromContext(ctx, s.cfg.DefapiImageModel)), append(args, smartLimitImagePrompt(prompt, s.cfg.DefapiImageMaxPromptChars))...)
	cmd := exec.CommandContext(cctx, s.cfg.DefapiImageCmd, args...)
	cmd.Env = commandEnv(ctx)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return generatedImage{}, fmt.Errorf("defapi image timed out after %s: %w", s.cfg.DefapiImageTimeout, err)
		}
		if stderr.Len() > 0 {
			s.workspaceLog(workspace, "defapi image: page=%d stderr=%s", pageNumber, strings.TrimSpace(stderr.String()))
			return generatedImage{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return generatedImage{}, err
	}
	if outText := strings.TrimSpace(stdout.String()); outText != "" {
		s.workspaceLog(workspace, "defapi image: page=%d stdout=%s", pageNumber, outText)
	}
	if _, err := os.Stat(out); err != nil {
		return generatedImage{}, fmt.Errorf("expected defapi image to create %s: %w", out, err)
	}
	publicURL := parseImageURL(stdout.String())
	if publicURL == "" {
		s.workspaceLog(workspace, "defapi image: page=%d warning=no public Image URL in stdout", pageNumber)
	}
	return generatedImage{Image: s.workspaceURL(workspace, "renders/"+filename), PublicURL: publicURL}, nil
}

func commandArgs(parts ...string) []string {
	args := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			args = append(args, part)
		}
	}
	return args
}

func defapiImageRef(imageRef string) string {
	imageRef = strings.TrimSpace(imageRef)
	if strings.HasPrefix(imageRef, "http://") || strings.HasPrefix(imageRef, "https://") {
		return strings.ReplaceAll(imageRef, ",", "%2C")
	}
	return ""
}

func brandAssetRefsForPage(page pagePlan, assets []brandAsset) []string {
	if page.Kind == "advert" || page.Kind == "poster" {
		return nil
	}
	refs := []string{}
	for _, asset := range assets {
		if asset.PublicURL != "" {
			refs = append(refs, asset.PublicURL)
		}
		if len(refs) >= 1 {
			break
		}
	}
	return refs
}

func (s *server) defapiImageRefs(ctx context.Context, workspace string, images []string) []string {
	refs := []string{}
	for _, imageRef := range limitStrings(uniqueStrings(images), 10) {
		ref := defapiImageRef(imageRef)
		if ref == "" {
			if strings.TrimSpace(imageRef) != "" {
				s.workspaceLog(workspace, "defapi image-ref: skipped non-public ref=%q", imageRef)
			}
			continue
		}
		contentType, err := defapiImageContentType(ctx, ref)
		if err != nil {
			s.workspaceLog(workspace, "defapi image-ref: skipped ref=%q error=%v", ref, err)
			continue
		}
		if !allowedDefapiImageContentType(contentType) {
			s.workspaceLog(workspace, "defapi image-ref: skipped ref=%q content_type=%q", ref, contentType)
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

func defapiImageContentType(ctx context.Context, imageRef string) (string, error) {
	contentType, status, err := requestImageContentType(ctx, http.MethodHead, imageRef)
	if err == nil {
		return contentType, nil
	}
	if status != http.StatusMethodNotAllowed && status != http.StatusForbidden {
		return "", err
	}
	contentType, _, getErr := requestImageContentType(ctx, http.MethodGet, imageRef)
	if getErr != nil {
		return "", getErr
	}
	return contentType, nil
}

func requestImageContentType(ctx context.Context, method, imageRef string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, imageRef, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "magazine-builder/0.1")
	if method == http.MethodGet {
		req.Header.Set("Range", "bytes=0-0")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, fmt.Errorf("%s returned HTTP %d", method, resp.StatusCode)
	}
	return resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

func allowedDefapiImageContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "image/jpeg", "image/jpg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func parseImageURL(output string) string {
	m := regexp.MustCompile(`(?im)^\s*Image URL:\s*(\S+)\s*$`).FindStringSubmatch(output)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func (s *server) renderURLToPath(workspace, imageURL string) (string, bool) {
	imageURL = strings.TrimSpace(imageURL)
	prefix := s.workspaceURL(workspace, "renders/")
	if !strings.HasPrefix(imageURL, prefix) {
		return "", false
	}
	name := filepath.Base(imageURL)
	return filepath.Join(s.workspaceDir(workspace), "renders", name), true
}

func contextWithAPIKey(ctx context.Context, apiKey string) context.Context {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ctx
	}
	return context.WithValue(ctx, apiKeyContextKey{}, apiKey)
}

func contextWithModels(ctx context.Context, textModel, imageModel string) context.Context {
	if textModel = strings.TrimSpace(textModel); textModel != "" {
		ctx = context.WithValue(ctx, textModelContextKey{}, textModel)
	}
	if imageModel = strings.TrimSpace(imageModel); imageModel != "" {
		ctx = context.WithValue(ctx, imageModelContextKey{}, imageModel)
	}
	return ctx
}

func contextWithWorkspace(ctx context.Context, workspace string) context.Context {
	workspace = sanitizeWorkspace(workspace)
	if workspace == "" {
		return ctx
	}
	return context.WithValue(ctx, workspaceContextKey{}, workspace)
}

func textModelFromContext(ctx context.Context, fallback string) string {
	if model, _ := ctx.Value(textModelContextKey{}).(string); strings.TrimSpace(model) != "" {
		return strings.TrimSpace(model)
	}
	return fallback
}

func imageModelFromContext(ctx context.Context, fallback string) string {
	if model, _ := ctx.Value(imageModelContextKey{}).(string); strings.TrimSpace(model) != "" {
		return strings.TrimSpace(model)
	}
	return fallback
}

func commandEnv(ctx context.Context) []string {
	env := os.Environ()
	apiKey, _ := ctx.Value(apiKeyContextKey{}).(string)
	if strings.TrimSpace(apiKey) != "" {
		env = append(env, "DEFAPI_API_KEY="+strings.TrimSpace(apiKey))
	}
	return env
}
