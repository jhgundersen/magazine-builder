package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

func cryptoRandInt(max int) int {
	if max <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return int(time.Now().UnixNano() % int64(max))
	}
	return int(n.Int64())
}

func randomHex(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return fmt.Sprintf("%x", b)
}

var (
	tagRE                 = regexp.MustCompile(`<[^>]+>`)
	spaceRE               = regexp.MustCompile(`\s+`)
	imgTagRE              = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	attrRE                = regexp.MustCompile(`(?is)\s([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*["']([^"']*)["']`)
	removableBlockRE      = regexp.MustCompile(`(?is)<(?:script|style|noscript|svg|iframe|object|embed|form|nav|footer|aside)[^>]*>.*?</(?:script|style|noscript|svg|iframe|object|embed|form|nav|footer|aside)>`)
	commentRE             = regexp.MustCompile(`(?is)<!--.*?-->`)
	linkRE                = regexp.MustCompile(`(?is)<a\b[^>]*>(.*?)</a>`)
	urlTextRE             = regexp.MustCompile(`https?://\S+`)
	likelyArticleRE       = regexp.MustCompile(`(?is)<(?:article|main)\b[^>]*>(.*?)</(?:article|main)>`)
	contentBlockRE        = regexp.MustCompile(`(?is)<(?:div|section)\b[^>]*(?:class|id)=["'][^"']*(?:article|post|entry|content|story|body|main)[^"']*["'][^>]*>(.*?)</(?:div|section)>`)
	paragraphRE           = regexp.MustCompile(`(?is)<p\b[^>]*>.*?</p>`)
	titleRE               = regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</h1>`)
	transcriptTimestampRE = regexp.MustCompile(`^\d{1,2}:\d{2}(?::\d{2})?(?:[.,]\d{1,3})?\s*-->\s*\d{1,2}:\d{2}(?::\d{2})?(?:[.,]\d{1,3})?`)
	transcriptCueRE       = regexp.MustCompile(`^\d+$`)
)

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

// smartLimitImagePrompt reduces the image prompt to max runes, preferring to
// trim verbose JSON fields (style.visual_system, style.visual_brief,
// style.creative_kit) before
// falling back to a hard rune cut.
func smartLimitImagePrompt(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) == nil {
		style, _ := m["style"].(map[string]any)
		for _, field := range []string{"visual_system", "visual_brief", "creative_kit"} {
			if style == nil {
				break
			}
			val, ok := style[field].(string)
			if !ok {
				continue
			}
			excess := len([]rune(s)) - max
			newLen := len([]rune(val)) - excess - 20
			if newLen < 80 {
				newLen = 80
			}
			if newLen < len([]rune(val)) {
				style[field] = string([]rune(val)[:newLen])
				m["style"] = style
				if b, err := json.Marshal(m); err == nil {
					s = string(b)
					if len([]rune(s)) <= max {
						return s
					}
				}
			}
		}
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
