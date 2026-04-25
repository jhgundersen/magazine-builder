package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type config struct {
	Addr                   string
	WorkDir                string
	TextgenCmd             string
	TextgenModel           string
	TextgenTimeout         time.Duration
	ImagegenCmd            string
	ImagegenModel          string
	ImagegenTimeout        time.Duration
	ImagegenFormat         string
	ImagegenSize           string
	ImagegenQuality        string
	ImagegenBackground     string
	ImagegenMaxPromptChars int
}

type server struct {
	cfg config
}

type styleRequest struct {
	Style string `json:"style"`
}

type styleResponse struct {
	EnhancedStyle string        `json:"enhancedStyle"`
	Style         magazineStyle `json:"style"`
	ReferencePath string        `json:"referencePath,omitempty"`
}

type rssRequest struct {
	URL   string `json:"url"`
	Limit int    `json:"limit"`
}

type article struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Images []string `json:"images"`
	Source string   `json:"source,omitempty"`
}

type buildRequest struct {
	MagazineType string    `json:"magazineType"`
	Title        string    `json:"title"`
	Style        string    `json:"style"`
	PageCount    int       `json:"pageCount"`
	Articles     []article `json:"articles"`
}

type buildResponse struct {
	Style       magazineStyle `json:"style"`
	CreativeKit creativeKit   `json:"creativeKit"`
	Pages       []pagePlan    `json:"pages"`
	Reference   string        `json:"reference,omitempty"`
}

type pagePlan struct {
	Number  int      `json:"number"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Prompt  string   `json:"prompt"`
	Images  []string `json:"images,omitempty"`
	Article *article `json:"article,omitempty"`
}

type magazineStyle struct {
	Name       string `json:"name"`
	Core       string `json:"core"`
	Cover      string `json:"cover"`
	Content    string `json:"content"`
	Feature    string `json:"feature"`
	Short      string `json:"short"`
	Advert     string `json:"advert"`
	Filler     string `json:"filler"`
	Back       string `json:"back"`
	Template   string `json:"template"`
	Typography string `json:"typography"`
	Color      string `json:"color"`
	Print      string `json:"print"`
	Avoid      string `json:"avoid"`
}

type creativeKit struct {
	Departments []string `json:"departments"`
	Adverts     []string `json:"adverts"`
	Sidebars    []string `json:"sidebars"`
	Captions    []string `json:"captions"`
	BackPage    []string `json:"backPage"`
}

type renderPageRequest struct {
	Page           pagePlan `json:"page"`
	StyleReference string   `json:"styleReference"`
	Reference      string   `json:"reference"`
}

type renderPageResponse struct {
	Image string `json:"image"`
}

type templateRequest struct {
	Style     magazineStyle `json:"style"`
	Reference string        `json:"reference"`
}

type pdfRequest struct {
	Images []string `json:"images"`
	Title  string   `json:"title"`
}

type pdfResponse struct {
	PDF string `json:"pdf"`
}

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
	Entries []atomEntry `xml:"entry"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Content     string `xml:"encoded"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	Link    []atomLink `xml:"link"`
	Summary string     `xml:"summary"`
	Content string     `xml:"content"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
}

func main() {
	cfg := parseFlags()
	for _, dir := range []string{"uploads", "renders"} {
		if err := os.MkdirAll(filepath.Join(cfg.WorkDir, dir), 0o755); err != nil {
			log.Fatal(err)
		}
	}
	s := &server{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/enhance-style", s.handleEnhanceStyle)
	mux.HandleFunc("/api/import-rss", s.handleImportRSS)
	mux.HandleFunc("/api/build", s.handleBuild)
	mux.HandleFunc("/api/render-page", s.handleRenderPage)
	mux.HandleFunc("/api/render-template", s.handleRenderTemplate)
	mux.HandleFunc("/api/write-pdf", s.handleWritePDF)
	mux.Handle("/renders/", http.StripPrefix("/renders/", http.FileServer(http.Dir(filepath.Join(cfg.WorkDir, "renders")))))
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(filepath.Join(cfg.WorkDir, "uploads")))))
	log.Printf("magazine-builder listening on http://localhost%s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.Addr, "addr", ":8080", "HTTP listen address")
	flag.StringVar(&cfg.WorkDir, "workdir", "magazine-work", "directory for uploads and generated artifacts")
	flag.StringVar(&cfg.TextgenCmd, "textgen", "textgen", "textgen command")
	flag.StringVar(&cfg.TextgenModel, "textgen-model", "claude", "textgen model subcommand")
	flag.DurationVar(&cfg.TextgenTimeout, "textgen-timeout", 90*time.Second, "textgen timeout")
	flag.StringVar(&cfg.ImagegenCmd, "imagegen", "imagegen", "imagegen command")
	flag.StringVar(&cfg.ImagegenModel, "imagegen-model", "gpt2", "imagegen model subcommand")
	flag.DurationVar(&cfg.ImagegenTimeout, "imagegen-timeout", 20*time.Minute, "imagegen timeout per page")
	flag.StringVar(&cfg.ImagegenFormat, "imagegen-format", "jpeg", "imagegen output format")
	flag.StringVar(&cfg.ImagegenSize, "imagegen-size", "9:16", "imagegen output size")
	flag.StringVar(&cfg.ImagegenQuality, "imagegen-quality", "high", "imagegen output quality")
	flag.StringVar(&cfg.ImagegenBackground, "imagegen-background", "opaque", "imagegen background")
	flag.IntVar(&cfg.ImagegenMaxPromptChars, "imagegen-max-prompt-chars", 4000, "maximum imagegen prompt length")
	flag.Parse()
	return cfg
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(w, nil); err != nil {
		log.Printf("index template: %v", err)
	}
}

