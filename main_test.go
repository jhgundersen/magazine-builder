package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractLikelyArticleRemovesEmbedsLinksAndFindsImages(t *testing.T) {
	html := `<html><body>
<nav>menu noise</nav>
<article>
<h1>Original Web Title</h1>
<p>First real paragraph with <a href="https://external.example/x">linked words</a>.</p>
<iframe src="https://youtube.com/embed/x"></iframe>
<p>Second real paragraph with https://example.com/noise removed.</p>
<img src="/image.jpg"><img srcset="/large.jpg 1200w, /small.jpg 600w">
</article>
<footer>footer noise</footer>
</body></html>`
	extracted := extractLikelyArticle(html)
	if extracted.Title != "Original Web Title" {
		t.Fatalf("unexpected title: %q", extracted.Title)
	}
	for _, bad := range []string{"youtube", "https://example.com/noise", "menu noise", "footer noise"} {
		if strings.Contains(strings.ToLower(extracted.Body), bad) {
			t.Fatalf("body kept %q: %s", bad, extracted.Body)
		}
	}
	if !strings.Contains(extracted.Body, "linked words") || !strings.Contains(extracted.Body, "Second real paragraph") {
		t.Fatalf("body missed article text: %s", extracted.Body)
	}
	images := extractImageURLs(extracted.Markup, "https://example.com/posts/story")
	if len(images) != 2 || images[0] != "https://example.com/image.jpg" || images[1] != "https://example.com/large.jpg" {
		t.Fatalf("unexpected images: %#v", images)
	}
}

func TestImagegenImageRefOnlyAllowsPublicURLs(t *testing.T) {
	if got := imagegenImageRef("https://example.com/page.jpg"); got != "https://example.com/page.jpg" {
		t.Fatalf("unexpected public ref: %q", got)
	}
	if got := imagegenImageRef("/renders/page.jpg"); got != "" {
		t.Fatalf("local render path should not be passed to imagegen: %q", got)
	}
}

func TestParseImageURL(t *testing.T) {
	output := "Task submitted: x\nPolling... done.\nImage URL: https://aisaas.nots.top/tmp/page.png\nSaved to: /tmp/page.jpg\n"
	if got := parseImageURL(output); got != "https://aisaas.nots.top/tmp/page.png" {
		t.Fatalf("parseImageURL() = %q", got)
	}
}

func TestPromptsIncludePageSideAndFolioSide(t *testing.T) {
	style := fallbackStyle("quiet magazine", "")
	got := articlePrompt(2, "Test", style, "quote box; timeline", "article", article{Title: "A", Body: "Body"}, 1, 1)
	if strings.Contains(got, "left-hand page") || strings.Contains(got, "Put page number 2") {
		t.Fatalf("editable article prompt should not include final page placement: %s", got)
	}
	got = genericPrompt(3, "Test", style, "reader mail; chart", "filler", "Task")
	if strings.Contains(got, "right-hand page") || strings.Contains(got, "Put page number 3") {
		t.Fatalf("editable generic prompt should not include final page placement: %s", got)
	}
}

func TestJPEGDataReencodesPNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, cfg, err := jpegData(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 4 || cfg.Height != 4 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		t.Fatalf("expected JPEG bytes, got prefix %#v", data[:2])
	}
}
