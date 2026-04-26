package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
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
	ImagegenRetries        int
}

type server struct {
	cfg config
}

type apiKeyContextKey struct{}

type styleRequest struct {
	Style  string `json:"style"`
	APIKey string `json:"apiKey"`
}

type styleResponse struct {
	EnhancedStyle string        `json:"enhancedStyle"`
	Style         magazineStyle `json:"style"`
	ReferencePath string        `json:"referencePath,omitempty"`
	Workspace     string        `json:"workspace"`
}

type rssRequest struct {
	URL       string `json:"url"`
	Limit     int    `json:"limit"`
	Style     string `json:"style"`
	Workspace string `json:"workspace"`
	APIKey    string `json:"apiKey"`
}

type generateArticlesRequest struct {
	Title     string `json:"title"`
	Style     string `json:"style"`
	Count     int    `json:"count"`
	Workspace string `json:"workspace"`
	APIKey    string `json:"apiKey"`
}

type article struct {
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Images   []string `json:"images"`
	Source   string   `json:"source,omitempty"`
	Enhanced bool     `json:"enhanced,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Pages    int      `json:"pages,omitempty"`
}

type buildRequest struct {
	MagazineType string    `json:"magazineType"`
	Title        string    `json:"title"`
	Style        string    `json:"style"`
	PageCount    int       `json:"pageCount"`
	Articles     []article `json:"articles"`
	Workspace    string    `json:"workspace"`
	APIKey       string    `json:"apiKey"`
}

type buildResponse struct {
	Style       magazineStyle `json:"style"`
	CreativeKit creativeKit   `json:"creativeKit"`
	Pages       []pagePlan    `json:"pages"`
	Reference   string        `json:"reference,omitempty"`
	Workspace   string        `json:"workspace"`
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
	Workspace      string   `json:"workspace"`
	APIKey         string   `json:"apiKey"`
}

type renderPageResponse struct {
	Image     string `json:"image"`
	PublicURL string `json:"publicUrl,omitempty"`
}

type generatedImage struct {
	Image     string
	PublicURL string
}

type templateRequest struct {
	Style     magazineStyle `json:"style"`
	Reference string        `json:"reference"`
	Workspace string        `json:"workspace"`
	APIKey    string        `json:"apiKey"`
	Prompt    string        `json:"prompt"`
}

type pdfRequest struct {
	Images    []string `json:"images"`
	Title     string   `json:"title"`
	Workspace string   `json:"workspace"`
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
	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		log.Fatal(err)
	}
	s := &server{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/enhance-style", s.handleEnhanceStyle)
	mux.HandleFunc("/api/import-rss", s.handleImportRSS)
	mux.HandleFunc("/api/generate-articles", s.handleGenerateArticles)
	mux.HandleFunc("/api/build", s.handleBuild)
	mux.HandleFunc("/api/render-page", s.handleRenderPage)
	mux.HandleFunc("/api/render-template", s.handleRenderTemplate)
	mux.HandleFunc("/api/write-pdf", s.handleWritePDF)
	mux.Handle("/work/", http.StripPrefix("/work/", http.FileServer(http.Dir(cfg.WorkDir))))
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
	flag.StringVar(&cfg.ImagegenSize, "imagegen-size", "auto", "imagegen output size")
	flag.StringVar(&cfg.ImagegenQuality, "imagegen-quality", "high", "imagegen output quality")
	flag.StringVar(&cfg.ImagegenBackground, "imagegen-background", "opaque", "imagegen background")
	flag.IntVar(&cfg.ImagegenMaxPromptChars, "imagegen-max-prompt-chars", 4000, "maximum imagegen prompt length")
	flag.IntVar(&cfg.ImagegenRetries, "imagegen-retries", 2, "retry attempts for failed imagegen calls")
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
	ctx := contextWithAPIKey(r.Context(), r.FormValue("apiKey"))
	title := strings.TrimSpace(r.FormValue("title"))
	style := strings.TrimSpace(r.FormValue("style"))
	workspace, err := s.ensureWorkspace(r.FormValue("workspace"), style)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	referencePath, err := s.saveUpload(workspace, r.MultipartForm, "reference")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	enhanced, err := s.enhanceStyle(ctx, title, style, referencePath)
	if err != nil {
		log.Printf("textgen style enhancement failed: %v", err)
		enhanced = fallbackStyle(style, referencePath)
	}
	if title != "" {
		enhanced.Name = title
	}
	styleJSON, _ := json.MarshalIndent(enhanced, "", "  ")
	writeJSON(w, styleResponse{EnhancedStyle: string(styleJSON), Style: enhanced, ReferencePath: referencePath, Workspace: workspace})
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
	ctx := contextWithAPIKey(r.Context(), req.APIKey)
	if req.Limit <= 0 || req.Limit > 50 {
		req.Limit = 10
	}
	workspace, err := s.ensureWorkspace(req.Workspace, req.Style)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	articles, err := fetchRSS(ctx, req.URL, req.Limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	style := parseStyle(req.Style)
	for i := range articles {
		improved, err := s.rewriteArticleForStyle(ctx, articles[i], style)
		if err != nil {
			log.Printf("textgen article rewrite failed for %q: %v", articles[i].Title, err)
			continue
		}
		articles[i].Title = improved.Title
		articles[i].Body = improved.Body
		articles[i].Enhanced = true
	}
	writeJSON(w, map[string]any{"articles": articles, "workspace": workspace})
}

func (s *server) handleGenerateArticles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req generateArticlesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx := contextWithAPIKey(r.Context(), req.APIKey)
	if req.Count <= 0 || req.Count > 12 {
		req.Count = 4
	}
	workspace, err := s.ensureWorkspace(req.Workspace, req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	style := parseStyle(req.Style)
	articles, err := s.generateArticles(ctx, req.Title, style, req.Count)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, map[string]any{"articles": articles, "workspace": workspace})
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
	ctx := contextWithAPIKey(r.Context(), req.APIKey)
	req.PageCount = normalizePageCount(req.PageCount)
	req.Articles = cleanArticles(req.Articles)
	style := parseStyle(req.Style)
	workspace, err := s.ensureWorkspace(req.Workspace, req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.workspaceLog(workspace, "build: start title=%q articles=%d pages=%d", req.Title, len(req.Articles), req.PageCount)
	for i := range req.Articles {
		if req.Articles[i].Enhanced {
			continue
		}
		var improved article
		var err error
		if req.Articles[i].Kind == "feature" {
			improved, err = s.rewriteFeatureForStyle(ctx, req.Articles[i], style)
		} else {
			improved, err = s.rewriteArticleForStyle(ctx, req.Articles[i], style)
		}
		if err != nil {
			log.Printf("textgen manual article rewrite failed for %q: %v", req.Articles[i].Title, err)
			continue
		}
		improved.Enhanced = true
		req.Articles[i] = improved
	}
	kit, err := s.generateCreativeKit(ctx, req, style)
	if err != nil {
		log.Printf("textgen creative kit failed: %v", err)
		s.workspaceLog(workspace, "build: creative kit failed: %v", err)
		kit = fallbackCreativeKit(req)
	}
	s.workspaceLog(workspace, "build: complete")
	writeJSON(w, buildResponse{Style: style, CreativeKit: kit, Pages: planMagazine(req, style, kit), Workspace: workspace})
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
	ctx := contextWithAPIKey(r.Context(), req.APIKey)
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = defaultTemplatePrompt(req.Style)
	}
	prompt = limitPrompt(prompt, s.cfg.ImagegenMaxPromptChars)
	workspace, err := s.ensureWorkspace(req.Workspace, req.Style.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.workspaceLog(workspace, "render-template: start")
	image, err := s.runImagegenWithRetry(ctx, workspace, 0, prompt, filterStrings([]string{req.Reference}), true)
	if err != nil {
		s.workspaceLog(workspace, "render-template: failed: %v", err)
		writeError(w, http.StatusBadGateway, err)
		return
	}
	s.workspaceLog(workspace, "render-template: complete image=%s public=%s", image.Image, image.PublicURL)
	writeJSON(w, renderPageResponse{Image: image.Image, PublicURL: image.PublicURL})
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
	ctx := contextWithAPIKey(r.Context(), req.APIKey)
	images := filterStrings([]string{req.StyleReference, req.Reference})
	images = append(images, req.Page.Images...)
	workspace, err := s.ensureWorkspace(req.Workspace, req.Page.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.workspaceLog(workspace, "render-page: start page=%d title=%q refs=%d", req.Page.Number, req.Page.Title, len(images))
	image, err := s.runImagegenWithRetry(ctx, workspace, req.Page.Number, limitPrompt(req.Page.Prompt, s.cfg.ImagegenMaxPromptChars), images, false)
	if err != nil {
		s.workspaceLog(workspace, "render-page: failed page=%d: %v", req.Page.Number, err)
		writeError(w, http.StatusBadGateway, err)
		return
	}
	s.workspaceLog(workspace, "render-page: complete page=%d image=%s public=%s", req.Page.Number, image.Image, image.PublicURL)
	writeJSON(w, renderPageResponse{Image: image.Image, PublicURL: image.PublicURL})
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
	workspace, err := s.ensureWorkspace(req.Workspace, req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.workspaceLog(workspace, "write-pdf: start images=%d", len(req.Images))
	paths := make([]string, 0, len(req.Images))
	for _, imageURL := range req.Images {
		path, ok := s.renderURLToPath(workspace, imageURL)
		if ok {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("no rendered images"))
		return
	}
	name := safeName(emptyDefault(req.Title, "magazine")) + "-" + time.Now().Format("20060102-150405") + ".pdf"
	out := filepath.Join(s.workspaceDir(workspace), "renders", name)
	if err := writeImagePDF(out, paths); err != nil {
		s.workspaceLog(workspace, "write-pdf: failed: %v", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.workspaceLog(workspace, "write-pdf: complete %s", s.workspaceURL(workspace, "renders/"+name))
	writeJSON(w, pdfResponse{PDF: s.workspaceURL(workspace, "renders/"+name)})
}

func (s *server) saveUpload(workspace string, form *multipart.Form, name string) (string, error) {
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
	path := filepath.Join(s.workspaceDir(workspace), "uploads", filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return s.workspaceURL(workspace, "uploads/"+filename), nil
}

func (s *server) ensureWorkspace(workspace, title string) (string, error) {
	workspace = sanitizeWorkspace(workspace)
	if workspace == "" {
		workspace = newWorkspaceID(title)
	}
	for _, dir := range []string{"uploads", "renders"} {
		if err := os.MkdirAll(filepath.Join(s.workspaceDir(workspace), dir), 0o755); err != nil {
			return "", err
		}
	}
	return workspace, nil
}

func (s *server) workspaceLog(workspace, format string, args ...any) {
	workspace = sanitizeWorkspace(workspace)
	if workspace == "" {
		return
	}
	line := time.Now().Format(time.RFC3339) + " " + fmt.Sprintf(format, args...) + "\n"
	_ = os.MkdirAll(s.workspaceDir(workspace), 0o755)
	f, err := os.OpenFile(filepath.Join(s.workspaceDir(workspace), "magazine.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("workspace log failed: %v", err)
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func (s *server) workspaceDir(workspace string) string {
	return filepath.Join(s.cfg.WorkDir, sanitizeWorkspace(workspace))
}

func (s *server) workspaceURL(workspace, rel string) string {
	return "/work/" + sanitizeWorkspace(workspace) + "/" + strings.TrimLeft(rel, "/")
}

func sanitizeWorkspace(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

func newWorkspaceID(title string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102-150405")
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (s *server) enhanceStyle(ctx context.Context, title, style, referencePath string) (magazineStyle, error) {
	prompt := "Return only valid compact JSON for a reusable magazine/newspaper style. No markdown. Keep each field under 220 characters so later image prompts stay under 4000 chars. Required keys: name, core, cover, content, feature, short, advert, filler, back, template, typography, color, print, avoid. The name field must be exactly " + strconv.Quote(emptyDefault(title, "Untitled Magazine")) + ". Define different guidance for cover, normal content pages, feature articles, short articles, adverts, filler/departments, back page, and a completely text-free blank content template image. The template field must describe only generic layout shapes, margins, empty image areas and grid rhythm, with no letters, masthead, captions, labels, technical print marks or readable text.\n\nUser style:\n" + emptyDefault(style, "clean contemporary general-interest magazine")
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
	prompt := fmt.Sprintf("Return only valid compact JSON. Required keys: departments, adverts, sidebars, captions, backPage. Make departments, sidebars and captions arrays of 18-24 unique short strings each. Make adverts and backPage arrays of 10-16 unique short strings each. Every string must describe one specific reusable page element, never a duplicate or near-duplicate. Prepare issue-wide generic page elements for a %s called %q. Match this style: %s. Avoid copyrighted brands unless supplied by the user.\n\nArticles:\n%s", emptyDefault(req.MagazineType, "magazine"), emptyDefault(req.Title, "Untitled Magazine"), styleLine(style, "content"), articleList(req.Articles))
	text, err := s.runTextgen(ctx, prompt, 1800)
	if err != nil {
		return creativeKit{}, err
	}
	kit, err := decodeCreativeKit(text)
	if err != nil {
		return creativeKit{}, err
	}
	return kit, nil
}

func (s *server) rewriteArticleForStyle(ctx context.Context, a article, style magazineStyle) (article, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and body. Rewrite this imported web article into print-ready magazine copy matching the style. Keep facts and names, remove web/navigation language, remove links, embeds, YouTube mentions, newsletter prompts and SEO clutter. Title should fit the publication voice. Body should be 900-1600 characters, in coherent paragraphs, ready for page layout.\n\nSTYLE: %s\n\nSOURCE TITLE: %s\nSOURCE BODY: %s", styleLine(style, "article"), a.Title, compact(a.Body, 3200))
	text, err := s.runTextgen(ctx, prompt, 1000)
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
	a.Enhanced = true
	return a, nil
}

func (s *server) rewriteFeatureForStyle(ctx context.Context, a article, style magazineStyle) (article, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and body. Turn this requested magazine feature page into a precise image-generation brief matching the publication style. The feature can be a crossword, comments page, quiz, TV/program listings, puzzle page, letters, classifieds, calendar, chart, or any other non-article department. Preserve the user's intent, but make it print-ready and specific about sections/modules. Body should be 700-1400 characters and describe what the page should contain.\n\nSTYLE: %s\n\nFEATURE TITLE: %s\nFEATURE REQUEST: %s", styleLine(style, "filler"), a.Title, compact(a.Body, 2400))
	text, err := s.runTextgen(ctx, prompt, 900)
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

func (s *server) generateArticles(ctx context.Context, title string, style magazineStyle, count int) ([]article, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with key articles, an array of exactly %d objects. Each object must have title and body. Generate original fictional-but-plausible magazine articles that fit this publication. Do not use real copyrighted brands unless generic/current facts are unavoidable. Vary article types: one feature, one short news item, one practical/service piece, one opinion/interview/list if count allows. Body length 900-1500 characters each, ready for print layout.\n\nPUBLICATION: %s\nSTYLE: %s", count, emptyDefault(title, "Untitled Magazine"), styleLine(style, "article"))
	text, err := s.runTextgen(ctx, prompt, 2200)
	if err != nil {
		return nil, err
	}
	var out struct {
		Articles []article `json:"articles"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &out); err != nil {
		return nil, err
	}
	cleaned := cleanArticles(out.Articles)
	for i := range cleaned {
		cleaned[i].Enhanced = true
	}
	if len(cleaned) > count {
		cleaned = cleaned[:count]
	}
	return cleaned, nil
}