func (s *server) handleEnhanceStyle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	style := strings.TrimSpace(r.FormValue("style"))
	referencePath, err := s.saveUpload(r.MultipartForm, "reference")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	enhanced, err := s.enhanceStyle(r.Context(), style, referencePath)
	if err != nil {
		log.Printf("textgen style enhancement failed: %v", err)
		enhanced = fallbackStyle(style, referencePath)
	}
	styleJSON, _ := json.MarshalIndent(enhanced, "", "  ")
	writeJSON(w, styleResponse{EnhancedStyle: string(styleJSON), Style: enhanced, ReferencePath: referencePath})
}

func (s *server) handleImportRSS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req rssRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Limit <= 0 || req.Limit > 50 {
		req.Limit = 10
	}
	articles, err := fetchRSS(r.Context(), req.URL, req.Limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, map[string]any{"articles": articles})
}

func (s *server) handleBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req buildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.PageCount = normalizePageCount(req.PageCount)
	req.Articles = cleanArticles(req.Articles)
	style := parseStyle(req.Style)
	kit, err := s.generateCreativeKit(r.Context(), req, style)
	if err != nil {
		log.Printf("textgen creative kit failed: %v", err)
		kit = fallbackCreativeKit(req)
	}
	writeJSON(w, buildResponse{Style: style, CreativeKit: kit, Pages: planMagazine(req, style, kit)})
}

func (s *server) handleRenderTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req templateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	prompt := limitPrompt(fmt.Sprintf("Create a blank reusable content-page production template image for this publication.\nSTYLE: %s\nTEMPLATE: %s\nIt must look like an empty printed page ready for content: masthead/header, folio, margins, grid, faint column guides, empty image boxes, caption zones, section furniture. No readable article text, no labels, no arrows, no moodboard.", styleLine(req.Style, "template"), req.Style.Template), s.cfg.ImagegenMaxPromptChars)
	image, err := s.runImagegen(r.Context(), 0, prompt, filterStrings([]string{req.Reference}))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, renderPageResponse{Image: image})
}

func (s *server) handleRenderPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req renderPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	images := filterStrings([]string{req.StyleReference, req.Reference})
	images = append(images, req.Page.Images...)
	image, err := s.runImagegen(r.Context(), req.Page.Number, limitPrompt(req.Page.Prompt, s.cfg.ImagegenMaxPromptChars), images)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, renderPageResponse{Image: image})
}

func (s *server) handleWritePDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req pdfRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	paths := make([]string, 0, len(req.Images))
	for _, imageURL := range req.Images {
		path, ok := s.renderURLToPath(imageURL)
		if ok {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("no rendered images"))
		return
	}
	name := safeName(emptyDefault(req.Title, "magazine")) + "-" + time.Now().Format("20060102-150405") + ".pdf"
	out := filepath.Join(s.cfg.WorkDir, "renders", name)
	if err := writeImagePDF(out, paths); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, pdfResponse{PDF: "/renders/" + name})
}

func (s *server) saveUpload(form *multipart.Form, name string) (string, error) {
	if form == nil || form.File == nil || len(form.File[name]) == 0 {
		return "", nil
	}
	file, err := form.File[name][0].Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 12<<20))
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	sum := sha1.Sum(data)
	ext := strings.ToLower(filepath.Ext(form.File[name][0].Filename))
	if ext == "" || len(ext) > 8 {
		ext = ".bin"
	}
	filename := hex.EncodeToString(sum[:]) + ext
	path := filepath.Join(s.cfg.WorkDir, "uploads", filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return "/uploads/" + filename, nil
}

