package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"html"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type config struct {
	Addr                      string
	WorkDir                   string
	DefapiTextCmd             string
	DefapiTextCategory        string
	DefapiTextModel           string
	DefapiTextTimeout         time.Duration
	DefapiImageCmd            string
	DefapiImageCategory       string
	DefapiImageModel          string
	DefapiImageTimeout        time.Duration
	DefapiImageFormat         string
	DefapiImageSize           string
	DefapiImageQuality        string
	DefapiImageBackground     string
	DefapiImageMaxPromptChars int
	DefapiImageRetries        int
}

type server struct {
	cfg      config
	mu       sync.Mutex
	progress map[string]progressStatus
}

type progressStatus struct {
	Kind    string `json:"kind"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Message string `json:"message"`
	Running bool   `json:"running"`
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
	Articles    []article     `json:"articles"`
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
		Items         []rssItem `xml:"item"`
		PodcastMedium string    `xml:"https://podcastindex.org/namespace/1.0 medium"`
	} `xml:"channel"`
	Entries []atomEntry `xml:"entry"`
}

type rssItem struct {
	Title              string              `xml:"title"`
	Link               string              `xml:"link"`
	Description        string              `xml:"description"`
	Content            string              `xml:"encoded"`
	Enclosure          rssEnclosure        `xml:"enclosure"`
	PodcastTranscripts []podcastTranscript `xml:"https://podcastindex.org/namespace/1.0 transcript"`
	PodcastChapters    podcastChapters     `xml:"https://podcastindex.org/namespace/1.0 chapters"`
	PodcastSeason      podcastValue        `xml:"https://podcastindex.org/namespace/1.0 season"`
	PodcastEpisode     podcastValue        `xml:"https://podcastindex.org/namespace/1.0 episode"`
	ItunesSeason       string              `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd season"`
	ItunesEpisode      string              `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd episode"`
	ItunesImage        imageHref           `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
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

type rssEnclosure struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

type imageHref struct {
	Href string `xml:"href,attr"`
}

type podcastTranscript struct {
	URL      string `xml:"url,attr"`
	Type     string `xml:"type,attr"`
	Language string `xml:"language,attr"`
}

type podcastChapters struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

type podcastValue struct {
	Value   string `xml:",chardata"`
	Name    string `xml:"name,attr"`
	Display string `xml:"display,attr"`
}

func main() {
	cfg := parseFlags()
	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		log.Fatal(err)
	}
	s := &server{cfg: cfg, progress: map[string]progressStatus{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/enhance-style", s.handleEnhanceStyle)
	mux.HandleFunc("/api/import-rss", s.handleImportRSS)
	mux.HandleFunc("/api/generate-articles", s.handleGenerateArticles)
	mux.HandleFunc("/api/build", s.handleBuild)
	mux.HandleFunc("/api/render-page", s.handleRenderPage)
	mux.HandleFunc("/api/render-template", s.handleRenderTemplate)
	mux.HandleFunc("/api/write-pdf", s.handleWritePDF)
	mux.HandleFunc("/api/progress", s.handleProgress)
	mux.Handle("/static/", http.StripPrefix("/static/", noCache(http.FileServer(http.Dir("static")))))
	mux.Handle("/work/", http.StripPrefix("/work/", http.FileServer(http.Dir(cfg.WorkDir))))
	log.Printf("magazine-builder listening on http://localhost%s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.Addr, "addr", ":8080", "HTTP listen address")
	flag.StringVar(&cfg.WorkDir, "workdir", "magazine-work", "directory for uploads and generated artifacts")
	flag.StringVar(&cfg.DefapiTextCmd, "defapi-text", "defapi", "defapi text command")
	flag.StringVar(&cfg.DefapiTextCategory, "defapi-text-category", "text", "defapi text category")
	flag.StringVar(&cfg.DefapiTextModel, "defapi-text-model", "claude", "defapi text model")
	flag.DurationVar(&cfg.DefapiTextTimeout, "defapi-text-timeout", 90*time.Second, "defapi text timeout")
	flag.StringVar(&cfg.DefapiImageCmd, "defapi-image", "defapi", "defapi image command")
	flag.StringVar(&cfg.DefapiImageCategory, "defapi-image-category", "image", "defapi image category")
	flag.StringVar(&cfg.DefapiImageModel, "defapi-image-model", "gpt2", "defapi image model")
	flag.DurationVar(&cfg.DefapiImageTimeout, "defapi-image-timeout", 20*time.Minute, "defapi image timeout per page")
	flag.StringVar(&cfg.DefapiImageFormat, "defapi-image-format", "jpeg", "defapi image output format")
	flag.StringVar(&cfg.DefapiImageSize, "defapi-image-size", "auto", "defapi image output size")
	flag.StringVar(&cfg.DefapiImageQuality, "defapi-image-quality", "high", "defapi image output quality")
	flag.StringVar(&cfg.DefapiImageBackground, "defapi-image-background", "opaque", "defapi image background")
	flag.IntVar(&cfg.DefapiImageMaxPromptChars, "defapi-image-max-prompt-chars", 4000, "maximum defapi image prompt length")
	flag.IntVar(&cfg.DefapiImageRetries, "defapi-image-retries", 2, "retry attempts for failed defapi image calls")
	flag.Parse()
	return cfg
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, filepath.Join("static", "index.html"))
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
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
	referencePath := strings.TrimSpace(r.FormValue("reference"))
	if referencePath != "" && defapiImageRef(referencePath) == "" {
		writeError(w, http.StatusBadRequest, errors.New("reference must be a public http(s) image URL"))
		return
	}
	enhanced, err := s.enhanceStyle(ctx, title, style, referencePath)
	if err != nil {
		log.Printf("defapi text style enhancement failed: %v", err)
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
		if articles[i].Kind == "podcast" && len([]rune(articles[i].Body)) > 3500 {
			summarized, err := s.summarizePodcastForImport(ctx, articles[i], style)
			if err != nil {
				log.Printf("defapi text podcast summary failed for %q: %v", articles[i].Title, err)
				articles[i].Body = sampleLongText(articles[i].Body, 3200)
			} else {
				articles[i].Title = summarized.Title
				articles[i].Body = summarized.Body
			}
		}
		improved, err := s.rewriteArticleForStyle(ctx, articles[i], style)
		if err != nil {
			log.Printf("defapi text article rewrite failed for %q: %v", articles[i].Title, err)
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
	total := 1
	for _, a := range req.Articles {
		if !a.Enhanced {
			total++
		}
	}
	done := 0
	completed := false
	s.setProgress(workspace, progressStatus{Kind: "build", Done: done, Total: total, Message: "Starting defapi text work", Running: true})
	defer func() {
		if !completed {
			s.setProgress(workspace, progressStatus{Kind: "build", Done: done, Total: total, Message: "Build interrupted or failed", Running: false})
		}
	}()
	s.workspaceLog(workspace, "build: start title=%q articles=%d pages=%d", req.Title, len(req.Articles), req.PageCount)
	for i := range req.Articles {
		if req.Articles[i].Enhanced {
			continue
		}
		s.setProgress(workspace, progressStatus{Kind: "build", Done: done, Total: total, Message: fmt.Sprintf("Rewriting %q", emptyDefault(req.Articles[i].Title, "Untitled")), Running: true})
		var improved article
		var err error
		if req.Articles[i].Kind == "feature" {
			improved, err = s.rewriteFeatureForStyle(ctx, req.Articles[i], style)
		} else {
			improved, err = s.rewriteArticleForStyle(ctx, req.Articles[i], style)
		}
		if err != nil {
			log.Printf("defapi text manual article rewrite failed for %q: %v", req.Articles[i].Title, err)
			done++
			s.setProgress(workspace, progressStatus{Kind: "build", Done: done, Total: total, Message: fmt.Sprintf("Rewrite failed for %q", emptyDefault(req.Articles[i].Title, "Untitled")), Running: true})
			continue
		}
		improved.Enhanced = true
		req.Articles[i] = improved
		done++
		s.setProgress(workspace, progressStatus{Kind: "build", Done: done, Total: total, Message: fmt.Sprintf("Rewritten %q", emptyDefault(req.Articles[i].Title, "Untitled")), Running: true})
	}
	s.setProgress(workspace, progressStatus{Kind: "build", Done: done, Total: total, Message: "Generating creative kit", Running: true})
	kit, err := s.generateCreativeKit(ctx, req, style)
	if err != nil {
		log.Printf("defapi text creative kit failed: %v", err)
		s.workspaceLog(workspace, "build: creative kit failed: %v", err)
		kit = fallbackCreativeKit(req)
	}
	done++
	s.setProgress(workspace, progressStatus{Kind: "build", Done: done, Total: total, Message: "Plan ready", Running: false})
	completed = true
	s.workspaceLog(workspace, "build: complete")
	writeJSON(w, buildResponse{Style: style, CreativeKit: kit, Articles: req.Articles, Pages: planMagazine(req, style, kit), Workspace: workspace})
}

func (s *server) handleProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	workspace := sanitizeWorkspace(r.URL.Query().Get("workspace"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	status := s.progressStatus(workspace, kind)
	writeJSON(w, status)
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
	prompt = limitPrompt(prompt, s.cfg.DefapiImageMaxPromptChars)
	workspace, err := s.ensureWorkspace(req.Workspace, req.Style.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.workspaceLog(workspace, "render-template: start")
	image, err := s.runDefapiImageWithRetry(ctx, workspace, 0, prompt, filterStrings([]string{req.Reference}), true)
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
	image, err := s.runDefapiImageWithRetry(ctx, workspace, req.Page.Number, limitPrompt(req.Page.Prompt, s.cfg.DefapiImageMaxPromptChars), images, false)
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

func (s *server) setProgress(workspace string, status progressStatus) {
	workspace = sanitizeWorkspace(workspace)
	if workspace == "" {
		return
	}
	if status.Kind == "" {
		status.Kind = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress == nil {
		s.progress = map[string]progressStatus{}
	}
	s.progress[workspace+":"+status.Kind] = status
}

func (s *server) progressStatus(workspace, kind string) progressStatus {
	workspace = sanitizeWorkspace(workspace)
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress == nil {
		return progressStatus{Kind: kind, Message: "No progress yet"}
	}
	if status, ok := s.progress[workspace+":"+kind]; ok {
		return status
	}
	return progressStatus{Kind: kind, Message: "No progress yet"}
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
		prompt += "\n\nReference image URL: " + referencePath + "\nUse this URL as visual inspiration for palette, typography mood, texture and layout feeling, but describe the reusable style in words."
	}
	text, err := s.runDefapiText(ctx, prompt, 1200)
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
	text, err := s.runDefapiText(ctx, prompt, 1800)
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
	text, err := s.runDefapiText(ctx, prompt, 1000)
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

func (s *server) summarizePodcastForImport(ctx context.Context, a article, style magazineStyle) (article, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and body. Summarize this podcast episode source material into print-editorial notes for a later magazine article rewrite. Preserve concrete facts, people, works, chronology, arguments, opinions and useful chapter structure. Do not mention transcripts, timestamps, RSS, show notes or source mechanics. Title should be a concise episode/article title. Body should be 1400-2200 characters, factual and balanced, not finished prose.\n\nSTYLE CONTEXT: %s\n\nEPISODE TITLE: %s\nSOURCE MATERIAL SAMPLE:\n%s", styleLine(style, "article"), a.Title, sampleLongText(a.Body, 12000))
	text, err := s.runDefapiText(ctx, prompt, 1200)
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

func (s *server) rewriteFeatureForStyle(ctx context.Context, a article, style magazineStyle) (article, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and body. Turn this requested magazine feature page into a precise image-generation brief matching the publication style. The feature can be a crossword, comments page, quiz, TV/program listings, puzzle page, letters, classifieds, calendar, chart, or any other non-article department. Preserve the user's intent, but make it print-ready and specific about sections/modules. Body should be 700-1400 characters and describe what the page should contain.\n\nSTYLE: %s\n\nFEATURE TITLE: %s\nFEATURE REQUEST: %s", styleLine(style, "filler"), a.Title, compact(a.Body, 2400))
	text, err := s.runDefapiText(ctx, prompt, 900)
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
	text, err := s.runDefapiText(ctx, prompt, 2200)
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

func (s *server) runDefapiText(ctx context.Context, prompt string, maxTokens int) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, s.cfg.DefapiTextTimeout)
	defer cancel()
	args := commandArgs(s.cfg.DefapiTextCategory, s.cfg.DefapiTextModel, "-max-tokens", strconv.Itoa(maxTokens), prompt)
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

func (s *server) runDefapiImageWithRetry(ctx context.Context, workspace string, pageNumber int, prompt string, images []string, allowPromptTweak bool) (generatedImage, error) {
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
		if attempt == 0 && allowPromptTweak {
			tweaked, tweakErr := s.tweakTemplatePrompt(ctx, currentPrompt, err)
			if tweakErr != nil {
				s.workspaceLog(workspace, "defapi image-retry: page=%d prompt tweak failed: %v", pageNumber, tweakErr)
				currentPrompt = safeTemplatePrompt()
			} else {
				currentPrompt = limitPrompt(tweaked, s.cfg.DefapiImageMaxPromptChars)
			}
			s.workspaceLog(workspace, "defapi image-retry: page=%d using safer template prompt chars=%d", pageNumber, len([]rune(currentPrompt)))
		}
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

func (s *server) tweakTemplatePrompt(ctx context.Context, prompt string, failure error) (string, error) {
	req := fmt.Sprintf("Rewrite this image-generation prompt to be safer and simpler. Keep the same goal: a blank printed magazine content-page template. Remove anything that could be interpreted as forbidden, confusing, label-heavy, or too complex. No markdown, return only the revised prompt under 1800 characters.\n\nFailure: %v\n\nPrompt:\n%s", failure, prompt)
	return s.runDefapiText(ctx, req, 700)
}

func safeTemplatePrompt() string {
	return "Create one completely text-free blank magazine layout page. Portrait page, aspect ratio 1240:1754, full page visible, no crop. Use only generic visual shapes: margins, a blank top band, a blank bottom band, a two-column grid, pale empty image rectangles, soft grey text-block shapes without letters, and subtle paper texture. No readable text, no letters, no numbers, no masthead, no captions, no labels, no arrows, no diagrams, no UI, no technical print marks. Calm editorial layout reference only."
}

func defaultTemplatePrompt(style magazineStyle) string {
	return fmt.Sprintf("Create one completely text-free magazine page layout template.\n%s\nSTYLE: %s\nTEMPLATE: %s\nShow only generic layout geometry: paper texture, margins, column rhythm, blank header band, blank footer band, empty image rectangles, pale rule lines and subtle placeholder blocks. Absolutely no readable text, no letters, no numbers, no masthead, no captions, no labels, no arrows, no registration marks, no printing press technical marks, no UI and no moodboard. It should be a calm blank layout reference for later pages.", pageFormatInstruction(), styleLine(style, "template"), style.Template)
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
	for _, img := range limitStrings(uniqueStrings(images), 16) {
		ref := defapiImageRef(img)
		if ref != "" {
			args = append(args, "-image", ref)
		}
	}
	s.workspaceLog(workspace, "defapi image: page=%d prompt_chars=%d input_refs=%d accepted_refs=%d", pageNumber, len([]rune(prompt)), len(images), (len(args)-8)/2)
	args = append(commandArgs(s.cfg.DefapiImageCategory, s.cfg.DefapiImageModel), append(args, limitPrompt(prompt, s.cfg.DefapiImageMaxPromptChars))...)
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
	isPodcastFeed := strings.EqualFold(strings.TrimSpace(feed.Channel.PodcastMedium), "podcast")
	for _, item := range feed.Channel.Items {
		a := rssItemArticle(ctx, item, isPodcastFeed)
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

func rssItemArticle(ctx context.Context, item rssItem, isPodcastFeed bool) article {
	source := strings.TrimSpace(item.Link)
	if isPodcastFeed || item.isPodcastEpisode() {
		return podcastItemArticle(ctx, item, source)
	}
	body := item.Content
	if body == "" {
		body = item.Description
	}
	a := article{Title: cleanText(item.Title), Body: cleanText(stripArticleHTML(body)), Source: source, Images: extractImageURLs(body, source)}
	return enrichArticleFromURL(ctx, a)
}

func (item rssItem) isPodcastEpisode() bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(item.Enclosure.Type)), "audio/") ||
		len(item.PodcastTranscripts) > 0 ||
		strings.TrimSpace(item.PodcastChapters.URL) != "" ||
		strings.TrimSpace(item.PodcastSeason.Value) != "" ||
		strings.TrimSpace(item.PodcastEpisode.Value) != ""
}

func podcastItemArticle(ctx context.Context, item rssItem, source string) article {
	description := cleanText(stripArticleHTML(item.Description))
	body := description
	if transcriptURL := firstTranscriptURL(item.PodcastTranscripts); transcriptURL != "" {
		if transcript, err := fetchPodcastTranscript(ctx, transcriptURL); err == nil && transcript != "" {
			body = strings.TrimSpace(strings.Join(filterStrings([]string{description, transcript}), "\n\n"))
		}
	}
	body = strings.TrimSpace(strings.Join(filterStrings([]string{podcastMetadata(ctx, item), body}), "\n\n"))
	images := extractImageURLs(item.Description, source)
	if href := strings.TrimSpace(item.ItunesImage.Href); href != "" {
		images = append(images, href)
	}
	return article{Title: cleanText(item.Title), Body: cleanText(body), Source: source, Images: uniqueStrings(images), Kind: "podcast"}
}

func firstTranscriptURL(transcripts []podcastTranscript) string {
	for _, transcript := range transcripts {
		u := strings.TrimSpace(transcript.URL)
		if u != "" {
			return u
		}
	}
	return ""
}

func podcastMetadata(ctx context.Context, item rssItem) string {
	parts := []string{}
	if season := podcastValueText(item.PodcastSeason, item.ItunesSeason); season != "" {
		parts = append(parts, "Season: "+season)
	}
	if episode := podcastValueText(item.PodcastEpisode, item.ItunesEpisode); episode != "" {
		parts = append(parts, "Episode: "+episode)
	}
	if chapters := podcastChaptersText(ctx, item.PodcastChapters.URL); chapters != "" {
		parts = append(parts, "Chapters: "+chapters)
	} else if u := strings.TrimSpace(item.PodcastChapters.URL); u != "" {
		parts = append(parts, "Chapters URL: "+u)
	}
	return strings.Join(parts, "\n")
}

func podcastValueText(v podcastValue, fallback string) string {
	display := strings.TrimSpace(v.Display)
	value := cleanText(v.Value)
	name := strings.TrimSpace(v.Name)
	if display != "" {
		return display
	}
	if name != "" && value != "" {
		return name + " (" + value + ")"
	}
	if name != "" {
		return name
	}
	if value != "" {
		return value
	}
	return cleanText(fallback)
}

func fetchPodcastTranscript(ctx context.Context, transcriptURL string) (string, error) {
	text, err := fetchURLTextFunc(ctx, transcriptURL, 6<<20)
	if err != nil {
		return "", err
	}
	return cleanTranscriptText(text), nil
}

func podcastChaptersText(ctx context.Context, chaptersURL string) string {
	chaptersURL = strings.TrimSpace(chaptersURL)
	if chaptersURL == "" {
		return ""
	}
	text, err := fetchURLTextFunc(ctx, chaptersURL, 1<<20)
	if err != nil {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return cleanText(text)
	}
	titles := []string{}
	collectJSONTitles(decoded, &titles)
	return strings.Join(limitStrings(uniqueStrings(titles), 16), "; ")
}

func collectJSONTitles(v any, titles *[]string) {
	switch x := v.(type) {
	case map[string]any:
		if title, ok := x["title"].(string); ok && strings.TrimSpace(title) != "" {
			*titles = append(*titles, cleanText(title))
		}
		for _, child := range x {
			collectJSONTitles(child, titles)
		}
	case []any:
		for _, child := range x {
			collectJSONTitles(child, titles)
		}
	}
}

func fetchURLText(ctx context.Context, rawURL string, maxBytes int64) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", errors.New("missing URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "magazine-builder/0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

var fetchURLTextFunc = fetchURLText

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
		if itemPart == 0 && isAdvertPage(n, req.PageCount) {
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
	return limitPrompt(fmt.Sprintf("Create the cover of %q, a %s.\n%s\nSTYLE: %s\nCOVER STYLE: %s\nInclude masthead, issue date, price/barcode or equivalent furniture, strong hierarchy, and avoid: %s. Cover-line story references and final page numbers are added at render time after page ordering is finished.", title, magType, pageFormatInstruction(), styleLine(style, "core"), style.Cover, style.Avoid), 3900)
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
		base += ". Use the reference image URL as visual inspiration for palette, texture, typography mood, and image treatment."
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
var transcriptTimestampRE = regexp.MustCompile(`^\d{1,2}:\d{2}(?::\d{2})?(?:[.,]\d{1,3})?\s*-->\s*\d{1,2}:\d{2}(?::\d{2})?(?:[.,]\d{1,3})?`)
var transcriptCueRE = regexp.MustCompile(`^\d+$`)

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

func cleanTranscriptText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" ||
			strings.EqualFold(line, "WEBVTT") ||
			strings.HasPrefix(strings.ToUpper(line), "NOTE") ||
			transcriptTimestampRE.MatchString(line) ||
			transcriptCueRE.MatchString(line) {
			continue
		}
		out = append(out, line)
	}
	return cleanText(stripHTML(strings.Join(out, " ")))
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

func sampleLongText(s string, max int) string {
	s = cleanText(s)
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	if max < 1200 {
		return string(r[:max]) + "..."
	}
	part := max / 3
	head := strings.TrimSpace(string(r[:part]))
	midStart := (len(r) - part) / 2
	middle := strings.TrimSpace(string(r[midStart : midStart+part]))
	tail := strings.TrimSpace(string(r[len(r)-part:]))
	return head + "\n\n[Middle excerpt]\n" + middle + "\n\n[Final excerpt]\n" + tail
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