func (s *server) runTextgen(ctx context.Context, prompt string, maxTokens int) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, s.cfg.TextgenTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, s.cfg.TextgenCmd, s.cfg.TextgenModel, "-max-tokens", strconv.Itoa(maxTokens), prompt)
	cmd.Env = commandEnv(ctx)
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

func (s *server) runImagegenWithRetry(ctx context.Context, workspace string, pageNumber int, prompt string, images []string, allowPromptTweak bool) (generatedImage, error) {
	var lastErr error
	currentPrompt := prompt
	for attempt := 0; attempt <= s.cfg.ImagegenRetries; attempt++ {
		if attempt > 0 {
			s.workspaceLog(workspace, "imagegen-retry: page=%d attempt=%d/%d", pageNumber, attempt, s.cfg.ImagegenRetries)
		}
		image, err := s.runImagegen(ctx, workspace, pageNumber, currentPrompt, images)
		if err == nil {
			return image, nil
		}
		lastErr = err
		s.workspaceLog(workspace, "imagegen-retry: page=%d attempt=%d failed: %v", pageNumber, attempt+1, err)
		if attempt == 0 && allowPromptTweak {
			tweaked, tweakErr := s.tweakTemplatePrompt(ctx, currentPrompt, err)
			if tweakErr != nil {
				s.workspaceLog(workspace, "imagegen-retry: page=%d prompt tweak failed: %v", pageNumber, tweakErr)
				currentPrompt = safeTemplatePrompt()
			} else {
				currentPrompt = limitPrompt(tweaked, s.cfg.ImagegenMaxPromptChars)
			}
			s.workspaceLog(workspace, "imagegen-retry: page=%d using safer template prompt chars=%d", pageNumber, len([]rune(currentPrompt)))
		}
		if attempt < s.cfg.ImagegenRetries {
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

func (s *server) tweakTemplatePrompt(ctx context.Context, prompt string, failure error) (string, error) {
	req := fmt.Sprintf("Rewrite this image-generation prompt to be safer and simpler. Keep the same goal: a blank printed magazine content-page template. Remove anything that could be interpreted as forbidden, confusing, label-heavy, or too complex. No markdown, return only the revised prompt under 1800 characters.\n\nFailure: %v\n\nPrompt:\n%s", failure, prompt)
	return s.runTextgen(ctx, req, 700)
}

func safeTemplatePrompt() string {
	return "Create one completely text-free blank magazine layout page. Portrait page, aspect ratio 1240:1754, full page visible, no crop. Use only generic visual shapes: margins, a blank top band, a blank bottom band, a two-column grid, pale empty image rectangles, soft grey text-block shapes without letters, and subtle paper texture. No readable text, no letters, no numbers, no masthead, no captions, no labels, no arrows, no diagrams, no UI, no technical print marks. Calm editorial layout reference only."
}

func defaultTemplatePrompt(style magazineStyle) string {
	return fmt.Sprintf("Create one completely text-free magazine page layout template.\n%s\nSTYLE: %s\nTEMPLATE: %s\nShow only generic layout geometry: paper texture, margins, column rhythm, blank header band, blank footer band, empty image rectangles, pale rule lines and subtle placeholder blocks. Absolutely no readable text, no letters, no numbers, no masthead, no captions, no labels, no arrows, no registration marks, no printing press technical marks, no UI and no moodboard. It should be a calm blank layout reference for later pages.", pageFormatInstruction(), styleLine(style, "template"), style.Template)
}

func (s *server) runImagegen(ctx context.Context, workspace string, pageNumber int, prompt string, images []string) (generatedImage, error) {
	cctx, cancel := context.WithTimeout(ctx, s.cfg.ImagegenTimeout)
	defer cancel()
	filename := fmt.Sprintf("page-%02d-%d.jpg", pageNumber, time.Now().UnixNano())
	out := filepath.Join(s.workspaceDir(workspace), "renders", filename)
	args := []string{
		s.cfg.ImagegenModel,
		"-format", s.cfg.ImagegenFormat,
		"-size", s.cfg.ImagegenSize,
		"-quality", s.cfg.ImagegenQuality,
		"-background", s.cfg.ImagegenBackground,
		"-output", out,
	}
	for _, img := range limitStrings(uniqueStrings(images), 16) {
		ref := imagegenImageRef(img)
		if ref != "" {
			args = append(args, "-image", ref)
		}
	}
	s.workspaceLog(workspace, "imagegen: page=%d prompt_chars=%d input_refs=%d accepted_refs=%d", pageNumber, len([]rune(prompt)), len(images), (len(args)-10)/2)
	args = append(args, limitPrompt(prompt, s.cfg.ImagegenMaxPromptChars))
	cmd := exec.CommandContext(cctx, s.cfg.ImagegenCmd, args...)
	cmd.Env = commandEnv(ctx)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return generatedImage{}, fmt.Errorf("imagegen timed out after %s: %w", s.cfg.ImagegenTimeout, err)
		}
		if stderr.Len() > 0 {
			s.workspaceLog(workspace, "imagegen: page=%d stderr=%s", pageNumber, strings.TrimSpace(stderr.String()))
			return generatedImage{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return generatedImage{}, err
	}
	if outText := strings.TrimSpace(stdout.String()); outText != "" {
		s.workspaceLog(workspace, "imagegen: page=%d stdout=%s", pageNumber, outText)
	}
	if _, err := os.Stat(out); err != nil {
		return generatedImage{}, fmt.Errorf("expected imagegen to create %s: %w", out, err)
	}
	publicURL := parseImageURL(stdout.String())
	if publicURL == "" {
		s.workspaceLog(workspace, "imagegen: page=%d warning=no public Image URL in stdout", pageNumber)
	}
	return generatedImage{Image: s.workspaceURL(workspace, "renders/"+filename), PublicURL: publicURL}, nil
}

func imagegenImageRef(imageRef string) string {
	imageRef = strings.TrimSpace(imageRef)
	if strings.HasPrefix(imageRef, "http://") || strings.HasPrefix(imageRef, "https://") {
		return imageRef
	}
	return ""
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
		a := article{Title: cleanText(item.Title), Body: cleanText(stripArticleHTML(body)), Source: strings.TrimSpace(item.Link), Images: extractImageURLs(body, item.Link)}
		a = enrichArticleFromURL(ctx, a)
		articles = append(articles, a)
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
		a := article{Title: cleanText(entry.Title), Body: cleanText(stripArticleHTML(body)), Source: link, Images: extractImageURLs(body, link)}
		a = enrichArticleFromURL(ctx, a)
		articles = append(articles, a)
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
	modules := newModulePlanner(kit)
	pages = append(pages, pagePlan{Number: 1, Kind: "cover", Title: "Cover", Prompt: coverPrompt(title, magType, style, req.Articles)})
	itemIndex := 0
	itemPart := 0
	for n := 2; n <= req.PageCount; n++ {
		if n == req.PageCount {
			pages = append(pages, pagePlan{Number: n, Kind: "back-page", Title: "Back page", Prompt: genericPrompt(n, title, style, modules.next("back", 4, n), "back", "Create a strong back page: advert, subscription panel, teaser, index, or closing visual depending on the publication style.")})
			continue
		}
		if isAdvertPage(n, req.PageCount) {
			pages = append(pages, pagePlan{Number: n, Kind: "advert", Title: "Advert", Prompt: genericPrompt(n, title, style, modules.next("advert", 4, n), "advert", "Create a full-page fictional advert that belongs naturally in this publication. Use no real brands unless supplied by the article content.")})
			continue
		}
		if itemIndex < len(req.Articles) {
			a := req.Articles[itemIndex]
			totalParts := normalizedArticlePages(a)
			itemPart++
			kind := strings.TrimSpace(a.Kind)
			if kind == "" {
				kind = "article"
			}
			if kind != "feature" && len([]rune(a.Body)) > 1800 {
				kind = "feature"
			}
			pages = append(pages, pagePlan{Number: n, Kind: kind, Title: a.Title, Article: &a, Images: a.Images, Prompt: articlePrompt(n, title, style, modules.next(kind, 3, n), kind, a, itemPart, totalParts)})
			if itemPart >= totalParts {
				itemIndex++
				itemPart = 0
			}
			continue
		}
		pages = append(pages, pagePlan{Number: n, Kind: "filler", Title: "Departments", Prompt: genericPrompt(n, title, style, modules.next("filler", 4, n), "filler", "Create a department/filler page with short recurring modules, briefs, reader notes, charts, sidebars, small adverts, captions, and visual rhythm suited to the publication.")})
	}
	return pages
}

func coverPrompt(title, magType string, style magazineStyle, articles []article) string {
	return limitPrompt(fmt.Sprintf("Create the cover of %q, a %s.\n%s\nSTYLE: %s\nCOVER STYLE: %s\nUse cover lines for: %s\nInclude masthead, issue date, price/barcode or equivalent furniture, strong hierarchy, and avoid: %s.", title, magType, pageFormatInstruction(), styleLine(style, "core"), style.Cover, articleTitles(articles, 6), style.Avoid), 3900)
}

func articlePrompt(n int, title string, style magazineStyle, modules, kind string, a article, part, totalParts int) string {
	taskStyle := styleLine(style, kind)
	partText := ""
	if totalParts > 1 {
		partText = fmt.Sprintf("\nSERIES: This is page %d of %d for this item. Continue the same story/feature without repeating the same layout.", part, totalParts)
	}
	return limitPrompt(fmt.Sprintf("Create a %s page for %q.\n%s\nSTYLE: %s\nPAGE STYLE: %s%s\nMODULE IDEAS: %s\nTITLE: %s\nBRIEF/BODY: %s\nLayout with headline, deck, byline/source if available, columns, image slots, captions, pull quote/sidebar where useful.", kind, title, pageFormatInstruction(), styleLine(style, "content"), taskStyle, partText, modules, a.Title, compact(a.Body, 1900)), 3900)
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

func genericPrompt(n int, title string, style magazineStyle, modules, kind, task string) string {
	return limitPrompt(fmt.Sprintf("Create a %s page for %q.\n%s\nSTYLE: %s\nPAGE STYLE: %s\nIDEAS: %s\nTASK: %s\nKeep header/footer treatment consistent with the issue.", kind, title, pageFormatInstruction(), styleLine(style, "content"), styleLine(style, kind), modules, task), 3900)
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
		Departments: []string{"editor's note", "short briefs", "reader mail", "local listings", "numbers panel", "what's next", "staff picks", "calendar strip", "reader poll", "corrections box", "market notes", "archive corner", "field report", "mini interview", "glossary block", "resource list", "event diary", "trend meter"},
		Adverts:     []string{"small classified ad", "fictional supplier advert", "subscription offer", "event notice", "service directory", "training course ad", "local shop panel", "conference notice", "mail-order coupon", "patron thank-you"},
		Sidebars:    []string{"key facts", "timeline", "quote box", "how it works", "recommended next read", "source notes", "before and after", "checklist", "map inset", "numbers to know", "pros and cons", "mini profile", "method box", "field notes", "reader tip", "myth versus fact", "toolbox", "quick glossary"},
		Captions:    []string{"dry editorial caption", "technical caption", "behind-the-scenes note", "short contextual label", "archive caption", "process caption", "comparison note", "source credit line", "location note", "object label", "timeline caption", "quote attribution", "data note", "materials caption", "scene setter", "detail callout", "before-note", "after-note"},
		BackPage:    []string{"subscription panel", "single bold advert", "teaser for next issue", "index and closing note", "reader challenge", "classified strip", "next-month calendar", "sponsor panel", "credits block", "closing image caption"},
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
			"article": uniqueStrings(append(append([]string{}, kit.Sidebars...), kit.Captions...)),
			"feature": uniqueStrings(append(append([]string{}, kit.Sidebars...), append(kit.Captions, kit.Departments...)...)),
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

var tagRE = regexp.MustCompile(`<[^>]+>`)
var spaceRE = regexp.MustCompile(`\s+`)
var imageRE = regexp.MustCompile(`(?is)<img[^>]+(?:src|data-src|data-original)=["']([^"']+)["']`)
var srcsetRE = regexp.MustCompile(`(?is)<img[^>]+srcset=["']([^"']+)["']`)
var removableBlockRE = regexp.MustCompile(`(?is)<(?:script|style|noscript|svg|iframe|object|embed|form|nav|footer|aside)[^>]*>.*?</(?:script|style|noscript|svg|iframe|object|embed|form|nav|footer|aside)>`)
var commentRE = regexp.MustCompile(`(?is)<!--.*?-->`)
var linkRE = regexp.MustCompile(`(?is)<a\b[^>]*>(.*?)</a>`)
var urlTextRE = regexp.MustCompile(`https?://\S+`)
var likelyArticleRE = regexp.MustCompile(`(?is)<(?:article|main)\b[^>]*>(.*?)</(?:article|main)>`)
var contentBlockRE = regexp.MustCompile(`(?is)<(?:div|section)\b[^>]*(?:class|id)=["'][^"']*(?:article|post|entry|content|story|body|main)[^"']*["'][^>]*>(.*?)</(?:div|section)>`)
var paragraphRE = regexp.MustCompile(`(?is)<p\b[^>]*>.*?</p>`)
var titleRE = regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</h1>`)

func stripHTML(s string) string { return tagRE.ReplaceAllString(s, " ") }
func cleanText(s string) string {
	return strings.TrimSpace(spaceRE.ReplaceAllString(html.UnescapeString(s), " "))
}

func stripArticleHTML(s string) string {
	s = cleanArticleHTML(s)
	s = linkRE.ReplaceAllString(s, "$1")
	s = stripHTML(s)
	s = urlTextRE.ReplaceAllString(s, "")
	return cleanText(s)
}

func cleanArticleHTML(s string) string {
	s = commentRE.ReplaceAllString(s, " ")
	s = removableBlockRE.ReplaceAllString(s, " ")
	return s
}

func extractImageURLs(markup, base string) []string {
	markup = cleanArticleHTML(markup)
	matches := imageRE.FindAllStringSubmatch(markup, -1)
	seen := map[string]bool{}
	out := []string{}
	for _, m := range matches {
		u := resolveURL(base, strings.TrimSpace(m[1]))
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	for _, m := range srcsetRE.FindAllStringSubmatch(markup, -1) {
		u := resolveURL(base, firstSrcsetURL(m[1]))
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

func enrichArticleFromURL(ctx context.Context, a article) article {
	if strings.TrimSpace(a.Source) == "" {
		return a
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Source, nil)
	if err != nil {
		return a
	}
	req.Header.Set("User-Agent", "magazine-builder/0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return a
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return a
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return a
	}
	htmlText := string(data)
	extracted := extractLikelyArticle(htmlText)
	if extracted.Title != "" {
		a.Title = extracted.Title
	}
	if len([]rune(extracted.Body)) > len([]rune(a.Body)) {
		a.Body = extracted.Body
	}
	a.Images = uniqueStrings(append(extractImageURLs(extracted.Markup, a.Source), a.Images...))
	return a
}

type extractedArticle struct {
	Title  string
	Body   string
	Markup string
}

func extractLikelyArticle(pageHTML string) extractedArticle {
	pageHTML = cleanArticleHTML(pageHTML)
	title := ""
	if m := titleRE.FindStringSubmatch(pageHTML); len(m) > 1 {
		title = cleanText(stripHTML(m[1]))
	}
	best := ""
	for _, m := range likelyArticleRE.FindAllStringSubmatch(pageHTML, -1) {
		if scoreArticleMarkup(m[1]) > scoreArticleMarkup(best) {
			best = m[1]
		}
	}
	for _, m := range contentBlockRE.FindAllStringSubmatch(pageHTML, -1) {
		block := m[1]
		if scoreArticleMarkup(block) > scoreArticleMarkup(best) {
			best = block
		}
	}
	if best == "" {
		paras := paragraphRE.FindAllString(pageHTML, -1)
		best = strings.Join(paras, "\n")
	}
	return extractedArticle{Title: title, Body: stripArticleHTML(best), Markup: best}
}

func scoreArticleMarkup(markup string) int {
	text := stripArticleHTML(markup)
	return len([]rune(text)) + strings.Count(strings.ToLower(markup), "<p")*80 - strings.Count(strings.ToLower(markup), "<li")*20
}

func resolveURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "data:") {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		return u.String()
	}
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Scheme == "" {
		return ref
	}
	return baseURL.ResolveReference(u).String()
}

func firstSrcsetURL(srcset string) string {
	parts := strings.Split(srcset, ",")
	if len(parts) == 0 {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(parts[0]))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
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
		id := strings.ToLower(s)
		if s == "" || seen[id] {
			continue
		}
		seen[id] = true
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

func contextWithAPIKey(ctx context.Context, apiKey string) context.Context {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ctx
	}
	return context.WithValue(ctx, apiKeyContextKey{}, apiKey)
}

func commandEnv(ctx context.Context) []string {
	env := os.Environ()
	apiKey, _ := ctx.Value(apiKeyContextKey{}).(string)
	if strings.TrimSpace(apiKey) != "" {
		env = append(env, "DEFAPI_API_KEY="+strings.TrimSpace(apiKey))
	}
	return env
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
		data, cfg, err := jpegData(imgPath)
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

func jpegData(path string) ([]byte, image.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, image.Config{}, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, image.Config{}, err
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 92}); err != nil {
		return nil, image.Config{}, err
	}
	bounds := img.Bounds()
	cfg := image.Config{Width: bounds.Dx(), Height: bounds.Dy(), ColorModel: img.ColorModel()}
	return b.Bytes(), cfg, nil
}

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Magazine Builder</title>
<style>
:root{color-scheme:light;--ink:#172026;--muted:#667680;--line:#d9e0e5;--panel:#f6f8fa;--accent:#b3261e;--blue:#235789;--paper:#fffdf8;--soft:#edf2f5}*{box-sizing:border-box}body{margin:0;font:15px/1.45 system-ui,-apple-system,Segoe UI,sans-serif;color:var(--ink);background:linear-gradient(#eef3f6,#e4e9ed)}button,input,select,textarea{font:inherit}button{border:0;border-radius:6px;background:var(--ink);color:white;padding:10px 14px;font-weight:700;cursor:pointer}button.secondary{background:var(--blue)}button.ghost{background:white;color:var(--ink);border:1px solid var(--line)}button:disabled{opacity:.55;cursor:not-allowed}.spinner{display:inline-block;width:13px;height:13px;border:2px solid rgba(255,255,255,.45);border-top-color:#fff;border-radius:50%;vertical-align:-2px;margin-right:6px;animation:spin .8s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}.shell{max-width:1320px;margin:0 auto;padding:28px 22px 44px}.hero{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:18px}.brand h1{font-size:34px;line-height:1.05;margin:0 0 7px}.brand p{margin:0;color:var(--muted);max-width:650px}.workspace{font:12px/1.3 ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--muted);background:white;border:1px solid var(--line);border-radius:6px;padding:8px 10px}.key-gate{position:fixed;inset:0;z-index:20;background:rgba(232,238,242,.96);display:flex;align-items:center;justify-content:center;padding:20px}.key-gate.hidden{display:none}.key-card{width:min(520px,100%);background:var(--paper);border:1px solid var(--line);border-radius:12px;box-shadow:0 24px 70px rgba(26,38,48,.18);padding:22px}.key-card h2{margin:0 0 8px;font-size:24px}.key-card p{color:var(--muted);margin:0 0 16px}.key-actions{display:flex;gap:10px;margin-top:14px}.key-actions button{flex:1}.steps{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;margin:18px 0}.step-pill{border:1px solid var(--line);border-radius:8px;padding:11px 12px;background:white;color:var(--muted);font-weight:700}.step-pill.active{background:var(--ink);border-color:var(--ink);color:white}.wizard{background:var(--paper);border:1px solid var(--line);border-radius:10px;box-shadow:0 18px 45px rgba(26,38,48,.08);overflow:hidden}.wizard-step{display:none;padding:24px}.wizard-step.active{display:block}.step-head{display:flex;justify-content:space-between;gap:20px;align-items:flex-start;margin-bottom:18px}.step-head h2{font-size:22px;margin:0 0 4px}.step-head p{margin:0;color:var(--muted);max-width:720px}.form-grid{display:grid;grid-template-columns:minmax(0,1fr) 260px;gap:16px}.field{margin-bottom:14px}label{display:block;font-weight:700;margin:0 0 6px}input,select,textarea{width:100%;border:1px solid var(--line);border-radius:7px;background:white;padding:10px 11px;color:var(--ink)}textarea{min-height:132px;resize:vertical}.style-box textarea{min-height:230px}.row{display:flex;gap:10px;align-items:center}.row>*{flex:1}.step-actions{display:flex;gap:10px;justify-content:flex-end;margin-top:18px;padding-top:16px;border-top:1px solid var(--line)}.status{color:var(--muted);font-size:13px;margin:8px 0 0;min-height:18px}.muted{color:var(--muted);font-size:13px}.article-tools{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:10px;align-items:end}.article-list{display:grid;grid-template-columns:repeat(auto-fit,minmax(320px,1fr));gap:12px;margin-top:14px}.article{border:1px solid var(--line);background:white;border-radius:8px;padding:12px}.article textarea{min-height:120px}.hidden{display:none!important}.toolbar{display:flex;gap:8px;margin:18px 0 10px;flex-wrap:wrap}.toolbar button{flex:none}.results{margin-top:18px}.grid{display:grid;gap:14px}.top-pair,.spread{display:grid;grid-template-columns:repeat(2,minmax(290px,1fr));gap:12px;align-items:start}.spread{border-top:1px solid var(--line);padding-top:14px}.progress{height:10px;background:#dfe6eb;border-radius:999px;overflow:hidden;margin:10px 0}.progress-bar{height:100%;width:0;background:var(--accent);transition:width .25s ease}.progress-row{margin:10px 0 14px}.page{background:white;border:1px solid var(--line);border-radius:8px;padding:12px;min-height:220px}.page[draggable=true]{cursor:grab}.page.dragging{opacity:.45}.fixed{opacity:.72}.kind{font-size:12px;color:white;background:var(--accent);display:inline-block;border-radius:999px;padding:2px 8px;text-transform:uppercase}.page h3{margin:8px 0;font-size:18px}.prompt{width:100%;min-height:180px;color:#26343c;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;background:#f8fafb;border:1px solid var(--line);border-radius:6px;padding:8px;resize:vertical}.template-preview{display:grid;grid-template-columns:minmax(220px,320px) 1fr;gap:14px;align-items:start;background:white;border:1px solid var(--line);border-radius:8px;padding:12px;margin:12px 0}.template-preview img{width:100%;aspect-ratio:1240/1754;object-fit:cover;border:1px solid var(--line);border-radius:6px}.template-actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:10px}.kit{white-space:pre-wrap;background:white;border:1px solid var(--line);border-radius:8px;padding:14px;margin-bottom:12px}.preview{width:100%;aspect-ratio:1240/1754;object-fit:cover;border:1px solid var(--line);border-radius:6px;background:#f5f7f8;margin:8px 0}.page-status{font-size:12px;color:var(--muted);margin:4px 0 8px}@media(max-width:760px){.shell{padding:18px 12px 32px}.hero{display:block}.brand h1{font-size:28px}.steps{grid-template-columns:1fr 1fr}.form-grid,.article-tools,.top-pair,.spread{grid-template-columns:1fr}.wizard-step{padding:16px}.step-head{display:block}.step-actions{justify-content:stretch}.step-actions button{flex:1}}
</style>
</head>
<body>
<div id="keyGate" class="key-gate hidden"><div class="key-card"><h2>DEFAPI key required</h2><p>Your key is stored only in this browser via localStorage and sent to this local server for textgen/imagegen calls.</p><label>DEFAPI API key</label><input id="apiKeyInput" type="password" autocomplete="off" placeholder="defapi..."><div class="key-actions"><button id="saveApiKey" class="secondary">Save and Start</button></div><div id="apiKeyStatus" class="status"></div></div></div>
<div class="shell">
<header class="hero">
  <div class="brand">
    <h1>Magazine Builder</h1>
    <p>Create a publication style, gather articles, generate a page plan, reorder the issue, then render images and a PDF.</p>
  </div>
  <div id="workspaceLabel" class="workspace">No workspace yet</div><button id="changeApiKey" class="ghost">API key</button>
</header>
<nav class="steps" aria-label="Wizard steps">
  <div class="step-pill active" data-step-label="1">1. Style</div>
  <div class="step-pill" data-step-label="2">2. Articles</div>
  <div class="step-pill" data-step-label="3">3. Plan</div>
  <div class="step-pill" data-step-label="4">4. PDF</div>
</nav>
<section class="wizard">
  <div id="wizardStyle" class="wizard-step active">
    <div class="step-head"><div><h2>Define the publication</h2><p>Name the issue and describe the visual/editorial direction. Textgen will turn this into compact style JSON for the image prompts.</p></div></div>
    <div class="form-grid">
      <div class="style-box">
        <div class="field"><label>Publication title</label><input id="title" value="New Magazine"></div>
        <div class="field"><label>Style idea</label><textarea id="style" placeholder="e.g. Nordic architecture quarterly, restrained, tactile paper, black and red accents"></textarea></div>
      </div>
      <div>
        <div class="field"><label>Pages</label><select id="pageCount"><option>4</option><option>8</option><option selected>12</option><option>16</option><option>24</option><option>32</option><option>48</option><option>64</option></select></div>
        <div class="field"><label>Reference image</label><input id="reference" type="file" accept="image/*"></div>
        <div class="row"><button class="ghost" id="clear">Clear</button></div><div id="styleStatus" class="status">Style will be enhanced when you continue.</div>
      </div>
    </div>
    <div class="step-actions"><button id="toArticles">Next: Articles</button></div>
  </div>
  <div id="wizardArticles" class="wizard-step">
    <div class="step-head"><div><h2>Add article material</h2><p>Import a feed or write articles manually. Imported and manual articles are rewritten into the publication voice before planning.</p></div></div>
    <div class="article-tools"><div><label>RSS or Atom feed URL</label><input id="rss" placeholder="https://example.com/feed.xml"><div id="rssStatus" class="status"></div></div><button id="importRSS">Import latest</button></div>
    <div class="toolbar"><button id="generateArticles" class="secondary">Generate Articles</button><button id="addArticle" class="ghost">Add Article</button><button id="addFeature" class="ghost">Add Feature Page</button></div><div id="generateStatus" class="status"></div>
    <div id="articles" class="article-list"></div>
    <p class="muted">Manual article text can be rough. It is cleaned and rewritten with textgen when the plan is generated.</p>
    <div class="step-actions"><button class="ghost" id="backStyle">Back</button><button id="toPlan">Generate Plan</button></div>
  </div>
  <div id="wizardPlan" class="wizard-step">
    <div class="step-head"><div><h2>Review and reorder</h2><p>Drag middle pages to change the running order. The cover and back page stay fixed.</p></div></div>
    <div id="planToolbar" class="toolbar hidden"><button id="build">Regenerate Plan</button><button id="download" class="ghost">Download JSON</button></div><section id="output" class="results"><div class="kit">Generate a plan to review pages here.</div></section><div class="step-actions"><button class="ghost" id="backArticles">Back</button><button id="toPdf">Next: Render PDF</button></div>
  </div>
  <div id="wizardPdf" class="wizard-step">
    <div class="step-head"><div><h2>Render the issue</h2><p>The app renders the cover, creates a shared content template image, renders the pages, then writes the PDF.</p></div></div>
    <div id="renderToolbar" class="toolbar hidden"><button id="render" class="secondary">Download PDF</button><button id="downloadPdfJson" class="ghost">Download JSON</button></div><div id="renderStatus" class="status"></div><div id="templateReview"></div><section id="renderOutput" class="results"></section><div class="step-actions"><button class="ghost" id="backPlan">Back</button><button id="renderSide" class="secondary">Download PDF</button></div>
  </div>
</section>

</div>
<script>
let articles=[];let lastPlan=null;let referencePath='';let workspace='';let apiKey=localStorage.getItem('defapiApiKey')||'';let renderedImages={};let renderedTemplate=null;let templatePrompt='';let draggedIndex=null;let currentStep=1;let busyCount=0;let isRendering=false;
const $=id=>document.getElementById(id);
function esc(s){return String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function lockButtons(on){document.body.classList.toggle('busy',on);document.querySelectorAll('button').forEach(b=>{if(on){b.dataset.wasDisabled=b.disabled?'1':'0';b.disabled=true}else{b.disabled=b.dataset.wasDisabled==='1';delete b.dataset.wasDisabled}})}
async function withBusy(button,msg,fn){if(busyCount>0)return;busyCount++;const old=button?button.innerHTML:'';lockButtons(true);if(button)button.innerHTML='<span class="spinner"></span>'+esc(msg||'Working...');try{return await fn()}finally{if(button)button.innerHTML=old;busyCount--;lockButtons(false)}}
function requireApiKey(){if(!apiKey){$('keyGate').classList.remove('hidden');return false}$('keyGate').classList.add('hidden');return true}
function saveApiKey(){const v=$('apiKeyInput').value.trim();if(!v){$('apiKeyStatus').textContent='Enter an API key.';return}apiKey=v;localStorage.setItem('defapiApiKey',apiKey);$('apiKeyInput').value='';$('keyGate').classList.add('hidden')}
$('saveApiKey').onclick=saveApiKey;$('changeApiKey').onclick=()=>{$('apiKeyInput').value=apiKey;$('keyGate').classList.remove('hidden')};
function updateWorkspaceLabel(){const el=$('workspaceLabel');if(el)el.innerHTML=workspace?'Workspace: '+esc(workspace)+' · <a href="/work/'+esc(workspace)+'/magazine.log" target="_blank">log</a>':'No workspace yet'}
function showStep(n){updateWorkspaceLabel();currentStep=n;const pt=$('planToolbar');if(pt)pt.classList.toggle('hidden',n!==3||!lastPlan);const rt=$('renderToolbar');if(rt)rt.classList.toggle('hidden',n!==4||!lastPlan);document.querySelectorAll('.step-pill').forEach(el=>el.classList.toggle('active',+el.dataset.stepLabel===n));[['wizardStyle',1],['wizardArticles',2],['wizardPlan',3],['wizardPdf',4]].forEach(([id,step])=>$(id).classList.toggle('active',step===n));if(n>=3&&lastPlan)renderPlan(lastPlan)}
async function ensureStyle(){if(!requireApiKey())return false;if($('style').value.trim().startsWith('{'))return true;$('styleStatus').textContent='Enhancing style JSON...';return await enhanceStyle()}
async function enhanceStyle(){const fd=new FormData();fd.append('apiKey',apiKey);fd.append('title',$('title').value);fd.append('style',$('style').value);fd.append('workspace',workspace);if($('reference').files[0])fd.append('reference',$('reference').files[0]);const res=await fetch('/api/enhance-style',{method:'POST',body:fd});const data=await res.json();if(!res.ok){$('styleStatus').textContent=data.error||'Failed';return false}$('style').value=data.enhancedStyle;referencePath=data.referencePath||referencePath;workspace=data.workspace||workspace;updateWorkspaceLabel();$('styleStatus').textContent=referencePath?'Enhanced with reference image.':'Enhanced.';return true}
function renderArticles(){
  const wrap=$('articles');wrap.innerHTML='';
  articles.forEach((a,i)=>{const div=document.createElement('div');div.className='article';const kind=a.kind||'article';div.innerHTML='<div class="row"><div><label>Type</label><select data-i="'+i+'" data-k="kind"><option value="article" '+(kind==='article'?'selected':'')+'>Article</option><option value="feature" '+(kind==='feature'?'selected':'')+'>Feature page</option></select></div><div><label>Pages</label><input type="number" min="1" max="8" value="'+esc(a.pages||1)+'" data-i="'+i+'" data-k="pages"></div></div><label>Title</label><input value="'+esc(a.title||'')+'" data-i="'+i+'" data-k="title"><label>'+(kind==='feature'?'Feature description':'Body')+'</label><textarea data-i="'+i+'" data-k="body">'+esc(a.body||'')+'</textarea><label>Image URLs, one per line</label><textarea data-i="'+i+'" data-k="images">'+esc((a.images||[]).join('\n'))+'</textarea><button class="ghost" data-remove="'+i+'">Remove</button>';wrap.appendChild(div)});
  wrap.querySelectorAll('input,textarea,select').forEach(el=>{const update=e=>{const i=+e.target.dataset.i,k=e.target.dataset.k;articles[i][k]=k==='images'?e.target.value.split('\n').map(s=>s.trim()).filter(Boolean):(k==='pages'?Math.max(1,Math.min(8,parseInt(e.target.value||'1',10))):e.target.value);articles[i].enhanced=false;if(k==='kind')renderArticles()};el.oninput=update;el.onchange=update});
  wrap.querySelectorAll('[data-remove]').forEach(el=>el.onclick=e=>{if(isRendering)return;articles.splice(+e.target.dataset.remove,1);renderArticles()})
}
$('toArticles').onclick=e=>withBusy(e.currentTarget,'Preparing...',async()=>{if(await ensureStyle())showStep(2)});
$('backStyle').onclick=()=>{if(!isRendering)showStep(1)};
$('backArticles').onclick=()=>{if(!isRendering)showStep(2)};
$('backPlan').onclick=()=>{if(!isRendering)showStep(3)};
$('toPlan').onclick=e=>withBusy(e.currentTarget,'Generating...',async()=>{await buildPlan();if(lastPlan)showStep(3)});
$('toPdf').onclick=e=>withBusy(e.currentTarget,'Preparing...',()=>startRenderFlow());
$('addArticle').onclick=()=>{if(isRendering)return;articles.push({kind:'article',title:'',body:'',images:[],pages:1,enhanced:false});renderArticles()};
$('addFeature').onclick=()=>{if(isRendering)return;articles.push({kind:'feature',title:'',body:'Describe this feature page: comments, crossword, quiz, listings, letters, classifieds, TV program, calendar, chart, etc.',images:[],pages:1,enhanced:false});renderArticles()};
$('clear').onclick=()=>{if(isRendering)return;$('style').value='';$('reference').value='';referencePath='';workspace='';$('styleStatus').textContent=''};
$('generateArticles').onclick=e=>withBusy(e.currentTarget,'Generating...',async()=>{if(!await ensureStyle())return;$('generateStatus').textContent='Writing articles for this publication...';const res=await fetch('/api/generate-articles',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({title:$('title').value,style:$('style').value,count:4,workspace,apiKey})});const data=await res.json();if(!res.ok){$('generateStatus').textContent=data.error||'Failed';return}workspace=data.workspace||workspace;updateWorkspaceLabel();articles=articles.concat((data.articles||[]).map(a=>Object.assign({kind:'article',pages:1},a)));renderArticles();$('generateStatus').textContent='Generated '+(data.articles||[]).length+' articles.'});
$('importRSS').onclick=e=>withBusy(e.currentTarget,'Importing...',async()=>{if(!await ensureStyle())return;$('rssStatus').textContent='Fetching articles, extracting pages and rewriting...';const res=await fetch('/api/import-rss',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:$('rss').value,limit:10,style:$('style').value,workspace,apiKey})});const data=await res.json();if(!res.ok){$('rssStatus').textContent=data.error||'Failed';return}workspace=data.workspace||workspace;updateWorkspaceLabel();articles=articles.concat((data.articles||[]).map(a=>Object.assign({kind:'article',pages:1},a)));renderArticles();$('rssStatus').textContent='Imported '+(data.articles||[]).length+' rewritten articles.'});
async function buildPlan(){if(!await ensureStyle())return;const payload={title:$('title').value,magazineType:'',style:$('style').value,pageCount:+$('pageCount').value,articles,workspace,apiKey};const out=$('output');out.innerHTML=progressHTML('Planning issue...',8,'planProgress');renderedImages={};setProgress('planProgress',22,'Enhancing articles...');const res=await fetch('/api/build',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});setProgress('planProgress',75,'Building page order...');const data=await res.json();if(!res.ok){out.innerHTML='<div class="kit">'+esc(data.error||'Failed')+'</div>';return}lastPlan=data;workspace=data.workspace||workspace;updateWorkspaceLabel();lastPlan.reference=referencePath;templatePrompt=templatePrompt||defaultTemplatePrompt(data.style||{});articles=uniquePlannedArticles(data.pages||[]);renderArticles();setProgress('planProgress',100,'Plan ready.');renderPlan(data)}
$('build').onclick=e=>withBusy(e.currentTarget,'Generating...',async()=>{await buildPlan();if(lastPlan)showStep(3)});
$('download').onclick=()=>{if(!lastPlan||isRendering)return;const blob=new Blob([JSON.stringify(lastPlan,null,2)],{type:'application/json'});const a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download='magazine-plan.json';a.click();URL.revokeObjectURL(a.href)};
$('render').onclick=e=>withBusy(e.currentTarget,'Preparing...',()=>startRenderFlow());$('renderSide').onclick=e=>withBusy(e.currentTarget,'Preparing...',()=>startRenderFlow());$('downloadPdfJson').onclick=()=>$('download').click();
function progressHTML(label,pct,id){id=id||'planProgress';return '<div class="progress-row"><div class="status" id="'+id+'Text">'+esc(label)+'</div><div class="progress"><div class="progress-bar" id="'+id+'" style="width:'+pct+'%"></div></div></div>'}
function setProgress(id,pct,label){const bar=$(id);if(bar)bar.style.width=Math.max(0,Math.min(100,pct))+'%';const text=$(id+'Text')||$(id==='renderProgress'?'renderProgressText':'planProgressText');if(text&&label)text.textContent=label}
function defaultTemplatePrompt(style){const styleText=style&&style.template?style.template:'blank content-page production dummy with header, folio, grid and image boxes';return 'Create one completely text-free magazine page layout template.\nFORMAT: Portrait magazine page, aspect ratio 1240:1754, full page visible edge to edge, no crop.\nTEMPLATE: '+styleText+'\nShow only generic layout geometry: paper texture, margins, column rhythm, blank header band, blank footer band, empty image rectangles, pale rule lines and subtle placeholder blocks. Absolutely no readable text, no letters, no numbers, no masthead, no captions, no labels, no arrows, no registration marks, no technical print marks, no UI and no moodboard.'}
function renderPlan(data){
  const kit=typeof data.creativeKit==='string'?data.creativeKit:JSON.stringify(data.creativeKit,null,2);
  const target=(currentStep===4&&$('renderOutput'))?$('renderOutput'):$('output');
  if(!target)return;
  target.innerHTML='<div class="kit"><strong>Style JSON</strong>\n'+esc(JSON.stringify(data.style||{},null,2))+'\n\n<strong>Creative kit</strong>\n'+esc(kit)+'</div><div id="pageGrid" class="grid">'+pageGridHTML(data.pages||[])+'</div>';
  wirePromptEditors();
  wireTemplateActions();
  wireDrag();
}
function pageGridHTML(pages){const cover=pages[0]?pageHTML(pages[0],0):'';let html='<div class="top-pair">'+cover+templateCardHTML()+'</div>';const rest=pages.slice(1);for(let i=0;i<rest.length;i+=2){html+='<div class="spread">'+pageHTML(rest[i],i+1)+(rest[i+1]?pageHTML(rest[i+1],i+2):'<article class="page fixed"><span class="kind">blank</span><h3>Inside cover</h3><div class="preview"></div></article>')+'</div>'}return html}
function pageHTML(p,i){const fixed=i===0||i===(lastPlan.pages.length-1);const item=renderedImages[p.number]||null;const img=item?item.image:'';const canDrag=!fixed&&!isRendering;return '<article class="page '+(fixed?'fixed':'')+'" data-i="'+i+'" draggable="'+canDrag+'"><span class="kind">'+esc(p.kind)+'</span><h3>'+p.number+'. '+esc(p.title)+'</h3><div class="page-status" id="status-'+p.number+'">'+(img?'Rendered':(fixed?'Fixed page':(isRendering?'Render locked':'Drag to reorder')))+'</div>'+(img?'<img class="preview" src="'+esc(img)+'">':'<div class="preview"></div>')+'<label>Image prompt</label><textarea class="prompt" data-prompt-i="'+i+'">'+esc(p.prompt)+'</textarea></article>'}
function templateCardHTML(){const ready=renderedTemplate&&renderedTemplate.publicUrl;const img=renderedTemplate&&renderedTemplate.image?'<img class="preview" src="'+esc(renderedTemplate.image)+'">':'<div class="preview"></div>';const status=ready?'Review before rendering content pages':'Template placeholder, edit prompt before rendering';return '<article class="page fixed"><span class="kind">template</span><h3>Template</h3><div class="page-status">'+status+'</div>'+img+'<label>Template prompt</label><textarea class="prompt" id="templatePrompt">'+esc(templatePrompt||defaultTemplatePrompt(lastPlan&&lastPlan.style||{}))+'</textarea><div class="template-actions"><button id="templateUse" class="secondary" '+(ready?'':'disabled')+'>✓ Use</button><button id="templateRefresh" class="ghost" '+(templatePrompt?'':'disabled')+'>↻ Refresh</button></div></article>'}
function wireTemplateActions(){const prompt=$('templatePrompt');if(prompt)prompt.oninput=e=>{templatePrompt=e.target.value};const use=$('templateUse');if(use)use.onclick=e=>{if(renderedTemplate&&renderedTemplate.publicUrl)withBusy(e.currentTarget,'Rendering...',()=>renderRemainingPages(renderedTemplate.publicUrl))};const refresh=$('templateRefresh');if(refresh)refresh.onclick=e=>{templatePrompt=$('templatePrompt')?$('templatePrompt').value:templatePrompt;withBusy(e.currentTarget,'Regenerating...',()=>renderTemplateForReview())}}
function wirePromptEditors(){document.querySelectorAll('[data-prompt-i]').forEach(el=>el.oninput=e=>{const i=+e.target.dataset.promptI;if(lastPlan&&lastPlan.pages[i])lastPlan.pages[i].prompt=e.target.value})}
function uniquePlannedArticles(pages){const seen=new Set();const out=[];(pages||[]).forEach(p=>{if(!p.article)return;const key=[p.article.kind||'article',p.article.title||'',p.article.body||'',p.article.source||''].join('\u001f');if(seen.has(key))return;seen.add(key);out.push(Object.assign({kind:'article',pages:1},p.article))});return out}
function wireDrag(){if(isRendering)return;document.querySelectorAll('.page[draggable=true]').forEach(card=>{card.ondragstart=e=>{draggedIndex=+card.dataset.i;card.classList.add('dragging')};card.ondragend=e=>card.classList.remove('dragging');card.ondragover=e=>e.preventDefault();card.ondrop=e=>{e.preventDefault();const target=+card.dataset.i;if(draggedIndex===null||target===0||target===lastPlan.pages.length-1||isRendering)return;movePage(draggedIndex,target)}})}
function movePage(from,to){const pages=lastPlan.pages;if(isRendering||from===0||from===pages.length-1||to===0||to===pages.length-1)return;const [p]=pages.splice(from,1);pages.splice(to,0,p);renumberPages();renderPlan(lastPlan)}
function renumberPages(){lastPlan.pages.forEach((p,i)=>{p.number=i+1})}
async function startRenderFlow(){if(!lastPlan){$('renderStatus').textContent='Generate a plan first.';return}isRendering=true;showStep(4);renderedImages={};renderedTemplate=null;$('templateReview').innerHTML='';$('renderStatus').innerHTML=progressHTML('Rendering cover...',5,'renderProgress');renderPlan(lastPlan);try{setProgress('renderProgress',10,'Rendering cover...');let cover=await renderPage(lastPlan.pages[0], '');renderedImages[1]=cover;setProgress('renderProgress',25,'Rendering template...');renderPlan(lastPlan);await renderTemplateForReview()}catch(e){$('renderStatus').textContent=e.message||'Render failed';isRendering=false;renderPlan(lastPlan)}}
async function renderTemplateForReview(){setProgress('renderProgress',35,'Rendering shared content template...');const template=await renderTemplate();renderedTemplate=template;const templateRef=template.publicUrl||'';if(!templateRef){$('renderStatus').textContent='Template rendered, but imagegen did not return a public Image URL. Check workspace log.';isRendering=false;renderPlan(lastPlan);return}setProgress('renderProgress',42,'Review the template card beside the cover.');$('templateReview').innerHTML='<p class="muted">Use ✓ or refresh ↻ on the template card.</p>';renderPlan(lastPlan)}
async function renderRemainingPages(templateRef){try{$('templateReview').innerHTML='';renderedTemplate=null;const middle=lastPlan.pages.slice(1);setProgress('renderProgress',45,'Rendering content pages...');for(let i=0;i<middle.length;i+=3){await Promise.all(middle.slice(i,i+3).map(async p=>{const img=await renderPage(p, templateRef);renderedImages[p.number]=img;renderPlan(lastPlan)}));const done=Math.min(middle.length,i+3);setProgress('renderProgress',45+Math.round((done/Math.max(1,middle.length))*45),'Rendered '+done+' of '+middle.length+' content pages.')}const ordered=lastPlan.pages.map(p=>renderedImages[p.number]&&renderedImages[p.number].image).filter(Boolean);setProgress('renderProgress',94,'Writing PDF...');const res=await fetch('/api/write-pdf',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({title:$('title').value,images:ordered,workspace})});const data=await res.json();if(!res.ok){$('renderStatus').textContent=data.error||'PDF failed';return}setProgress('renderProgress',100,'Done. Download should start automatically.');$('renderStatus').insertAdjacentHTML('beforeend','<div class="status"><a href="'+esc(data.pdf)+'" target="_blank">Open PDF</a></div>');const a=document.createElement('a');a.href=data.pdf;a.download='';document.body.appendChild(a);a.click();a.remove()}finally{isRendering=false;renderPlan(lastPlan)}}
async function renderTemplate(){templatePrompt=$('templatePrompt')?$('templatePrompt').value:templatePrompt;const res=await fetch('/api/render-template',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({style:lastPlan.style,reference:referencePath,workspace,apiKey,prompt:templatePrompt})});const data=await res.json();if(!res.ok)throw new Error(data.error||'template failed');return {image:data.image,publicUrl:data.publicUrl||''}}
async function renderPage(page,styleReference){setStatus(page.number,'Rendering...');const ref=page.number===1?referencePath:'';const renderPage=Object.assign({},page,{prompt:finalRenderPrompt(page)});const res=await fetch('/api/render-page',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({page:renderPage,styleReference,reference:ref,workspace,apiKey})});const data=await res.json();if(!res.ok){setStatus(page.number,data.error||'Failed');throw new Error(data.error||'render failed')}setStatus(page.number,'Rendered');return {image:data.image,publicUrl:data.publicUrl||''}}
function finalRenderPrompt(page){const side=page.number%2===0?'left-hand page':'right-hand page';const folioSide=page.number%2===0?'left':'right';return String(page.prompt||'')+'\n\nRENDER POSITION: This is page '+page.number+', a '+side+'. Put page number '+page.number+' on the '+folioSide+' side in the footer.'}
function setStatus(n,msg){const el=$('status-'+n);if(el)el.textContent=msg}
renderArticles();updateWorkspaceLabel();showStep(1);requireApiKey();
</script>
</body>
</html>`))
