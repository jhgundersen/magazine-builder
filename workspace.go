package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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

func (s *server) workspaceDir(workspace string) string {
	return filepath.Join(s.cfg.WorkDir, sanitizeWorkspace(workspace))
}

func (s *server) workspaceURL(workspace, rel string) string {
	return "/work/" + sanitizeWorkspace(workspace) + "/" + strings.TrimLeft(rel, "/")
}

func (s *server) cleanupLoop() {
	for {
		s.cleanupWorkspaces()
		time.Sleep(time.Hour)
	}
}

func (s *server) autoUpdateLoop() {
	time.Sleep(5 * time.Minute)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		tag, err := latestReleaseTag(ctx, repositoryName)
		cancel()
		if err == nil && tag != "" && tag != version {
			log.Printf("auto-update: new version available: %s (running %s) — run 'magazine-builder update' to upgrade", tag, version)
		}
		time.Sleep(24 * time.Hour)
	}
}

func (s *server) cleanupWorkspaces() {
	entries, err := os.ReadDir(s.cfg.WorkDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-s.cfg.WorkspaceMaxAge)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			workspace := e.Name()
			dir := filepath.Join(s.cfg.WorkDir, workspace)
			if v, ok := s.dbs.LoadAndDelete(workspace); ok {
				_ = v.(*sql.DB).Close()
			}
			if err := os.RemoveAll(dir); err == nil {
				log.Printf("cleanup: removed workspace %s (age %s)", workspace, time.Since(info.ModTime()).Round(time.Minute))
			}
		}
	}
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
