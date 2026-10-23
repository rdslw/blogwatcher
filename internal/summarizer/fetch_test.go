package summarizer

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rdslw/blogwatcher/internal/config"
)

const jinaTestKey = "jina-test-key"

func newArticleServer(t *testing.T, status int, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_, _ = io.WriteString(w, "<html><body><article><p>Direct body text.</p></article></body></html>")
	}))
	t.Cleanup(server.Close)
	return server
}

func newJinaServer(t *testing.T, status int, body string, hits *atomic.Int32) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer "+jinaTestKey {
			t.Errorf("unexpected Authorization header %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("unexpected Accept header %q", got)
		}
		if !strings.HasPrefix(r.URL.Path, "/http://") {
			t.Errorf("expected article URL in path, got %q", r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	original := jinaReaderURL
	jinaReaderURL = server.URL + "/"
	t.Cleanup(func() { jinaReaderURL = original })
}

const jinaOKBody = `{"code":200,"status":20000,"data":{"title":"T","content":"## Heading\n\n  Jina body text.  \n\n","httpStatus":200}}`

func jinaOptions(mode string) Options {
	return Options{JinaAPIKey: jinaTestKey, JinaFetch: mode}
}

func TestFetchArticleFallbackUsesJinaAfterHTTPFailure(t *testing.T) {
	var siteHits, jinaHits atomic.Int32
	site := newArticleServer(t, http.StatusForbidden, &siteHits)
	newJinaServer(t, http.StatusOK, jinaOKBody, &jinaHits)

	text, source, note, err := fetchArticle(site.URL, jinaOptions(config.JinaFetchFallback))
	if err != nil {
		t.Fatalf("fetch article: %v", err)
	}
	if source != FetchSourceJina {
		t.Fatalf("expected source %q, got %q", FetchSourceJina, source)
	}
	if text != "## Heading\nJina body text." {
		t.Fatalf("unexpected text %q", text)
	}
	if !strings.Contains(note, "http fetch failed") || !strings.Contains(note, "status 403") || !strings.HasSuffix(note, "fetched via jina.") {
		t.Fatalf("expected note with HTTP failure reason, got %q", note)
	}
	if siteHits.Load() != 1 || jinaHits.Load() != 1 {
		t.Fatalf("expected one request per stage, got site=%d jina=%d", siteHits.Load(), jinaHits.Load())
	}
}

func TestFetchArticleFallbackSkipsJinaAfterHTTPSuccess(t *testing.T) {
	var siteHits, jinaHits atomic.Int32
	site := newArticleServer(t, http.StatusOK, &siteHits)
	newJinaServer(t, http.StatusOK, jinaOKBody, &jinaHits)

	text, source, note, err := fetchArticle(site.URL, jinaOptions(config.JinaFetchFallback))
	if err != nil {
		t.Fatalf("fetch article: %v", err)
	}
	if source != FetchSourceHTTP || text != "Direct body text." || note != "" {
		t.Fatalf("unexpected result source=%q text=%q note=%q", source, text, note)
	}
	if jinaHits.Load() != 0 {
		t.Fatalf("expected no Jina request, got %d", jinaHits.Load())
	}
}

func TestFetchArticlePrimaryUsesJinaFirst(t *testing.T) {
	var siteHits, jinaHits atomic.Int32
	site := newArticleServer(t, http.StatusOK, &siteHits)
	newJinaServer(t, http.StatusOK, jinaOKBody, &jinaHits)

	_, source, note, err := fetchArticle(site.URL, jinaOptions(config.JinaFetchPrimary))
	if err != nil {
		t.Fatalf("fetch article: %v", err)
	}
	if source != FetchSourceJina || note != "" {
		t.Fatalf("unexpected result source=%q note=%q", source, note)
	}
	if siteHits.Load() != 0 {
		t.Fatalf("expected no direct request, got %d", siteHits.Load())
	}
}

func TestFetchArticlePrimaryFallsBackToHTTPOnJinaError(t *testing.T) {
	var siteHits, jinaHits atomic.Int32
	site := newArticleServer(t, http.StatusOK, &siteHits)
	newJinaServer(t, http.StatusUnauthorized, `{"data":null,"code":401,"readableMessage":"AuthenticationFailedError: Invalid API key"}`, &jinaHits)

	text, source, note, err := fetchArticle(site.URL, jinaOptions(config.JinaFetchPrimary))
	if err != nil {
		t.Fatalf("fetch article: %v", err)
	}
	if source != FetchSourceHTTP || text != "Direct body text." {
		t.Fatalf("unexpected result source=%q text=%q", source, text)
	}
	if !strings.Contains(note, "jina fetch failed: reader status 401: AuthenticationFailedError: Invalid API key") {
		t.Fatalf("expected Jina failure reason in note, got %q", note)
	}
}

func TestFetchArticleJinaTargetErrorStatusFails(t *testing.T) {
	var siteHits, jinaHits atomic.Int32
	site := newArticleServer(t, http.StatusNotFound, &siteHits)
	newJinaServer(t, http.StatusOK, `{"code":200,"data":{"content":"Not found page","httpStatus":404,"warning":"Target URL returned error 404: Not Found"}}`, &jinaHits)

	_, _, _, err := fetchArticle(site.URL, jinaOptions(config.JinaFetchPrimary))
	if err == nil {
		t.Fatalf("expected error when both stages fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "jina fetch failed: target returned status 404") || !strings.Contains(msg, "http fetch failed") {
		t.Fatalf("expected both stage failures in error, got %q", msg)
	}
}

func TestFetchArticleTreatsEmptyJinaContentAsFailure(t *testing.T) {
	var siteHits, jinaHits atomic.Int32
	site := newArticleServer(t, http.StatusOK, &siteHits)
	newJinaServer(t, http.StatusOK, `{"code":200,"data":{"content":"  \n ","httpStatus":"200"}}`, &jinaHits)

	_, source, note, err := fetchArticle(site.URL, jinaOptions(config.JinaFetchPrimary))
	if err != nil {
		t.Fatalf("fetch article: %v", err)
	}
	if source != FetchSourceHTTP || !strings.Contains(note, "jina fetch failed: no text content found in article") {
		t.Fatalf("unexpected result source=%q note=%q", source, note)
	}
}

func TestFetchArticleNoneModeUsesHTTPOnly(t *testing.T) {
	var siteHits, jinaHits atomic.Int32
	site := newArticleServer(t, http.StatusForbidden, &siteHits)
	newJinaServer(t, http.StatusOK, jinaOKBody, &jinaHits)

	_, _, _, err := fetchArticle(site.URL, jinaOptions(config.JinaFetchNone))
	if err == nil {
		t.Fatalf("expected HTTP error")
	}
	if strings.Contains(err.Error(), "http fetch failed") {
		t.Fatalf("expected the plain single-stage error, got %q", err.Error())
	}
	if jinaHits.Load() != 0 {
		t.Fatalf("expected no Jina request, got %d", jinaHits.Load())
	}
}

func TestSummarizeArticleMarksJinaEngineAndNote(t *testing.T) {
	var siteHits, jinaHits atomic.Int32
	site := newArticleServer(t, http.StatusForbidden, &siteHits)
	newJinaServer(t, http.StatusOK, jinaOKBody, &jinaHits)

	result, err := SummarizeArticle(site.URL, false, jinaOptions(config.JinaFetchFallback))
	if err != nil {
		t.Fatalf("summarize article: %v", err)
	}
	if result.Engine != EngineVerbatim+"+jina" {
		t.Fatalf("expected engine %q, got %q", EngineVerbatim+"+jina", result.Engine)
	}
	if !strings.Contains(result.Warning, "fetched via jina.") {
		t.Fatalf("expected fetch note in warning, got %q", result.Warning)
	}
}