func (s *server) enhanceStyle(ctx context.Context, style, referencePath string) (magazineStyle, error) {
	prompt := "Return only valid compact JSON for a reusable magazine/newspaper style. No markdown. Keep each field under 220 characters so later image prompts stay under 4000 chars. Required keys: name, core, cover, content, feature, short, advert, filler, back, template, typography, color, print, avoid. Define different guidance for cover, normal content pages, feature articles, short articles, adverts, filler/departments, back page, and the blank content template image.\n\nUser style:\n" + emptyDefault(style, "clean contemporary general-interest magazine")
	if referencePath != "" {
		prompt += "\n\nA reference image was uploaded. Infer palette, typography mood, texture and layout feeling from it, but describe the reusable style in words."
	}
	text, err := s.runTextgen(ctx, prompt, 1200)
	if err != nil {
		return magazineStyle{}, err
	}
	parsed, err := decodeStyle(text)
	if err != nil {
		return magazineStyle{}, err
	}
	return parsed, nil
}

func (s *server) generateCreativeKit(ctx context.Context, req buildRequest, style magazineStyle) (creativeKit, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON. Required keys: departments, adverts, sidebars, captions, backPage. Each value is an array of 5-8 short strings. Prepare reusable generic page elements for a %s called %q. Match this style: %s. Vary advert ideas and article modules. Avoid copyrighted brands unless supplied by the user.\n\nArticles:\n%s", emptyDefault(req.MagazineType, "magazine"), emptyDefault(req.Title, "Untitled Magazine"), styleLine(style, "content"), articleList(req.Articles))
	text, err := s.runTextgen(ctx, prompt, 1200)
	if err != nil {
		return creativeKit{}, err
	}
	kit, err := decodeCreativeKit(text)
	if err != nil {
		return creativeKit{}, err
	}
	return kit, nil
}

func (s *server) runTextgen(ctx context.Context, prompt string, maxTokens int) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, s.cfg.TextgenTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, s.cfg.TextgenCmd, s.cfg.TextgenModel, "-max-tokens", strconv.Itoa(maxTokens), prompt)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("textgen timed out after %s: %w", s.cfg.TextgenTimeout, err)
		}
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (s *server) runImagegen(ctx context.Context, pageNumber int, prompt string, images []string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, s.cfg.ImagegenTimeout)
	defer cancel()
	filename := fmt.Sprintf("page-%02d-%d.jpg", pageNumber, time.Now().UnixNano())
	out := filepath.Join(s.cfg.WorkDir, "renders", filename)
	args := []string{
		s.cfg.ImagegenModel,
		"-format", s.cfg.ImagegenFormat,
		"-size", s.cfg.ImagegenSize,
		"-quality", s.cfg.ImagegenQuality,
		"-background", s.cfg.ImagegenBackground,
		"-output", out,
	}
	for _, img := range limitStrings(uniqueStrings(images), 16) {
		args = append(args, "-image", img)
	}
	args = append(args, limitPrompt(prompt, s.cfg.ImagegenMaxPromptChars))
	cmd := exec.CommandContext(cctx, s.cfg.ImagegenCmd, args...)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("imagegen timed out after %s: %w", s.cfg.ImagegenTimeout, err)
		}
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	if _, err := os.Stat(out); err != nil {
		return "", fmt.Errorf("expected imagegen to create %s: %w", out, err)
	}
	return "/renders/" + filename, nil
}

func (s *server) renderURLToPath(imageURL string) (string, bool) {
	imageURL = strings.TrimSpace(imageURL)
	if !strings.HasPrefix(imageURL, "/renders/") {
		return "", false
	}
	name := filepath.Base(imageURL)
	return filepath.Join(s.cfg.WorkDir, "renders", name), true
}

