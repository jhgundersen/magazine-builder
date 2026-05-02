package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

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

type extractedArticle struct {
	Title  string
	Body   string
	Markup string
}

type imageCandidate struct {
	URL   string
	Score int
	Order int
}

var fetchURLTextFunc = fetchURLText

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

func extractImageURLs(markup, base string) []string {
	markup = cleanArticleHTML(markup)
	seen := map[string]int{}
	candidates := []imageCandidate{}
	add := func(raw string, score int) {
		u := resolveURL(base, strings.TrimSpace(raw))
		if u == "" {
			return
		}
		if i, ok := seen[u]; ok {
			if score > candidates[i].Score {
				candidates[i].Score = score
			}
			return
		}
		seen[u] = len(candidates)
		candidates = append(candidates, imageCandidate{URL: u, Score: score, Order: len(candidates)})
	}

	for _, tag := range imgTagRE.FindAllString(markup, -1) {
		attrs := imageTagAttrs(tag)
		score := imageAttrScore(attrs)
		for _, name := range []string{"src", "data-src", "data-original"} {
			if attrs[name] != "" {
				add(attrs[name], score)
				break
			}
		}
		for _, candidate := range srcsetCandidates(attrs["srcset"]) {
			candidate.Score = max(candidate.Score, score)
			add(candidate.URL, candidate.Score)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Order < candidates[j].Order
		}
		return candidates[i].Score > candidates[j].Score
	})
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.URL)
	}
	return out
}

func imageTagAttrs(tag string) map[string]string {
	attrs := map[string]string{}
	for _, m := range attrRE.FindAllStringSubmatch(tag, -1) {
		if len(m) > 2 {
			attrs[strings.ToLower(m[1])] = strings.TrimSpace(m[2])
		}
	}
	return attrs
}

func imageAttrScore(attrs map[string]string) int {
	width := firstPositiveInt(attrs["width"], attrs["data-width"])
	height := firstPositiveInt(attrs["height"], attrs["data-height"])
	return max(width, height)
}

func srcsetCandidates(srcset string) []imageCandidate {
	parts := strings.Split(srcset, ",")
	out := make([]imageCandidate, 0, len(parts))
	for i, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		score := 0
		if len(fields) > 1 {
			score = srcsetDescriptorScore(fields[1])
		}
		out = append(out, imageCandidate{URL: fields[0], Score: score, Order: i})
	}
	return out
}

func srcsetDescriptorScore(desc string) int {
	desc = strings.TrimSpace(strings.ToLower(desc))
	if strings.HasSuffix(desc, "w") {
		return firstPositiveInt(strings.TrimSuffix(desc, "w"))
	}
	if strings.HasSuffix(desc, "x") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(desc, "x"), 64)
		if err == nil && v > 0 {
			return int(v * 1000)
		}
	}
	return 0
}

func firstPositiveInt(values ...string) int {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		n, err := strconv.Atoi(value)
		if err == nil && n > 0 {
			return n
		}
	}
	return 0
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
	candidates := srcsetCandidates(srcset)
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Order < candidates[j].Order
		}
		return candidates[i].Score > candidates[j].Score
	})
	return candidates[0].URL
}
