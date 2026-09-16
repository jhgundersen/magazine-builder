package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Exercise the actual subprocess boundary without calling a paid AI service.
func promptTestServer(t *testing.T, response string) (*server, func() string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test fixture requires a POSIX shell")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "defapi")
	for name, data := range map[string]string{
		script:                         "#!/bin/sh\ndir=$(dirname \"$0\")\ncat > \"$dir/prompt\"\ncat \"$dir/response\"\n",
		filepath.Join(dir, "response"): response,
	} {
		if err := os.WriteFile(name, []byte(data), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return &server{cfg: config{DefapiTextCmd: script, DefapiTextTimeout: time.Second * 10}}, func() string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "prompt"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
}

func TestGeneratedAndImportedCopyUseStyleLength(t *testing.T) {
	style := magazineStyle{ArticleLength: "220-650 chars; short comic panel beats"}
	for _, kind := range []string{"generated", "feature", "imported"} {
		t.Run(kind, func(t *testing.T) {
			s, readPrompt := promptTestServer(t, `{"articles":[{"title":"New","body":"Copy"}],"title":"New","body":"Copy"}`)
			var err error
			switch kind {
			case "generated":
				_, err = s.generateArticles(context.Background(), "Comic", style, 1)
			case "feature":
				_, err = s.rewriteFeatureForStyle(context.Background(), article{Title: "Quiz", Body: "Questions"}, style)
			case "imported":
				_, err = s.rewriteArticleForStyle(context.Background(), article{Title: "News", Body: "Source"}, style)
			}
			if err != nil {
				t.Fatal(err)
			}
			prompt := readPrompt()
			if !strings.Contains(prompt, "220-650") || !strings.Contains(prompt, "short comic panel beats") {
				t.Fatalf("style guidance missing: %s", prompt)
			}
			for _, bad := range []string{"900-650", "900-1500", "700-1400"} {
				if strings.Contains(prompt, bad) {
					t.Fatalf("conflicting length %s", bad)
				}
			}
		})
	}
}

func TestMultiPageRewriteRejectsIncompleteSections(t *testing.T) {
	original := article{Title: "Original", Body: "Source facts", Pages: 2}
	for _, response := range []string{
		`{"title":"Changed","sections":[{"body":"One","image_brief":"Scene"}]}`,
		`{"title":"Changed","sections":[{"body":"One","image_brief":"Scene"},{"body":"Two","image_brief":""}]}`,
	} {
		s, _ := promptTestServer(t, response)
		got, err := s.rewriteArticleForStyle(context.Background(), original, magazineStyle{ArticleLength: "220-650 chars; panel beats"})
		if err == nil || !reflect.DeepEqual(got, original) {
			t.Fatalf("invalid response must preserve original article: %#v, %v", got, err)
		}
	}
}

func TestMultiPageRewriteUsesPerPageRangeAndSourceFidelity(t *testing.T) {
	s, readPrompt := promptTestServer(t, `{"title":"Story","sections":[{"body":"One","image_brief":"Same character opens door"},{"body":"Two","image_brief":"Same character closes door"}]}`)
	got, err := s.rewriteArticleForStyle(context.Background(), article{Body: "Facts", Pages: 2}, magazineStyle{ArticleLength: "220-650 chars; panel beats"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enhanced || len(got.Sections) != 2 {
		t.Fatalf("valid sections not accepted: %#v", got)
	}
	prompt := readPrompt()
	for _, want := range []string{"Body length guidance per page: 220-650", "Preserve source facts", "Do not invent quotes", "each page is rendered independently"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q: %s", want, prompt)
		}
	}
}