func fetchRSS(ctx context.Context, feedURL string, limit int) ([]article, error) {
	feedURL = strings.TrimSpace(feedURL)
	if feedURL == "" {
		return nil, errors.New("missing RSS URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "magazine-builder/0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("RSS fetch returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var feed rssFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, err
	}
	articles := make([]article, 0, limit)
	for _, item := range feed.Channel.Items {
		body := item.Content
		if body == "" {
			body = item.Description
		}
		articles = append(articles, article{Title: cleanText(item.Title), Body: cleanText(stripHTML(body)), Source: strings.TrimSpace(item.Link), Images: extractImageURLs(body)})
		if len(articles) >= limit {
			return articles, nil
		}
	}
	for _, entry := range feed.Entries {
		link := ""
		if len(entry.Link) > 0 {
			link = entry.Link[0].Href
		}
		body := entry.Content
		if body == "" {
			body = entry.Summary
		}
		articles = append(articles, article{Title: cleanText(entry.Title), Body: cleanText(stripHTML(body)), Source: link, Images: extractImageURLs(body)})
		if len(articles) >= limit {
			break
		}
	}
	return articles, nil
}

func planMagazine(req buildRequest, style magazineStyle, kit creativeKit) []pagePlan {
	pages := make([]pagePlan, 0, req.PageCount)
	title := emptyDefault(req.Title, "Untitled Magazine")
	magType := emptyDefault(req.MagazineType, "magazine")
	pages = append(pages, pagePlan{Number: 1, Kind: "cover", Title: "Cover", Prompt: coverPrompt(title, magType, style, req.Articles)})
	articleIndex := 0
	for n := 2; n <= req.PageCount; n++ {
		if n == req.PageCount {
			pages = append(pages, pagePlan{Number: n, Kind: "back-page", Title: "Back page", Prompt: genericPrompt(n, title, style, kit, "back", "Create a strong back page: advert, subscription panel, teaser, index, or closing visual depending on the publication style.")})
			continue
		}
		if isAdvertPage(n, req.PageCount) {
			pages = append(pages, pagePlan{Number: n, Kind: "advert", Title: "Advert", Prompt: genericPrompt(n, title, style, kit, "advert", "Create a full-page fictional advert that belongs naturally in this publication. Use no real brands unless supplied by the article content.")})
			continue
		}
		if articleIndex < len(req.Articles) {
			a := req.Articles[articleIndex]
			articleIndex++
			kind := "article"
			if len([]rune(a.Body)) > 1800 {
				kind = "feature"
			}
			pages = append(pages, pagePlan{Number: n, Kind: kind, Title: a.Title, Article: &a, Images: a.Images, Prompt: articlePrompt(n, title, style, kit, kind, a)})
			continue
		}
		pages = append(pages, pagePlan{Number: n, Kind: "filler", Title: "Departments", Prompt: genericPrompt(n, title, style, kit, "filler", "Create a department/filler page with short recurring modules, briefs, reader notes, charts, sidebars, small adverts, captions, and visual rhythm suited to the publication.")})
	}
	return pages
}

func coverPrompt(title, magType string, style magazineStyle, articles []article) string {
	return limitPrompt(fmt.Sprintf("Create page 1, the cover of %q, a %s.\nSTYLE: %s\nCOVER STYLE: %s\nUse cover lines for: %s\nInclude masthead, issue date, price/barcode or equivalent furniture, strong hierarchy, and avoid: %s.", title, magType, styleLine(style, "core"), style.Cover, articleTitles(articles, 6), style.Avoid), 3900)
}

func articlePrompt(n int, title string, style magazineStyle, kit creativeKit, kind string, a article) string {
	taskStyle := styleLine(style, kind)
	modules := strings.Join(pickStrings(append(kit.Sidebars, kit.Captions...), n, 3), "; ")
	return limitPrompt(fmt.Sprintf("Create page %d of %q as a %s page.\nSTYLE: %s\nPAGE STYLE: %s\nMODULE IDEAS: %s\nARTICLE: %s\nBODY: %s\nLayout with headline, deck, byline/source if available, columns, image slots, captions, pull quote/sidebar where useful, and page number %d.", n, title, kind, styleLine(style, "content"), taskStyle, modules, a.Title, compact(a.Body, 1900), n), 3900)
}

func genericPrompt(n int, title string, style magazineStyle, kit creativeKit, kind, task string) string {
	modules := creativeLine(kit, kind, n)
	return limitPrompt(fmt.Sprintf("Create page %d of %q.\nSTYLE: %s\nPAGE STYLE: %s\nIDEAS: %s\nTASK: %s\nInclude consistent header/footer/page number treatment for page %d.", n, title, styleLine(style, "content"), styleLine(style, kind), modules, task, n), 3900)
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

func cleanArticles(in []article) []article {
	out := make([]article, 0, len(in))
	for _, a := range in {
		a.Title = cleanText(a.Title)
		a.Body = cleanText(a.Body)
		if a.Title == "" && a.Body == "" {
			continue
		}
		out = append(out, a)
	}
	return out
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

func fallbackStyle(style, referencePath string) magazineStyle {
	base := emptyDefault(style, "A polished general-interest magazine with clear editorial hierarchy")
	if referencePath != "" {
		base += ". Use the uploaded reference image as visual inspiration for palette, texture, typography mood, and image treatment."
	}
	return magazineStyle{
		Name:       "Custom magazine",
		Core:       compact(base, 210),
		Cover:      "large masthead, confident cover lines, one dominant image, date/price/barcode if fitting",
		Content:    "consistent grid, clear folios, restrained page furniture, modular image and text rhythm",
		Feature:    "more generous opening image, pull quote, sidebar, longer headline and stronger hierarchy",
		Short:      "compact brief layout, small image, dense but readable columns, one small sidebar",
		Advert:     "fictional full-page advert using the same print world, distinct from editorial pages",
		Filler:     "departments, briefs, charts, reader notes, small classifieds and recurring modules",
		Back:       "closing advert, subscription panel, teaser or striking single visual",
		Template:   "blank content-page production dummy: header, folio, grid, image boxes, no readable text",
		Typography: "strong masthead, readable serif or humanist body, compact captions and section labels",
		Color:      "limited coherent palette with one warm and one cool accent",
		Print:      "tactile paper, realistic print texture, subtle scan/photo imperfections",
		Avoid:      "generic web UI, floating app cards, unreadable logo, mismatched styles, real brands unless provided",
	}
}

func fallbackCreativeKit(req buildRequest) creativeKit {
	return creativeKit{
		Departments: []string{"editor's note", "short briefs", "reader mail", "listings", "numbers panel", "what's next"},
		Adverts:     []string{"small classified ad", "full-page fictional supplier advert", "subscription offer", "event notice", "service directory"},
		Sidebars:    []string{"key facts", "timeline", "quote box", "how it works", "recommended next read", "source notes"},
		Captions:    []string{"dry editorial caption", "technical caption", "behind-the-scenes note", "short contextual label"},
		BackPage:    []string{"subscription panel", "single bold advert", "teaser for next issue", "index and closing note"},
	}
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
	fallback := fallbackStyle("", "")
	if style.Name == "" {
		style.Name = fallback.Name
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
	if style.Template == "" {
		style.Template = fallback.Template
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
	return style, nil
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
	if len(kit.Captions) == 0 {
		kit.Captions = fallback.Captions
	}
	if len(kit.BackPage) == 0 {
		kit.BackPage = fallback.BackPage
	}
	return kit, nil
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

func styleLine(style magazineStyle, kind string) string {
	var specific string
	switch kind {
	case "cover":
		specific = style.Cover
	case "feature":
		specific = style.Feature
	case "short", "article":
		specific = style.Short
	case "advert":
		specific = style.Advert
	case "filler":
		specific = style.Filler
	case "back", "back-page":
		specific = style.Back
	case "template":
		specific = style.Template
	default:
		specific = style.Content
	}
	return compact(strings.Join([]string{style.Core, style.Typography, style.Color, style.Print, specific, "Avoid: " + style.Avoid}, " "), 900)
}

func creativeLine(kit creativeKit, kind string, seed int) string {
	var pool []string
	switch kind {
	case "advert":
		pool = kit.Adverts
	case "back", "back-page":
		pool = append(kit.BackPage, kit.Adverts...)
	case "filler":
		pool = append(kit.Departments, kit.Sidebars...)
	default:
		pool = append(kit.Sidebars, kit.Captions...)
	}
	return strings.Join(pickStrings(pool, seed, 4), "; ")
}

func pickStrings(in []string, seed, n int) []string {
	if len(in) == 0 || n <= 0 {
		return nil
	}
	out := make([]string, 0, n)
	for i := 0; i < n && i < len(in); i++ {
		out = append(out, in[(seed+i)%len(in)])
	}
	return out
}

var tagRE = regexp.MustCompile(`<[^>]+>`)
var spaceRE = regexp.MustCompile(`\s+`)
var imageRE = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)

func stripHTML(s string) string { return tagRE.ReplaceAllString(s, " ") }
func cleanText(s string) string { return strings.TrimSpace(spaceRE.ReplaceAllString(s, " ")) }

func extractImageURLs(html string) []string {
	matches := imageRE.FindAllStringSubmatch(html, -1)
	seen := map[string]bool{}
	out := []string{}
	for _, m := range matches {
		u := strings.TrimSpace(m[1])
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

func compact(s string, max int) string {
	s = cleanText(s)
	if len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "..."
}

func limitPrompt(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	note := "\n\n[Prompt shortened: keep page task, style, title, number, and main content.] "
	r := []rune(s)
	noteRunes := []rune(note)
	head := (max - len(noteRunes)) * 2 / 3
	tail := max - len(noteRunes) - head
	if head < 1 || tail < 1 {
		return string(r[:max])
	}
	return string(r[:head]) + note + string(r[len(r)-tail:])
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func filterStrings(in []string) []string {
	out := []string{}
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func limitStrings(in []string, max int) []string {
	if max <= 0 || len(in) <= max {
		return in
	}
	return in[:max]
}

func safeName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 && b.String()[b.Len()-1] != '-' {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "magazine"
	}
	return out
}

func emptyDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return strings.TrimSpace(s)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
}

const (
	pageWidth  = 1240
	pageHeight = 1754
)

func writeImagePDF(path string, images []string) error {
	var out bytes.Buffer
	write := func(format string, args ...any) { fmt.Fprintf(&out, format, args...) }
	offsets := []int{0}
	write("%%PDF-1.4\n")
	obj := 1
	catalogObj := obj
	obj++
	pagesObj := obj
	obj++
	type pageObj struct{ page, image, content int }
	objects := make([]pageObj, 0, len(images))
	for range images {
		objects = append(objects, pageObj{page: obj, image: obj + 1, content: obj + 2})
		obj += 3
	}
	offsets = make([]int, obj)
	offsets[catalogObj] = out.Len()
	write("%d 0 obj\n<< /Type /Catalog /Pages %d 0 R >>\nendobj\n", catalogObj, pagesObj)
	offsets[pagesObj] = out.Len()
	write("%d 0 obj\n<< /Type /Pages /Count %d /Kids [", pagesObj, len(objects))
	for _, po := range objects {
		write(" %d 0 R", po.page)
	}
	write(" ] >>\nendobj\n")
	for i, imgPath := range images {
		data, err := os.ReadFile(imgPath)
		if err != nil {
			return err
		}
		cfg, err := imageConfig(imgPath)
		if err != nil {
			return err
		}
		content := fmt.Sprintf("q\n%.2f 0 0 %.2f 0 0 cm\n/Im%d Do\nQ\n", float64(pageWidth), float64(pageHeight), i+1)
		offsets[objects[i].page] = out.Len()
		write("%d 0 obj\n<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %d %d] /Resources << /XObject << /Im%d %d 0 R >> >> /Contents %d 0 R >>\nendobj\n",
			objects[i].page, pagesObj, pageWidth, pageHeight, i+1, objects[i].image, objects[i].content)
		offsets[objects[i].image] = out.Len()
		write("%d 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n",
			objects[i].image, cfg.Width, cfg.Height, len(data))
		out.Write(data)
		write("\nendstream\nendobj\n")
		offsets[objects[i].content] = out.Len()
		write("%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", objects[i].content, len(content), content)
	}
	xref := out.Len()
	write("xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for i := 1; i < len(offsets); i++ {
		write("%010d 00000 n \n", offsets[i])
	}
	write("trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), catalogObj, xref)
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func imageConfig(path string) (image.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Config{}, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	return cfg, err
}

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Magazine Builder</title>
<style>
:root{color-scheme:light;--ink:#172026;--muted:#62717b;--line:#d8dee3;--panel:#f5f7f8;--accent:#b3261e;--blue:#235789;--paper:#fffdf8}*{box-sizing:border-box}body{margin:0;font:15px/1.45 system-ui,-apple-system,Segoe UI,sans-serif;color:var(--ink);background:#e9edf0}.app{display:grid;grid-template-columns:360px 1fr;min-height:100vh}.side{background:var(--paper);border-right:1px solid var(--line);padding:18px;overflow:auto}.main{padding:18px;overflow:auto}h1{font-size:22px;margin:0 0 16px}h2{font-size:15px;margin:22px 0 8px;text-transform:uppercase;letter-spacing:.04em;color:#34444f}label{display:block;font-weight:650;margin:12px 0 5px}input,select,textarea,button{font:inherit}input,select,textarea{width:100%;border:1px solid var(--line);border-radius:6px;background:white;padding:9px;color:var(--ink)}textarea{min-height:96px;resize:vertical}button{border:0;border-radius:6px;background:var(--ink);color:white;padding:9px 12px;font-weight:700;cursor:pointer}button.secondary{background:var(--blue)}button.ghost{background:white;color:var(--ink);border:1px solid var(--line)}.row{display:flex;gap:8px;align-items:center}.row>*{flex:1}.article{border:1px solid var(--line);background:white;border-radius:8px;padding:10px;margin:10px 0}.status{color:var(--muted);font-size:13px;margin:6px 0 10px;min-height:18px}.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(290px,1fr));gap:12px}.page{background:white;border:1px solid var(--line);border-radius:8px;padding:12px;min-height:220px}.page[draggable=true]{cursor:grab}.page.dragging{opacity:.45}.fixed{opacity:.7}.kind{font-size:12px;color:white;background:var(--accent);display:inline-block;border-radius:999px;padding:2px 8px;text-transform:uppercase}.page h3{margin:8px 0;font-size:18px}.prompt{white-space:pre-wrap;color:#26343c;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;background:#f8fafb;border:1px solid var(--line);border-radius:6px;padding:8px;max-height:220px;overflow:auto}.kit{white-space:pre-wrap;background:#fff;border:1px solid var(--line);border-radius:8px;padding:12px;margin-bottom:12px}.toolbar{display:flex;gap:8px;margin-bottom:6px;flex-wrap:wrap}.toolbar button{flex:none}.preview{width:100%;aspect-ratio:9/16;object-fit:cover;border:1px solid var(--line);border-radius:6px;background:#f5f7f8;margin:8px 0}.page-status{font-size:12px;color:var(--muted);margin:4px 0 8px}@media(max-width:850px){.app{grid-template-columns:1fr}.side{border-right:0;border-bottom:1px solid var(--line)}}
</style>
</head>
<body>
<div class="app">
<aside class="side">
<h1>Magazine Builder</h1>
<label>Publication title</label><input id="title" value="New Magazine">
<label>Type</label><select id="magazineType"><option>Magazine</option><option>Newspaper</option><option>Zine</option><option>Catalogue</option><option>Newsletter</option><option>Journal</option></select>
<label>Pages</label><select id="pageCount"><option>4</option><option>8</option><option selected>12</option><option>16</option><option>24</option><option>32</option><option>48</option><option>64</option></select>
<label>Style idea</label><textarea id="style" placeholder="e.g. Nordic architecture quarterly, restrained, tactile paper, black and red accents"></textarea>
<label>Reference image</label><input id="reference" type="file" accept="image/*">
<div class="row"><button class="secondary" id="enhance">Enhance Style</button><button class="ghost" id="clear">Clear</button></div><div id="styleStatus" class="status"></div>
<h2>RSS Import</h2><label>Feed URL</label><input id="rss" placeholder="https://example.com/feed.xml"><button id="importRSS">Import latest</button><div id="rssStatus" class="status"></div>
<h2>Articles</h2><button id="addArticle" class="ghost">Add Article</button><div id="articles"></div>
</aside>
<main class="main">
<div class="toolbar"><button id="build">Build Magazine Plan</button><button id="render" class="secondary">Render Images + PDF</button><button id="download" class="ghost">Download JSON</button></div>
<div id="renderStatus" class="status"></div>
<section id="output"><div class="kit">No plan yet.</div></section>
</main>
</div>
<script>
let articles=[];let lastPlan=null;let referencePath='';let renderedImages={};let draggedIndex=null;
const $=id=>document.getElementById(id);
function esc(s){return String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function renderArticles(){
  const wrap=$('articles');wrap.innerHTML='';
  articles.forEach((a,i)=>{const div=document.createElement('div');div.className='article';div.innerHTML='<label>Title</label><input value="'+esc(a.title||'')+'" data-i="'+i+'" data-k="title"><label>Body</label><textarea data-i="'+i+'" data-k="body">'+esc(a.body||'')+'</textarea><label>Image URLs, one per line</label><textarea data-i="'+i+'" data-k="images">'+esc((a.images||[]).join('\n'))+'</textarea><button class="ghost" data-remove="'+i+'">Remove</button>';wrap.appendChild(div)});
  wrap.querySelectorAll('input,textarea').forEach(el=>el.oninput=e=>{const i=+e.target.dataset.i,k=e.target.dataset.k;articles[i][k]=k==='images'?e.target.value.split('\n').map(s=>s.trim()).filter(Boolean):e.target.value});
  wrap.querySelectorAll('[data-remove]').forEach(el=>el.onclick=e=>{articles.splice(+e.target.dataset.remove,1);renderArticles()})
}
$('addArticle').onclick=()=>{articles.push({title:'',body:'',images:[]});renderArticles()};
$('clear').onclick=()=>{$('style').value='';$('reference').value='';referencePath='';$('styleStatus').textContent=''};
$('enhance').onclick=async()=>{const fd=new FormData();fd.append('style',$('style').value);if($('reference').files[0])fd.append('reference',$('reference').files[0]);$('styleStatus').textContent='Enhancing style JSON...';const res=await fetch('/api/enhance-style',{method:'POST',body:fd});const data=await res.json();if(!res.ok){$('styleStatus').textContent=data.error||'Failed';return}$('style').value=data.enhancedStyle;referencePath=data.referencePath||referencePath;$('styleStatus').textContent=referencePath?'Enhanced with reference image.':'Enhanced.'};
$('importRSS').onclick=async()=>{$('rssStatus').textContent='Importing...';const res=await fetch('/api/import-rss',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:$('rss').value,limit:10})});const data=await res.json();if(!res.ok){$('rssStatus').textContent=data.error||'Failed';return}articles=articles.concat(data.articles||[]);renderArticles();$('rssStatus').textContent='Imported '+(data.articles||[]).length+' articles.'};
$('build').onclick=async()=>{const payload={title:$('title').value,magazineType:$('magazineType').value,style:$('style').value,pageCount:+$('pageCount').value,articles};const out=$('output');out.innerHTML='<div class="kit">Building...</div>';renderedImages={};const res=await fetch('/api/build',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});const data=await res.json();if(!res.ok){out.innerHTML='<div class="kit">'+esc(data.error||'Failed')+'</div>';return}lastPlan=data;lastPlan.reference=referencePath;renderPlan(data)};
$('download').onclick=()=>{if(!lastPlan)return;const blob=new Blob([JSON.stringify(lastPlan,null,2)],{type:'application/json'});const a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download='magazine-plan.json';a.click();URL.revokeObjectURL(a.href)};
$('render').onclick=()=>renderAll();
function renderPlan(data){
  const kit=typeof data.creativeKit==='string'?data.creativeKit:JSON.stringify(data.creativeKit,null,2);
  const pages=(data.pages||[]).map((p,i)=>pageHTML(p,i)).join('');
  $('output').innerHTML='<div class="kit"><strong>Style JSON</strong>\n'+esc(JSON.stringify(data.style||{},null,2))+'\n\n<strong>Creative kit</strong>\n'+esc(kit)+'</div><div id="pageGrid" class="grid">'+pages+'</div>';
  wireDrag();
}
function pageHTML(p,i){const fixed=i===0||i===(lastPlan.pages.length-1);const img=renderedImages[p.number]||'';return '<article class="page '+(fixed?'fixed':'')+'" data-i="'+i+'" draggable="'+(!fixed)+'"><span class="kind">'+esc(p.kind)+'</span><h3>'+p.number+'. '+esc(p.title)+'</h3><div class="page-status" id="status-'+p.number+'">'+(img?'Rendered':'Drag middle pages to reorder')+'</div>'+(img?'<img class="preview" src="'+esc(img)+'">':'<div class="preview"></div>')+'<div class="prompt">'+esc(p.prompt)+'</div></article>'}
function wireDrag(){document.querySelectorAll('.page[draggable=true]').forEach(card=>{card.ondragstart=e=>{draggedIndex=+card.dataset.i;card.classList.add('dragging')};card.ondragend=e=>card.classList.remove('dragging');card.ondragover=e=>e.preventDefault();card.ondrop=e=>{e.preventDefault();const target=+card.dataset.i;if(draggedIndex===null||target===0||target===lastPlan.pages.length-1)return;movePage(draggedIndex,target)}})}
function movePage(from,to){const pages=lastPlan.pages;if(from===0||from===pages.length-1||to===0||to===pages.length-1)return;const [p]=pages.splice(from,1);pages.splice(to,0,p);renumberPages();renderPlan(lastPlan)}
function renumberPages(){lastPlan.pages.forEach((p,i)=>{const old=p.number;p.number=i+1;p.prompt=renumberPrompt(p.prompt,old,p.number)})}
function renumberPrompt(prompt,oldNo,newNo){let s=String(prompt);s=s.replace(new RegExp('page '+oldNo,'gi'),'page '+newNo);s=s.replace(new RegExp('Page '+oldNo,'g'),'Page '+newNo);s=s.replace(new RegExp('side '+oldNo,'gi'),'side '+newNo);s=s.replace(new RegExp('number '+oldNo,'gi'),'number '+newNo);return s}
async function renderAll(){if(!lastPlan){$('renderStatus').textContent='Build a plan first.';return}renderedImages={};renderPlan(lastPlan);$('renderStatus').textContent='Rendering cover...';let cover=await renderPage(lastPlan.pages[0], '');renderedImages[1]=cover;renderPlan(lastPlan);$('renderStatus').textContent='Rendering shared content template...';let template=await renderTemplate();$('renderStatus').textContent='Rendering content pages...';const middle=lastPlan.pages.slice(1);for(let i=0;i<middle.length;i+=3){await Promise.all(middle.slice(i,i+3).map(async p=>{const img=await renderPage(p, template);renderedImages[p.number]=img;renderPlan(lastPlan)}))}const ordered=lastPlan.pages.map(p=>renderedImages[p.number]).filter(Boolean);$('renderStatus').textContent='Writing PDF...';const res=await fetch('/api/write-pdf',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({title:$('title').value,images:ordered})});const data=await res.json();if(!res.ok){$('renderStatus').textContent=data.error||'PDF failed';return}$('renderStatus').innerHTML='Done. <a href="'+esc(data.pdf)+'" target="_blank">Open PDF</a>'}
async function renderTemplate(){const res=await fetch('/api/render-template',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({style:lastPlan.style,reference:referencePath})});const data=await res.json();if(!res.ok)throw new Error(data.error||'template failed');return data.image}
async function renderPage(page,styleReference){setStatus(page.number,'Rendering...');const ref=page.number===1?referencePath:'';const res=await fetch('/api/render-page',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({page,styleReference,reference:ref})});const data=await res.json();if(!res.ok){setStatus(page.number,data.error||'Failed');throw new Error(data.error||'render failed')}setStatus(page.number,'Rendered');return data.image}
function setStatus(n,msg){const el=$('status-'+n);if(el)el.textContent=msg}
renderArticles();
</script>
</body>
</html>`))
