package rss

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rdslw/blogwatcher/internal/sitehttp"
)

const sampleFeed = `<?xml version="1.0" encoding="UTF-8" ?>
<rss version="2.0">
<channel>
<title>Example Feed</title>
<item>
<title>First</title>
<link>https://example.com/1</link>
<description>First article summary</description>
<pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
</item>
<item>
<title>Second</title>
<link>https://example.com/2</link>
</item>
</channel>
</rss>`

func TestParseFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != sitehttp.UserAgent() {
			t.Fatalf("expected user agent %q, got %q", sitehttp.UserAgent(), got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer server.Close()

	articles, err := ParseFeed(server.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("parse feed: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected 2 articles, got %d", len(articles))
	}
	if articles[0].PublishedDate == nil {
		t.Fatalf("expected published date")
	}
}

func TestDiscoverFeedURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != sitehttp.UserAgent() {
			t.Fatalf("expected user agent %q, got %q", sitehttp.UserAgent(), got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><head><link rel="alternate" type="application/rss+xml" href="/feed.xml" /></head></html>`))
	})
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleFeed))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	feedURL, err := DiscoverFeedURL(server.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("discover feed: %v", err)
	}
	if feedURL == "" {
		t.Fatalf("expected feed url")
	}
}

func TestDiscoverFeedURLContentType(t *testing.T) {
	for _, test := range []struct {
		contentType string
		wantFeed    bool
	}{
		{"application/rss+xml; charset=UTF-8", true},
		{"Application/Atom+XML; charset=utf-8", true},
		{"application/feed+json", true},
		{"application/xml", false},
		{"text/xml", false},
		{"application/rss+xml; broken", false},
	} {
		t.Run(test.contentType, func(t *testing.T) {
			const userAgent = "blogwatcher-discovery-test"
			mux := http.NewServeMux()
			mux.HandleFunc("/tag/AI/feed/", func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("User-Agent"); got != userAgent {
					t.Errorf("expected user agent %q, got %q", userAgent, got)
				}
				w.Header().Set("Content-Type", test.contentType)
				_, _ = w.Write([]byte(`<root/>`))
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			url := server.URL + "/tag/AI/feed/"
			got, err := DiscoverFeedURLWithUserAgent(url, 2*time.Second, userAgent)
			if err != nil {
				t.Fatalf("discover feed: %v", err)
			}
			want := ""
			if test.wantFeed {
				want = url
			}
			if got != want {
				t.Fatalf("expected feed URL %q, got %q", want, got)
			}
		})
	}
}

func TestDiscoverFeedURLRelTokens(t *testing.T) {
	for _, rel := range []string{"self", "self bookmark", "alternate bookmark"} {
		t.Run(rel, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = fmt.Fprintf(w, `<html><head><link rel="%s" type="application/rss+xml" href="/my-feed.xml" /></head></html>`, rel)
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			got, err := DiscoverFeedURL(server.URL, 2*time.Second)
			if err != nil {
				t.Fatalf("discover feed: %v", err)
			}
			if want := server.URL + "/my-feed.xml"; got != want {
				t.Fatalf("expected feed URL %q, got %q", want, got)
			}
		})
	}
}

func TestIsValidFeedUsesBlogwatcherUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != sitehttp.UserAgent() {
			t.Fatalf("expected user agent %q, got %q", sitehttp.UserAgent(), got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer server.Close()

	ok, err := isValidFeed(server.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("validate feed: %v", err)
	}
	if !ok {
		t.Fatal("expected valid feed")
	}
}

func TestParseFeedExtractsDescription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer server.Close()

	articles, err := ParseFeed(server.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("parse feed: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected 2 articles, got %d", len(articles))
	}
	if articles[0].Description != "First article summary" {
		t.Fatalf("expected description, got %q", articles[0].Description)
	}
	if articles[1].Description != "" {
		t.Fatalf("expected empty description for second article, got %q", articles[1].Description)
	}
}

func TestParseFeedReportsReadTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8" ?><rss version="2.0"><channel>`))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer server.Close()

	_, err := ParseFeed(server.URL, 10*time.Millisecond)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(err.Error(), "feed read timeout after 10ms while parsing") {
		t.Fatalf("expected read timeout wording, got %q", err.Error())
	}
}
