package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

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
	current := comparableVersion(version)
	if current != "" && tag == current {
		fmt.Printf("Already up to date (%s)\n", version)
		return nil
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", opts.Repo, tag, asset)
	fmt.Printf("Updating %s → %s...\n", version, tag)
	if err := downloadAndReplace(ctx, url, target); err != nil {
		return err
	}
	fmt.Printf("Updated to %s — restart to apply\n", tag)
	return nil
}

func comparableVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "dev" {
		return v
	}
	if i := strings.IndexAny(v, "+ "); i >= 0 {
		v = v[:i]
	}
	for _, suffix := range []string{"-dirty"} {
		v = strings.TrimSuffix(v, suffix)
	}
	return v
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
