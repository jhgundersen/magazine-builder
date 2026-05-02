package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

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

type buildRequest struct {
	MagazineType string    `json:"magazineType"`
	Title        string    `json:"title"`
	Style        string    `json:"style"`
	StylePrompt  string    `json:"stylePrompt"`
	PageCount    int       `json:"pageCount"`
	Articles     []article `json:"articles"`
	Workspace    string    `json:"workspace"`
	APIKey       string    `json:"apiKey"`
	TextModel    string    `json:"textModel"`
	ImageModel   string    `json:"imageModel"`
}

type buildResponse struct {
	Style       magazineStyle `json:"style"`
	CreativeKit creativeKit   `json:"creativeKit"`
	BrandAssets []brandAsset  `json:"brandAssets,omitempty"`
	Articles    []article     `json:"articles"`
	Pages       []pagePlan    `json:"pages"`
	Issue       issueContext  `json:"issue"`
	Reference   string        `json:"reference,omitempty"`
	Workspace   string        `json:"workspace"`
}

type coverPlanRequest struct {
	Title     string        `json:"title"`
	Style     magazineStyle `json:"style"`
	Pages     []pagePlan    `json:"pages"`
	Issue     issueContext  `json:"issue"`
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

type renderPageRequest struct {
	Page           pagePlan      `json:"page"`
	Style          magazineStyle `json:"style"`
	Issue          issueContext  `json:"issue"`
	BrandAssets    []brandAsset  `json:"brandAssets"`
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

type pdfRequest struct {
	Images    []string `json:"images"`
	Title     string   `json:"title"`
	Workspace string   `json:"workspace"`
}

type pdfResponse struct {
	PDF string `json:"pdf"`
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
	inp := enhanceStyleInput{
		APIKey:        r.FormValue("apiKey"),
		TextModel:     r.FormValue("textModel"),
		Title:         strings.TrimSpace(r.FormValue("title")),
		Style:         strings.TrimSpace(r.FormValue("style")),
		Workspace:     r.FormValue("workspace"),
		ReferencePath: strings.TrimSpace(r.FormValue("reference")),
	}
	if inp.ReferencePath != "" && defapiImageRef(inp.ReferencePath) == "" {
		writeError(w, http.StatusBadRequest, errors.New("reference must be a public http(s) image URL"))
		return
	}
	workspace, err := s.ensureWorkspace(inp.Workspace, inp.Style)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	inp.Workspace = workspace
	db, err := s.openWorkspaceDB(workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	taskID := newTaskID()
	if err := createTask(db, "enhance-style", taskID, taskJSONOutput(inp)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	go s.runEnhanceStyleTask(db, workspace, taskID, inp)
	writeJSON(w, map[string]string{"taskId": taskID, "workspace": workspace})
}

func (s *server) runEnhanceStyleTask(db *sql.DB, workspace, taskID string, inp enhanceStyleInput) {
	ctx := context.Background()
	ctx = contextWithModels(contextWithAPIKey(ctx, inp.APIKey), inp.TextModel, "")
	ctx = contextWithWorkspace(ctx, workspace)
	if err := startTask(db, taskID); err != nil {
		log.Printf("runEnhanceStyleTask startTask: %v", err)
	}
	var taskErr error
	defer func() {
		if r := recover(); r != nil {
			taskErr = fmt.Errorf("panic: %v", r)
		}
		if taskErr != nil {
			taskLogErr("runEnhanceStyleTask failTask", failTask(db, taskID, taskErr.Error()))
		}
	}()
	enhanced, err := s.enhanceStyle(ctx, inp.Title, inp.Style, inp.ReferencePath)
	if err != nil {
		log.Printf("defapi text style enhancement failed: %v", err)
		s.workspaceLog(workspace, "style: enhancement failed: %v", err)
		enhanced = fallbackStyle(inp.Style, inp.ReferencePath)
	}
	if inp.Title != "" {
		enhanced.Name = inp.Title
	}
	styleJSON, _ := json.MarshalIndent(enhanced, "", "  ")
	s.workspaceLog(workspace, "style: source title=%q reference=%q user_style=%q", inp.Title, inp.ReferencePath, compact(inp.Style, 1200))
	s.workspaceLogJSON(workspace, "style: enhanced JSON", enhanced)
	taskErr = completeTask(db, taskID, taskJSONOutput(styleResponse{EnhancedStyle: string(styleJSON), Style: enhanced, ReferencePath: inp.ReferencePath, Workspace: workspace}))
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
	if req.Offset < 0 {
		req.Offset = 0
	}
	workspace, err := s.ensureWorkspace(req.Workspace, req.Style)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	db, err := s.openWorkspaceDB(workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	taskID := newTaskID()
	if err := createTask(db, "import-rss", taskID, taskJSONOutput(req)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	go s.runImportRSSTask(db, workspace, taskID, req)
	writeJSON(w, map[string]string{"taskId": taskID, "workspace": workspace})
}

func (s *server) runImportRSSTask(db *sql.DB, workspace, taskID string, req rssRequest) {
	ctx := context.Background()
	ctx = contextWithModels(contextWithAPIKey(ctx, req.APIKey), req.TextModel, "")
	ctx = contextWithWorkspace(ctx, workspace)
	if err := startTask(db, taskID); err != nil {
		log.Printf("runImportRSSTask startTask: %v", err)
	}
	var taskErr error
	defer func() {
		if r := recover(); r != nil {
			taskErr = fmt.Errorf("panic: %v", r)
		}
		if taskErr != nil {
			taskLogErr("runImportRSSTask failTask", failTask(db, taskID, taskErr.Error()))
		}
	}()
	s.workspaceLog(workspace, "rss-import: start url=%q offset=%d limit=%d", req.URL, req.Offset, req.Limit)
	articles, err := fetchRSS(ctx, req.URL, req.Offset, req.Limit)
	if err != nil {
		s.workspaceLog(workspace, "rss-import: fetch failed: %v", err)
		taskErr = err
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
	taskErr = completeTask(db, taskID, taskJSONOutput(map[string]any{"articles": articles, "workspace": workspace}))
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
	if req.Count <= 0 || req.Count > 12 {
		req.Count = 4
	}
	workspace, err := s.ensureWorkspace(req.Workspace, req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	db, err := s.openWorkspaceDB(workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	taskID := newTaskID()
	if err := createTask(db, "generate-articles", taskID, taskJSONOutput(req)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	go s.runGenerateArticlesTask(db, workspace, taskID, req)
	writeJSON(w, map[string]string{"taskId": taskID, "workspace": workspace})
}

func (s *server) runGenerateArticlesTask(db *sql.DB, workspace, taskID string, req generateArticlesRequest) {
	ctx := context.Background()
	ctx = contextWithModels(contextWithAPIKey(ctx, req.APIKey), req.TextModel, "")
	ctx = contextWithWorkspace(ctx, workspace)
	if err := startTask(db, taskID); err != nil {
		log.Printf("runGenerateArticlesTask startTask: %v", err)
	}
	var taskErr error
	defer func() {
		if r := recover(); r != nil {
			taskErr = fmt.Errorf("panic: %v", r)
		}
		if taskErr != nil {
			taskLogErr("runGenerateArticlesTask failTask", failTask(db, taskID, taskErr.Error()))
		}
	}()
	style := parseStyle(req.Style)
	articles, err := s.generateArticles(ctx, req.Title, style, req.Count)
	if err != nil {
		taskErr = err
		return
	}
	taskErr = completeTask(db, taskID, taskJSONOutput(map[string]any{"articles": articles, "workspace": workspace}))
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
	workspace, err := s.ensureWorkspace(req.Workspace, req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	db, err := s.openWorkspaceDB(workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	taskID := newTaskID()
	inputJSON := taskJSONOutput(req)
	if err := createTask(db, "build", taskID, inputJSON); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	go s.runBuildTask(db, workspace, taskID, req)
	writeJSON(w, map[string]string{"taskId": taskID, "workspace": workspace})
}

func (s *server) runBuildTask(db *sql.DB, workspace, taskID string, req buildRequest) {
	ctx := context.Background()
	ctx = contextWithModels(contextWithAPIKey(ctx, req.APIKey), req.TextModel, req.ImageModel)
	ctx = contextWithWorkspace(ctx, workspace)
	if err := startTask(db, taskID); err != nil {
		log.Printf("runBuildTask startTask: %v", err)
	}
	var taskErr error
	defer func() {
		if r := recover(); r != nil {
			taskErr = fmt.Errorf("panic: %v", r)
		}
		if taskErr != nil {
			taskLogErr("runBuildTask failTask", failTask(db, taskID, taskErr.Error()))
		}
	}()
	style := parseStyle(req.Style)
	total := 3
	for _, a := range req.Articles {
		if !a.Enhanced {
			total++
		}
	}
	done := 0
	s.taskProgress(db, taskID, done, total, "Starting defapi text work")
	s.workspaceLog(workspace, "build: start title=%q articles=%d pages=%d", req.Title, len(req.Articles), req.PageCount)
	s.workspaceLogJSON(workspace, "build: style JSON", style)
	s.workspaceLogJSON(workspace, "build: input articles", articleLogEntries(req.Articles, false))
	s.taskProgress(db, taskID, done, total, "Choosing issue number and date")
	issue, err := s.generateIssueContext(ctx, req, style)
	if err != nil {
		log.Printf("defapi text issue context failed: %v", err)
		s.workspaceLog(workspace, "build: issue context failed: %v", err)
		issue = newIssueContext(time.Now())
	}
	s.workspaceLogJSON(workspace, "build: issue JSON", issue)
	done++
	for i := range req.Articles {
		if req.Articles[i].Enhanced {
			continue
		}
		s.taskProgress(db, taskID, done, total, fmt.Sprintf("Rewriting %q", emptyDefault(req.Articles[i].Title, "Untitled")))
		var improved article
		if req.Articles[i].Kind == "feature" {
			improved, err = s.rewriteFeatureForStyle(ctx, req.Articles[i], style)
		} else {
			improved, err = s.rewriteManualArticleForStyle(ctx, req.Articles[i], style)
		}
		if err != nil {
			log.Printf("defapi text manual article rewrite failed for %q: %v", req.Articles[i].Title, err)
			s.workspaceLog(workspace, "build: rewrite failed index=%d title=%q error=%v", i, req.Articles[i].Title, err)
			done++
			s.taskProgress(db, taskID, done, total, fmt.Sprintf("Rewrite failed for %q", emptyDefault(req.Articles[i].Title, "Untitled")))
			continue
		}
		improved.Enhanced = true
		req.Articles[i] = improved
		s.workspaceLogJSON(workspace, fmt.Sprintf("build: rewritten article index=%d", i), articleLogEntryFromArticle(i, req.Articles[i], true))
		done++
		s.taskProgress(db, taskID, done, total, fmt.Sprintf("Rewritten %q", emptyDefault(req.Articles[i].Title, "Untitled")))
	}
	s.taskProgress(db, taskID, done, total, "Generating creative kit")
	kit, err := s.generateCreativeKit(ctx, req, style, issue)
	if err != nil {
		log.Printf("defapi text creative kit failed: %v", err)
		s.workspaceLog(workspace, "build: creative kit failed: %v", err)
		kit = fallbackCreativeKit(req)
	}
	s.workspaceLogJSON(workspace, "build: final articles", articleLogEntries(req.Articles, true))
	s.workspaceLogJSON(workspace, "build: creative kit JSON", kit)
	done++
	s.taskProgress(db, taskID, done, total, "Generating brand assets")
	brandAssets, err := s.generateBrandAssets(ctx, workspace, req, style, issue)
	if err != nil {
		log.Printf("defapi image brand assets failed: %v", err)
		s.workspaceLog(workspace, "build: brand assets failed: %v", err)
	}
	s.workspaceLogJSON(workspace, "build: brand assets JSON", brandAssets)
	done++
	s.taskProgress(db, taskID, done, total, "Plan ready")
	s.workspaceLog(workspace, "build: complete")
	result := buildResponse{Style: style, CreativeKit: kit, BrandAssets: brandAssets, Articles: req.Articles, Pages: planMagazine(req, style, kit, issue), Issue: issue, Workspace: workspace}
	taskErr = completeTask(db, taskID, taskJSONOutput(result))
}

func (s *server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	workspace := sanitizeWorkspace(r.URL.Query().Get("workspace"))
	taskID := strings.TrimSpace(r.URL.Query().Get("id"))
	if workspace == "" || taskID == "" {
		writeError(w, http.StatusBadRequest, errors.New("workspace and id required"))
		return
	}
	db, err := s.openWorkspaceDB(workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	t, err := getTask(db, taskID)
	if errIsNotFound(err) {
		writeError(w, http.StatusNotFound, errors.New("task not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, t)
}

func (s *server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	workspace := sanitizeWorkspace(r.URL.Query().Get("workspace"))
	if workspace == "" {
		writeError(w, http.StatusBadRequest, errors.New("workspace required"))
		return
	}
	db, err := s.openWorkspaceDB(workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	tasks, err := listTasks(db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	st, err := getAllState(db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if tasks == nil {
		tasks = []task{}
	}
	writeJSON(w, map[string]any{"tasks": tasks, "state": st})
}

func (s *server) handleSetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Workspace string `json:"workspace"`
		Key       string `json:"key"`
		Value     string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspace := sanitizeWorkspace(req.Workspace)
	if workspace == "" || req.Key == "" {
		writeError(w, http.StatusBadRequest, errors.New("workspace and key required"))
		return
	}
	db, err := s.openWorkspaceDB(workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := setState(db, req.Key, req.Value); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]string{"ok": "true"})
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
	workspace, err := s.ensureWorkspace(req.Workspace, req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	db, err := s.openWorkspaceDB(workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	taskID := newTaskID()
	if err := createTask(db, "cover-plan", taskID, taskJSONOutput(req)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	go s.runCoverPlanTask(db, workspace, taskID, req)
	writeJSON(w, map[string]string{"taskId": taskID, "workspace": workspace})
}

func (s *server) runCoverPlanTask(db *sql.DB, workspace, taskID string, req coverPlanRequest) {
	ctx := context.Background()
	ctx = contextWithModels(contextWithAPIKey(ctx, req.APIKey), req.TextModel, "")
	ctx = contextWithWorkspace(ctx, workspace)
	if err := startTask(db, taskID); err != nil {
		log.Printf("runCoverPlanTask startTask: %v", err)
	}
	var taskErr error
	defer func() {
		if r := recover(); r != nil {
			taskErr = fmt.Errorf("panic: %v", r)
		}
		if taskErr != nil {
			taskLogErr("runCoverPlanTask failTask", failTask(db, taskID, taskErr.Error()))
		}
	}()
	s.workspaceLog(workspace, "cover-plan: start pages=%d", len(req.Pages))
	issue := normalizeIssueContext(req.Issue, time.Now())
	plan, err := s.generateCoverPlan(ctx, req.Title, req.Style, req.Pages, issue)
	if err != nil {
		s.workspaceLog(workspace, "cover-plan: failed: %v", err)
		plan = fallbackCoverPlan(req.Style, req.Pages)
	}
	taskErr = completeTask(db, taskID, taskJSONOutput(map[string]any{"coverPlan": plan, "workspace": workspace}))
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
	workspace, err := s.ensureWorkspace(req.Workspace, req.Page.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	db, err := s.openWorkspaceDB(workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	taskID := newTaskID()
	if err := createTask(db, "render-page", taskID, taskJSONOutput(req)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	go s.runRenderPageTask(db, workspace, taskID, req)
	writeJSON(w, map[string]string{"taskId": taskID, "workspace": workspace})
}

func (s *server) runRenderPageTask(db *sql.DB, workspace, taskID string, req renderPageRequest) {
	ctx := context.Background()
	ctx = contextWithModels(contextWithAPIKey(ctx, req.APIKey), req.TextModel, req.ImageModel)
	if err := startTask(db, taskID); err != nil {
		log.Printf("runRenderPageTask startTask: %v", err)
	}
	var taskErr error
	defer func() {
		if r := recover(); r != nil {
			taskErr = fmt.Errorf("panic: %v", r)
		}
		if taskErr != nil {
			taskLogErr("runRenderPageTask failTask", failTask(db, taskID, taskErr.Error()))
		}
	}()
	images := filterStrings([]string{req.StyleReference, req.Reference})
	images = append(images, brandAssetRefsForPage(req.Page, req.BrandAssets)...)
	images = append(images, req.Page.Images...)
	issue := normalizeIssueContext(req.Issue, time.Now())
	req.Page.Prompt = s.pagePromptWithFurniture(ctx, req.Style, req.Page, issue)
	s.workspaceLog(workspace, "render-page: start page=%d title=%q refs=%d", req.Page.Number, req.Page.Title, len(images))
	image, err := s.runDefapiImageWithRetry(ctx, workspace, req.Page.Number, smartLimitImagePrompt(req.Page.Prompt, s.cfg.DefapiImageMaxPromptChars), images)
	if err != nil {
		s.workspaceLog(workspace, "render-page: failed page=%d: %v", req.Page.Number, err)
		taskErr = err
		return
	}
	s.workspaceLog(workspace, "render-page: complete page=%d image=%s public=%s", req.Page.Number, image.Image, image.PublicURL)
	taskErr = completeTask(db, taskID, taskJSONOutput(renderPageResponse{Image: image.Image, PublicURL: image.PublicURL}))
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
