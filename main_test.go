package main

import (
	"context"
	"encoding/xml"
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

func TestDefapiImageRefOnlyAllowsPublicURLs(t *testing.T) {
	if got := defapiImageRef("https://example.com/page.jpg"); got != "https://example.com/page.jpg" {
		t.Fatalf("unexpected public ref: %q", got)
	}
	if got := defapiImageRef("https://example.com/full/561,/0/default.jpg"); got != "https://example.com/full/561%2C/0/default.jpg" {
		t.Fatalf("comma should be escaped before defapi receives the URL: %q", got)
	}
	if got := defapiImageRef("/renders/page.jpg"); got != "" {
		t.Fatalf("local render path should not be passed to defapi image: %q", got)
	}
}

func TestParseImageURL(t *testing.T) {
	output := "Task submitted: x\nPolling... done.\nImage URL: https://aisaas.nots.top/tmp/page.png\nSaved to: /tmp/page.jpg\n"
	if got := parseImageURL(output); got != "https://aisaas.nots.top/tmp/page.png" {
		t.Fatalf("parseImageURL() = %q", got)
	}
}

func TestFetchRSSPodcastUsesTranscriptAndMetadata(t *testing.T) {
	oldFetch := fetchURLTextFunc
	fetchURLTextFunc = func(_ context.Context, rawURL string, _ int64) (string, error) {
		switch rawURL {
		case "https://example.com/transcript.vtt":
			return "WEBVTT\n\n1\n00:00:00.000 --> 00:00:03.000\nTranscript line one.\n\n00:00:03.000 --> 00:00:05.000\nTranscript line two.", nil
		case "https://example.com/chapters.json":
			return `{"chapters":[{"title":"Intro"},{"title":"Deep dive"}]}`, nil
		default:
			t.Fatalf("unexpected fetch: %s", rawURL)
			return "", nil
		}
	}
	defer func() { fetchURLTextFunc = oldFetch }()

	var feed rssFeed
	if err := xml.Unmarshal([]byte(`<?xml version="1.0"?>
<rss xmlns:podcast="https://podcastindex.org/namespace/1.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" version="2.0">
<channel>
<podcast:medium>podcast</podcast:medium>
<item>
<title>Episode Title</title>
<link>https://example.com/article</link>
<description><![CDATA[<p>Short podcast description.</p>]]></description>
<enclosure url="https://example.com/episode.mp3" type="audio/mpeg"/>
<podcast:transcript url="https://example.com/transcript.vtt" type="text/vtt" language="no"/>
<podcast:chapters url="https://example.com/chapters.json" type="application/json"/>
<podcast:season name="Spring">13</podcast:season>
<podcast:episode display="8 (#131)">131</podcast:episode>
</item>
</channel>
</rss>`), &feed); err != nil {
		t.Fatal(err)
	}
	if len(feed.Channel.Items) != 1 {
		t.Fatalf("expected one item, got %d", len(feed.Channel.Items))
	}
	a := rssItemArticle(context.Background(), feed.Channel.Items[0], true)
	body := a.Body
	if a.Kind != "podcast" {
		t.Fatalf("expected podcast kind, got %q", a.Kind)
	}
	for _, want := range []string{"Short podcast description.", "Transcript line one.", "Transcript line two.", "Season: Spring (13)", "Episode: 8 (#131)", "Chapters: Intro; Deep dive"} {
		if !strings.Contains(body, want) {
			t.Fatalf("podcast body missing %q: %s", want, body)
		}
	}
	if a.Source != "https://example.com/article" {
		t.Fatalf("unexpected source: %q", a.Source)
	}
}

func TestSampleLongTextUsesHeadMiddleAndTail(t *testing.T) {
	got := sampleLongText(strings.Repeat("A", 2000)+" "+strings.Repeat("B", 2000)+" "+strings.Repeat("C", 2000), 1200)
	for _, want := range []string{"AAA", "[Middle excerpt]", "BBB", "[Final excerpt]", "CCC"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sample missing %q: %s", want, got)
		}
	}
}

func TestCommandArgsSkipsEmptyCategory(t *testing.T) {
	got := strings.Join(commandArgs("text", "claude", "-max-tokens", "100"), " ")
	if got != "text claude -max-tokens 100" {
		t.Fatalf("unexpected defapi args: %q", got)
	}
	got = strings.Join(commandArgs("", "claude", "-max-tokens", "100"), " ")
	if got != "claude -max-tokens 100" {
		t.Fatalf("unexpected legacy args: %q", got)
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

func TestPlanKeepsMultiPageArticleContiguousAcrossAdvertSlot(t *testing.T) {
	req := buildRequest{
		Title:     "Test",
		PageCount: 8,
		Articles:  []article{{Title: "Long item", Body: "Body", Pages: 3}},
	}
	pages := planMagazine(req, fallbackStyle("quiet", ""), fallbackCreativeKit(req))
	got := []string{pages[1].Title, pages[2].Title, pages[3].Title}
	for i, title := range got {
		if title != "Long item" {
			t.Fatalf("page %d should continue article, got %q in sequence %#v", i+2, title, got)
		}
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
