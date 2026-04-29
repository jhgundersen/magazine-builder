package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
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
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	repositoryName = "jhgundersen/magazine-builder"
	binaryName     = "magazine-builder"
)

//go:embed static/*
var embeddedStatic embed.FS

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
type textModelContextKey struct{}
type imageModelContextKey struct{}

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
	Offset    int    `json:"offset"`
	Style     string `json:"style"`
	Workspace string `json:"workspace"`
	APIKey    string `json:"apiKey"`
	TextModel string `json:"textModel"`
}

type generateArticlesRequest struct {
	Title     string `json:"title"`
	Style     string `json:"style"`
	Count     int    `json:"count"`
	Workspace string `json:"workspace"`
	APIKey    string `json:"apiKey"`
	TextModel string `json:"textModel"`
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
	TextModel    string    `json:"textModel"`
}

type buildResponse struct {
	Style       magazineStyle `json:"style"`
	CreativeKit creativeKit   `json:"creativeKit"`
	Articles    []article     `json:"articles"`
	Pages       []pagePlan    `json:"pages"`
	Reference   string        `json:"reference,omitempty"`
	Workspace   string        `json:"workspace"`
}

type coverPlanRequest struct {
	Title     string        `json:"title"`
	Style     magazineStyle `json:"style"`
	Pages     []pagePlan    `json:"pages"`
	Workspace string        `json:"workspace"`
	APIKey    string        `json:"apiKey"`
	TextModel string        `json:"textModel"`
}

type coverPlan struct {
	Language       string          `json:"language"`
	MainStoryTitle string          `json:"mainStoryTitle"`
	Lines          []coverLineItem `json:"lines"`
}

type coverLineItem struct {
	Page  int    `json:"page"`
	Title string `json:"title"`
	Label string `json:"label"`
	Role  string `json:"role,omitempty"`
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
	Language   string `json:"language"`
	Tone       string `json:"tone"`
	Core       string `json:"core"`
	Cover      string `json:"cover"`
	Content    string `json:"content"`
	Feature    string `json:"feature"`
	Short      string `json:"short"`
	Advert     string `json:"advert"`
	Filler     string `json:"filler"`
	Back       string `json:"back"`
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
	Page           pagePlan      `json:"page"`
	Style          magazineStyle `json:"style"`
	StyleReference string        `json:"styleReference"`
	Reference      string        `json:"reference"`
	Workspace      string        `json:"workspace"`
	APIKey         string        `json:"apiKey"`
	TextModel      string        `json:"textModel"`
	ImageModel     string        `json:"imageModel"`
}

type renderPageResponse struct {
	Image     string `json:"image"`
	PublicURL string `json:"publicUrl,omitempty"`
}

