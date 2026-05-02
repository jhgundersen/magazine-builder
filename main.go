package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	repositoryName = "jhgundersen/magazine-builder"
	binaryName     = "magazine-builder"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
// It defaults to "dev" which disables self-update.
var version = "dev"

//go:embed static/*
var embeddedStatic embed.FS

type config struct {
	Addr                      string
	WorkDir                   string
	WorkspaceMaxAge           time.Duration
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
	cfg config
	dbs sync.Map
}

type apiKeyContextKey struct{}
type textModelContextKey struct{}
type imageModelContextKey struct{}
type workspaceContextKey struct{}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			println(binaryName, version)
			return
		case "update":
			if version == "dev" {
				log.Fatal("update: not available in dev builds")
			}
			if err := runUpdate(os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		}
	}
	cfg := parseFlags()
	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		log.Fatal(err)
	}
	s := &server{cfg: cfg}
	s.startupCleanup()
	if cfg.WorkspaceMaxAge > 0 {
		go s.cleanupLoop()
	}
	if version != "dev" {
		go s.autoUpdateLoop()
	}
	handler, err := s.routes()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("magazine-builder %s listening on http://localhost%s", version, cfg.Addr)
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
	mux.HandleFunc("/api/brand-assets", s.handleBrandAssets)
	mux.HandleFunc("/api/cover-plan", s.handleCoverPlan)
	mux.HandleFunc("/api/render-page", s.handleRenderPage)
	mux.HandleFunc("/api/write-pdf", s.handleWritePDF)
	mux.HandleFunc("/api/task", s.handleGetTask)
	mux.HandleFunc("/api/tasks", s.handleListTasks)
	mux.HandleFunc("/api/workspace-state", s.handleSetState)
	mux.Handle("/static/", http.StripPrefix("/static/", noCache(http.FileServer(http.FS(staticFS)))))
	mux.Handle("/work/", http.StripPrefix("/work/", http.FileServer(http.Dir(s.cfg.WorkDir))))
	return mux, nil
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.Addr, "addr", ":8080", "HTTP listen address")
	flag.StringVar(&cfg.WorkDir, "workdir", "magazine-work", "directory for uploads and generated artifacts")
	flag.DurationVar(&cfg.WorkspaceMaxAge, "workspace-max-age", 48*time.Hour, "delete workspaces older than this (0 disables)")
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
	flag.IntVar(&cfg.DefapiImageMaxPromptChars, "defapi-image-max-prompt-chars", 3990, "maximum defapi image prompt length")
	flag.IntVar(&cfg.DefapiImageRetries, "defapi-image-retries", 2, "retry attempts for failed defapi image calls")
	flag.Parse()
	return cfg
}