type generatedImage struct {
	Image     string
	PublicURL string
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
	PubDate            string              `xml:"pubDate"`
	Date               string              `xml:"http://purl.org/dc/elements/1.1/ date"`
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
	Title     string     `xml:"title"`
	Link      []atomLink `xml:"link"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
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

type feedCandidate struct {
	Kind      string
	RSSItem   rssItem
	AtomEntry atomEntry
	Date      time.Time
	Index     int
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
	if len(os.Args) > 1 && os.Args[1] == "update" {
		if err := runUpdate(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	cfg := parseFlags()
	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		log.Fatal(err)
	}
	s := &server{cfg: cfg, progress: map[string]progressStatus{}}
	handler, err := s.routes()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("magazine-builder listening on http://localhost%s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, handler))
}

func (s *server) routes() (http.Handler, error) {
	staticFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/enhance-style", s.handleEnhanceStyle)
	mux.HandleFunc("/api/import-rss", s.handleImportRSS)
	mux.HandleFunc("/api/generate-articles", s.handleGenerateArticles)
	mux.HandleFunc("/api/build", s.handleBuild)
	mux.HandleFunc("/api/cover-plan", s.handleCoverPlan)
	mux.HandleFunc("/api/render-page", s.handleRenderPage)
	mux.HandleFunc("/api/write-pdf", s.handleWritePDF)
	mux.HandleFunc("/api/progress", s.handleProgress)
	mux.Handle("/static/", http.StripPrefix("/static/", noCache(http.FileServer(http.FS(staticFS)))))
	mux.Handle("/work/", http.StripPrefix("/work/", http.FileServer(http.Dir(s.cfg.WorkDir))))
	return mux, nil
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

type updateOptions struct {
	Repo       string
	Prefix     string
	InstallDir string
}

type latestRelease struct {
	TagName string `json:"tag_name"`
}

func runUpdate(args []string) error {
	opts := updateOptions{
		Repo:   repositoryName,
		Prefix: defaultPrefix(),
	}
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&opts.Repo, "repo", opts.Repo, "GitHub repository to update from")
	flags.StringVar(&opts.Prefix, "prefix", opts.Prefix, "installation prefix used when the current executable is not magazine-builder")
	flags.StringVar(&opts.InstallDir, "install-dir", "", "directory containing the magazine-builder binary")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	asset, err := releaseAssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	target, err := updateTarget(opts)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tag, err := latestReleaseTag(ctx, opts.Repo)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", opts.Repo, tag, asset)
	fmt.Printf("Updating %s to %s...\n", target, tag)
	if err := downloadAndReplace(ctx, url, target); err != nil {
		return err
	}
	fmt.Printf("Updated: %s\n", target)
	return nil
}

func releaseAssetName(goos, goarch string) (string, error) {
	switch goos {
	case "linux", "darwin":
	default:
		return "", fmt.Errorf("unsupported OS: %s", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
	return fmt.Sprintf("%s-%s-%s", binaryName, goos, goarch), nil
}

func updateTarget(opts updateOptions) (string, error) {
	if opts.InstallDir != "" {
		return resolvedPath(filepath.Join(opts.InstallDir, binaryName)), nil
	}
	exe, err := os.Executable()
	if err == nil {
		exe = resolvedPath(exe)
		if filepath.Base(exe) == binaryName {
			return exe, nil
		}
	}
	if opts.Prefix == "" {
		opts.Prefix = defaultPrefix()
	}
	return resolvedPath(filepath.Join(opts.Prefix, "bin", binaryName)), nil
}

func resolvedPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func defaultPrefix() string {
	if prefix := os.Getenv("PREFIX"); prefix != "" {
		return prefix
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local")
}

func latestReleaseTag(ctx context.Context, repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", binaryName)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("latest release lookup failed: %s", resp.Status)
	}
	var release latestRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", errors.New("latest release response did not include a tag")
	}
	return release.TagName, nil
}

func downloadAndReplace(ctx context.Context, url, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+binaryName+"-update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	req.Header.Set("User-Agent", binaryName)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = tmp.Close()
		return fmt.Errorf("binary download failed: %s", resp.Status)
	}
	n, err := io.Copy(tmp, resp.Body)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if n == 0 {
		_ = tmp.Close()
		return errors.New("downloaded binary was empty")
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	index, err := embeddedStatic.ReadFile("static/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
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
	ctx := contextWithModels(contextWithAPIKey(r.Context(), r.FormValue("apiKey")), r.FormValue("textModel"), "")
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
		s.workspaceLog(workspace, "style: enhancement failed: %v", err)
		enhanced = fallbackStyle(style, referencePath)
	}
	if title != "" {
		enhanced.Name = title
	}
	styleJSON, _ := json.MarshalIndent(enhanced, "", "  ")
	s.workspaceLog(workspace, "style: source title=%q reference=%q user_style=%q", title, referencePath, compact(style, 1200))
	s.workspaceLogJSON(workspace, "style: enhanced JSON", enhanced)
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
	ctx := contextWithModels(contextWithAPIKey(r.Context(), req.APIKey), req.TextModel, "")
	if req.Limit <= 0 || req.Limit > 50 {
		req.Limit = 10
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	workspace, err := s.ensureWorkspace(req.Workspace, req.Style)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.workspaceLog(workspace, "rss-import: start url=%q offset=%d limit=%d", req.URL, req.Offset, req.Limit)
	articles, err := fetchRSS(ctx, req.URL, req.Offset, req.Limit)
	if err != nil {
		s.workspaceLog(workspace, "rss-import: fetch failed: %v", err)
		writeError(w, http.StatusBadGateway, err)
		return
	}
	style := parseStyle(req.Style)
	s.workspaceLogJSON(workspace, "rss-import: style JSON", style)
	rawEntries := make([]articleLogEntry, len(articles))
	for i, a := range articles {
		rawEntries[i] = articleLogEntryFromArticle(i, a, false)
	}
	s.workspaceLogJSON(workspace, "rss-import: extracted articles", rawEntries)
	rewriteEntries := make([]articleLogEntry, len(articles))
	parallelMap(len(articles), 3, func(i int) {
		if articles[i].Kind == "podcast" && len([]rune(articles[i].Body)) > 5000 {
			summarized, err := s.summarizePodcastForImport(ctx, articles[i], style)
			if err != nil {
				log.Printf("defapi text podcast summary failed for %q: %v", articles[i].Title, err)
				rewriteEntries[i] = articleLogEntryFromArticle(i, articles[i], false)
				rewriteEntries[i].Error = fmt.Sprintf("podcast summary failed: %v", err)
				articles[i].Body = sampleLongText(articles[i].Body, 4800)
			} else {
				articles[i].Title = summarized.Title
				articles[i].Body = summarized.Body
			}
		}
		improved, err := s.rewriteArticleForStyle(ctx, articles[i], style)
		if err != nil {
			log.Printf("defapi text article rewrite failed for %q: %v", articles[i].Title, err)
			rewriteEntries[i] = articleLogEntryFromArticle(i, articles[i], false)
			if rewriteEntries[i].Error != "" {
				rewriteEntries[i].Error += "; "
			}
			rewriteEntries[i].Error += fmt.Sprintf("article rewrite failed: %v", err)
			return
		}
		articles[i].Title = improved.Title
		articles[i].Body = improved.Body
		articles[i].Enhanced = true
		rewriteEntries[i] = articleLogEntryFromArticle(i, articles[i], true)
	})
	for i := range rewriteEntries {
		if rewriteEntries[i].Title == "" && rewriteEntries[i].Body == "" && rewriteEntries[i].Error == "" {
			rewriteEntries[i] = articleLogEntryFromArticle(i, articles[i], true)
		}
	}
	s.workspaceLogJSON(workspace, "rss-import: rewritten articles", rewriteEntries)
	s.workspaceLog(workspace, "rss-import: complete articles=%d rewritten=%d", len(articles), countEnhancedArticles(articles))
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
	ctx := contextWithModels(contextWithAPIKey(r.Context(), req.APIKey), req.TextModel, "")
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
	ctx := contextWithModels(contextWithAPIKey(r.Context(), req.APIKey), req.TextModel, "")
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
	s.workspaceLogJSON(workspace, "build: style JSON", style)
	s.workspaceLogJSON(workspace, "build: input articles", articleLogEntries(req.Articles, false))
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
			s.workspaceLog(workspace, "build: rewrite failed index=%d title=%q error=%v", i, req.Articles[i].Title, err)
			done++
			s.setProgress(workspace, progressStatus{Kind: "build", Done: done, Total: total, Message: fmt.Sprintf("Rewrite failed for %q", emptyDefault(req.Articles[i].Title, "Untitled")), Running: true})
			continue
		}
		improved.Enhanced = true
		req.Articles[i] = improved
		s.workspaceLogJSON(workspace, fmt.Sprintf("build: rewritten article index=%d", i), articleLogEntryFromArticle(i, req.Articles[i], true))
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
	s.workspaceLogJSON(workspace, "build: final articles", articleLogEntries(req.Articles, true))
	s.workspaceLogJSON(workspace, "build: creative kit JSON", kit)
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

func (s *server) handleCoverPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req coverPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx := contextWithModels(contextWithAPIKey(r.Context(), req.APIKey), req.TextModel, "")
	workspace, err := s.ensureWorkspace(req.Workspace, req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.workspaceLog(workspace, "cover-plan: start pages=%d", len(req.Pages))
	plan, err := s.generateCoverPlan(ctx, req.Title, req.Style, req.Pages)
	if err != nil {
		s.workspaceLog(workspace, "cover-plan: failed: %v", err)
		plan = fallbackCoverPlan(req.Style, req.Pages)
	}
	writeJSON(w, map[string]any{"coverPlan": plan, "workspace": workspace})
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
	ctx := contextWithModels(contextWithAPIKey(r.Context(), req.APIKey), req.TextModel, req.ImageModel)
	images := filterStrings([]string{req.StyleReference, req.Reference})
	images = append(images, req.Page.Images...)
	workspace, err := s.ensureWorkspace(req.Workspace, req.Page.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.Page.Prompt = s.pagePromptWithFurniture(ctx, req.Style, req.Page)
	s.workspaceLog(workspace, "render-page: start page=%d title=%q refs=%d", req.Page.Number, req.Page.Title, len(images))
	image, err := s.runDefapiImageWithRetry(ctx, workspace, req.Page.Number, limitPrompt(req.Page.Prompt, s.cfg.DefapiImageMaxPromptChars), images)
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

func (s *server) workspaceLogJSON(workspace, label string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		s.workspaceLog(workspace, "%s: json encode failed: %v", label, err)
		return
	}
	s.workspaceLog(workspace, "%s:\n%s", label, data)
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
	prompt := "Return only valid compact JSON for a reusable magazine/newspaper/comic style. No markdown. Keep each field under 260 characters so later image prompts stay under 4000 chars. Required keys: name, language, tone, core, cover, content, feature, short, advert, filler, back, typography, color, print, avoid. The name field must be exactly " + strconv.Quote(emptyDefault(title, "Untitled Magazine")) + ". The language field must be the publication language inferred from the user style/title, such as English, Norwegian Bokmal, French, etc. The tone field must describe the text voice for article rewrites and generated page furniture. If the user asks for a comic, satire, parody, hysterical/adult humor, tabloid, puzzle, or other strong format, do not flatten it into a normal respectable magazine. Put explicit structural instructions into content, feature, short, filler and back: panels, cartoons, captions, recurring gags, fake departments, absurd infographics, pull-quotes, punchlines, and visual comedy. Adult comic means aimed at grown-ups, not explicit or vulgar unless the user asks. Define guidance for cover, normal content pages, feature articles, short articles, adverts, filler/departments and back page. Include consistent header, footer, folio and grid guidance in content/core rather than a separate template page.\n\nUser style:\n" + emptyDefault(style, "clean contemporary general-interest magazine")
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
	prompt := fmt.Sprintf("Return only valid compact JSON. Required keys: departments, adverts, sidebars, captions, backPage. Make departments, sidebars and captions arrays of 18-24 unique short strings each. Make adverts and backPage arrays of 10-16 unique short strings each. Every string must describe one specific reusable page element, never a duplicate or near-duplicate. Prepare issue-wide generic page elements for a %s called %q. Match this style and tone: %s. Avoid copyrighted brands unless supplied by the user.\n\nArticles:\n%s", emptyDefault(req.MagazineType, "magazine"), emptyDefault(req.Title, "Untitled Magazine"), styleLine(style, "content"), articleList(req.Articles))
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
	bodyRange := "900-1600"
	bodySample := compact(a.Body, 3200)
	maxTokens := 1000
	if a.Kind == "podcast" {
		bodyRange = "1800-2800"
		bodySample = sampleLongText(a.Body, 6500)
		maxTokens = 1600
	}
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and body. Rewrite this imported source into print-ready magazine copy matching the publication concept, style and text tone. If the publication is comic-led, satirical, tabloid, puzzle-like, literary, technical, or otherwise strongly formatted, make the copy sound and structure fit that format. Keep facts, names, chronology, arguments, concrete examples and useful nuance. Remove web/navigation language, links, embeds, YouTube mentions, newsletter prompts, transcript mechanics and SEO clutter. Title should fit the publication voice. Body should be %s characters, in coherent paragraphs or short page-ready chunks as the style demands.\n\nSTYLE AND TONE: %s\n\nSOURCE TITLE: %s\nSOURCE BODY: %s", bodyRange, styleLine(style, "article"), a.Title, bodySample)
	text, err := s.runDefapiText(ctx, prompt, maxTokens)
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
	prompt := fmt.Sprintf("Return only valid compact JSON with keys title and body. Summarize this podcast episode source material into detailed print-editorial notes for a later magazine article rewrite. Preserve concrete facts, people, works, chronology, arguments, opinions, disagreements, examples, recommendations and useful chapter structure. Prefer substance over polish. Do not mention transcripts, timestamps, RSS, show notes or source mechanics. Title should be a concise episode/article title. Body should be 2600-4200 characters, factual and balanced, not finished prose.\n\nSTYLE CONTEXT: %s\n\nEPISODE TITLE: %s\nSOURCE MATERIAL SAMPLE:\n%s", styleLine(style, "article"), a.Title, sampleLongText(a.Body, 18000))
	text, err := s.runDefapiText(ctx, prompt, 2200)
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

func (s *server) generateCoverPlan(ctx context.Context, title string, style magazineStyle, pages []pagePlan) (coverPlan, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with keys language, mainStoryTitle, lines. The lines value must be an array of 4-7 objects with keys page, title, label and role. Choose the strongest cover lines from the final page order. Identify exactly one main story using mainStoryTitle and role=\"main\" on that line. Translate page labels into the publication language; do not use English-only shorthand like p2 unless that is natural for the language. Use the final page numbers exactly as supplied. Avoid adverts unless the issue has too few editorial pages.\n\nPUBLICATION: %s\nLANGUAGE: %s\nSTYLE: %s\nFINAL PAGES:\n%s", emptyDefault(title, style.Name), emptyDefault(style.Language, "English"), styleLine(style, "cover"), pageListForCover(pages))
	text, err := s.runDefapiText(ctx, prompt, 900)
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

type pageFurniture struct {
	Header string `json:"header"`
	Footer string `json:"footer"`
}

func (s *server) generatePageFurniture(ctx context.Context, style magazineStyle, page pagePlan) (pageFurniture, error) {
	prompt := fmt.Sprintf("Return only valid compact JSON with keys header and footer. Write very short localized magazine page furniture in %s. Tone: %s. Header: 1-5 words suitable for a running header or section slug for this page. Footer: 1-6 words that can sit beside page number %d. Use the article/page content, do not repeat a long headline. Match this publication style: %s.\n\nPAGE KIND: %s\nPAGE TITLE: %s\nPAGE BODY: %s", emptyDefault(style.Language, "English"), emptyDefault(style.Tone, "editorial"), page.Number, styleLine(style, page.Kind), page.Kind, page.Title, pageBodyForFurniture(page))
	text, err := s.runDefapiText(ctx, prompt, 300)
	if err != nil {
		return pageFurniture{}, err
	}
	var out pageFurniture
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &out); err != nil {
		return pageFurniture{}, err
	}
	out.Header = compact(cleanText(out.Header), 60)
	out.Footer = compact(cleanText(out.Footer), 70)
	if out.Header == "" || out.Footer == "" {
		return pageFurniture{}, errors.New("empty page furniture")
	}
	return out, nil
}

func (s *server) pagePromptWithFurniture(ctx context.Context, style magazineStyle, page pagePlan) string {
	if strings.EqualFold(page.Kind, "cover") {
		return page.Prompt
	}
	copy, err := s.generatePageFurniture(ctx, style, page)
	if err != nil {
		log.Printf("defapi text page furniture failed for page %d: %v", page.Number, err)
		copy = fallbackPageFurniture(style, page)
	}
	side := pageSide(page.Number)
	outer := pageNumberSide(page.Number)
	payload := map[string]any{
		"header":   copy.Header,
		"footer":   copy.Footer,
		"language": emptyDefault(style.Language, "English"),
		"side":     side,
		"page":     page.Number,
		"folio":    fmt.Sprintf("Place page number %d on the %s outer footer edge.", page.Number, outer),
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(page.Prompt), &decoded); err == nil {
		decoded["page_furniture"] = payload
		return compactJSON(decoded)
	}
	return page.Prompt + "\n\nPAGE FURNITURE JSON: " + compactJSON(payload)
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

func pageListForCover(pages []pagePlan) string {
	parts := []string{}
	for _, p := range pages {
		if p.Number <= 1 {
			continue
		}
		body := ""
		if p.Article != nil {
			body = compact(p.Article.Body, 180)
		}
		parts = append(parts, fmt.Sprintf("- page %d | kind=%s | title=%s | body=%s", p.Number, p.Kind, emptyDefault(p.Title, "Untitled"), body))
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

func (s *server) runDefapiText(ctx context.Context, prompt string, maxTokens int) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, s.cfg.DefapiTextTimeout)
	defer cancel()
	args := commandArgs(s.cfg.DefapiTextCategory, textModelFromContext(ctx, s.cfg.DefapiTextModel), "-max-tokens", strconv.Itoa(maxTokens), prompt)
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
	for _, img := range limitStrings(uniqueStrings(images), 16) {
		ref := defapiImageRef(img)
		if ref != "" {
			args = append(args, "-image", ref)
		}
	}
	s.workspaceLog(workspace, "defapi image: page=%d prompt_chars=%d input_refs=%d accepted_refs=%d", pageNumber, len([]rune(prompt)), len(images), (len(args)-8)/2)
	args = append(commandArgs(s.cfg.DefapiImageCategory, imageModelFromContext(ctx, s.cfg.DefapiImageModel)), append(args, limitPrompt(prompt, s.cfg.DefapiImageMaxPromptChars))...)
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

func fetchRSS(ctx context.Context, feedURL string, offset, limit int) ([]article, error) {
	feedURL = strings.TrimSpace(feedURL)
	if feedURL == "" {
		return nil, errors.New("missing RSS URL")
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 10
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
	candidates := []feedCandidate{}
	isPodcastFeed := strings.EqualFold(strings.TrimSpace(feed.Channel.PodcastMedium), "podcast")
	for i, item := range feed.Channel.Items {
		candidates = append(candidates, feedCandidate{Kind: "rss", RSSItem: item, Date: parseFeedDate(item.PubDate, item.Date), Index: i})
	}
	for i, entry := range feed.Entries {
		candidates = append(candidates, feedCandidate{Kind: "atom", AtomEntry: entry, Date: parseFeedDate(entry.Published, entry.Updated), Index: len(feed.Channel.Items) + i})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Date.IsZero() && candidates[j].Date.IsZero() {
			return candidates[i].Index < candidates[j].Index
		}
		if candidates[i].Date.IsZero() {
			return false
		}
		if candidates[j].Date.IsZero() {
			return true
		}
		return candidates[i].Date.After(candidates[j].Date)
	})
	if offset >= len(candidates) {
		return []article{}, nil
	}
	end := offset + limit
	if end > len(candidates) {
		end = len(candidates)
	}
	selected := candidates[offset:end]
	articles := make([]article, len(selected))
	parallelMap(len(selected), 6, func(i int) {
		candidate := selected[i]
		if candidate.Kind == "rss" {
			articles[i] = rssItemArticle(ctx, candidate.RSSItem, isPodcastFeed)
			return
		}
		entry := candidate.AtomEntry
		link := ""
		if len(entry.Link) > 0 {
			link = entry.Link[0].Href
		}
		body := entry.Content
		if body == "" {
			body = entry.Summary
		}
		a := article{Title: cleanText(entry.Title), Body: cleanText(stripArticleHTML(body)), Source: link, Images: extractImageURLs(body, link)}
		articles[i] = enrichArticleFromURL(ctx, a)
	})
	return articles, nil
}

func parallelMap(count, workers int, fn func(int)) {
	if count <= 0 {
		return
	}
	if workers < 1 {
		workers = 1
	}
	if workers > count {
		workers = count
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				fn(i)
			}
		}()
	}
	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func parseFeedDate(values ...string) time.Time {
	layouts := []string{time.RFC1123Z, time.RFC1123, time.RFC3339, time.RFC3339Nano, time.RFC822Z, time.RFC822, "Mon, 02 Jan 2006 15:04:05 -0700", "2006-01-02T15:04:05-07:00", "2006-01-02"}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, value); err == nil {
				return t
			}
		}
	}
	return time.Time{}
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
	return imagePromptJSON(map[string]any{
		"task": "Create the magazine cover.",
		"metadata": map[string]any{
			"publication":      title,
			"publication_type": magType,
			"page_role":        "cover",
			"language":         emptyDefault(style.Language, "English"),
			"format":           pageFormatInstruction(),
			"tone":             emptyDefault(style.Tone, "editorial"),
		},
		"style": map[string]any{
			"visual_brief": imageStyleBrief(style, "cover"),
		},
		"content": map[string]any{
			"masthead":     title,
			"requirements": "issue date, price/barcode or equivalent cover furniture, strong hierarchy",
			"cover_lines":  "story references and final page numbers are supplied at render time",
		},
		"constraints": []string{"full page visible", "no crop", "consistent print magazine design", "avoid " + style.Avoid},
	})
}

func articlePrompt(n int, title string, style magazineStyle, modules, kind string, a article, part, totalParts int) string {
	series := ""
	if totalParts > 1 {
		series = fmt.Sprintf("This is page %d of %d for this item. Continue the same story/feature without repeating the same layout.", part, totalParts)
	}
	return imagePromptJSON(map[string]any{
		"task": "Create a print magazine content page.",
		"metadata": map[string]any{
			"publication": title,
			"page_role":   kind,
			"language":    emptyDefault(style.Language, "English"),
			"format":      pageFormatInstruction(),
			"tone":        emptyDefault(style.Tone, "editorial"),
		},
		"style": map[string]any{
			"visual_brief": imageStyleBrief(style, kind),
		},
		"content": map[string]any{
			"title":       a.Title,
			"brief_body":  compact(a.Body, 1900),
			"series_note": series,
			"modules":     modules,
		},
		"layout": map[string]any{
			"required_elements": "headline, deck, byline/source if available, readable columns, image slots, captions, pull quote/sidebar where useful",
		},
		"constraints": []string{"full page visible", "no crop", "keep page furniture consistent across issue", "avoid " + style.Avoid},
	})
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
	return imagePromptJSON(map[string]any{
		"task": task,
		"metadata": map[string]any{
			"publication": title,
			"page_role":   kind,
			"language":    emptyDefault(style.Language, "English"),
			"format":      pageFormatInstruction(),
			"tone":        emptyDefault(style.Tone, "editorial"),
		},
		"style": map[string]any{
			"visual_brief": imageStyleBrief(style, kind),
		},
		"content": map[string]any{
			"module_ideas": modules,
		},
		"constraints": []string{"full page visible", "no crop", "fictional brands only unless supplied by article content", "avoid " + style.Avoid},
	})
}

func imageStyleBrief(style magazineStyle, kind string) string {
	return compact(strings.Join([]string{
		"Self-contained visual system for this page:",
		style.Core,
		style.Content,
		styleLineSpecific(style, kind),
		"Typography: " + style.Typography,
		"Palette: " + style.Color,
		"Print treatment: " + style.Print,
		"Page furniture: same margins, column grid, running-header placement, footer rule, folio placement and caption treatment on every page.",
		"Avoid: " + style.Avoid,
	}, " "), 1200)
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

func fallbackStyle(style, referencePath string) magazineStyle {
	base := emptyDefault(style, "A polished general-interest magazine with clear editorial hierarchy")
	if referencePath != "" {
		base += ". Use the reference image URL as visual inspiration for palette, texture, typography mood, and image treatment."
	}
	return magazineStyle{
		Name:       "Custom magazine",
		Language:   "English",
		Tone:       "clear, magazine-like, factual and polished",
		Core:       compact(base, 210),
		Cover:      "large masthead, confident cover lines, one dominant image, date/price/barcode if fitting",
		Content:    "consistent grid, clear folios, restrained page furniture, modular image and text rhythm",
		Feature:    "more generous opening image, pull quote, sidebar, longer headline and stronger hierarchy",
		Short:      "compact brief layout, small image, dense but readable columns, one small sidebar",
		Advert:     "fictional full-page advert using the same print world, distinct from editorial pages",
		Filler:     "departments, briefs, charts, reader notes, small classifieds and recurring modules",
		Back:       "closing advert, subscription panel, teaser or striking single visual",
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
	specific := styleLineSpecific(style, kind)
	return compact(strings.Join([]string{"Language: " + emptyDefault(style.Language, "English"), "Tone: " + emptyDefault(style.Tone, "editorial"), style.Core, style.Typography, style.Color, style.Print, specific, "Avoid: " + style.Avoid}, " "), 900)
}

func styleLineSpecific(style magazineStyle, kind string) string {
	switch kind {
	case "cover":
		return style.Cover
	case "feature":
		return style.Feature
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
	r := []rune(s)
	return string(r[:max])
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

func contextWithModels(ctx context.Context, textModel, imageModel string) context.Context {
	if textModel = strings.TrimSpace(textModel); textModel != "" {
		ctx = context.WithValue(ctx, textModelContextKey{}, textModel)
	}
	if imageModel = strings.TrimSpace(imageModel); imageModel != "" {
		ctx = context.WithValue(ctx, imageModelContextKey{}, imageModel)
	}
	return ctx
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
