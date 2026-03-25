package summarizer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSummarizeWithLLMRespectsMaxRequestBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html><body><article>é漢z</article></body></html>")
	}))
	defer server.Close()

	originalTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})

	var requestBody chatRequest
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.URL.String(), server.URL) {
			return originalTransport.RoundTrip(req)
		}
		if req.URL.String() != openAIAPIURL {
			t.Fatalf("unexpected request URL: %s", req.URL.String())
		}
		if err := json.NewDecoder(req.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`)),
			Request:    req,
		}, nil
	})

	summary, err := summarizeWithLLM("é漢z", "test-key", Options{
		Model:           "test-model",
		SystemPrompt:    "summarize",
		MaxRequestBytes: 4,
	})
	if err != nil {
		t.Fatalf("summarize with llm: %v", err)
	}
	if summary != "ok" {
		t.Fatalf("expected summary ok, got %q", summary)
	}

	if got := requestBody.Messages[1].Content; got != "é" {
		t.Fatalf("expected truncated content %q, got %q", "é", got)
	}
	if gotBytes := len(requestBody.Messages[1].Content); gotBytes > 4 {
		t.Fatalf("expected content within 4 bytes, got %d", gotBytes)
	}
}

func TestSummarizeArticleReportsExtractiveFallbackWhenOpenAIFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html><body><article>Fallback body text.</article></body></html>")
	}))
	defer server.Close()

	originalTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.URL.String(), server.URL) {
			return originalTransport.RoundTrip(req)
		}
		if req.URL.String() != openAIAPIURL {
			t.Fatalf("unexpected request URL: %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid api key"}}`)),
			Request:    req,
		}, nil
	})

	result, err := SummarizeArticle(server.URL, false, Options{
		OpenAIAPIKey: "bad-key",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("summarize article: %v", err)
	}
	if result.Engine != EngineVerbatim {
		t.Fatalf("expected short-article engine %q, got %q", EngineVerbatim, result.Engine)
	}
	if result.Warning != "" {
		t.Fatalf("expected no warning for verbatim short article, got %q", result.Warning)
	}
	if !strings.Contains(result.Summary, "Fallback body text.") {
		t.Fatalf("expected verbatim summary, got %q", result.Summary)
	}
}

func TestSummarizeArticleFallsBackToSnippetWhenOpenAIFails(t *testing.T) {
	longText := strings.Repeat("word ", verbatimWordLimit) + "ending sentence."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html><body><article>"+longText+"</article></body></html>")
	}))
	defer server.Close()

	originalTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasPrefix(req.URL.String(), server.URL) {
			return originalTransport.RoundTrip(req)
		}
		if req.URL.String() != openAIAPIURL {
			t.Fatalf("unexpected request URL: %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid api key"}}`)),
			Request:    req,
		}, nil
	})

	result, err := SummarizeArticle(server.URL, false, Options{
		OpenAIAPIKey: "bad-key",
		Model:        "test-model",
	})
	if err != nil {
		t.Fatalf("summarize article: %v", err)
	}
	if result.Engine != EngineSnippet {
		t.Fatalf("expected fallback engine %q, got %q", EngineSnippet, result.Engine)
	}
	if result.Warning == "" || !strings.Contains(result.Warning, "OpenAI summarization failed") {
		t.Fatalf("expected fallback warning, got %q", result.Warning)
	}
	if !strings.Contains(result.Summary, "word word") {
		t.Fatalf("expected snippet summary, got %q", result.Summary)
	}
}

func TestFetchArticleTextPrefersMainContentOverPageChrome(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
  <div id="smallhead">Simon Willison's Weblog <a href="/subscribe">Subscribe</a></div>
  <div id="sponsored-banner">Sponsored by Example</div>
  <div class="entry">
    <div class="note">
      <p>First useful paragraph.</p>
      <p>Second useful paragraph.</p>
    </div>
    <div class="entryFooter">Posted today</div>
  </div>
  <div class="recent-articles">Noise that should not be included.</div>
  <div id="secondary">Tag cloud noise</div>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, html)
	}))
	defer server.Close()

	text, err := fetchArticleText(server.URL)
	if err != nil {
		t.Fatalf("fetch article text: %v", err)
	}
	if strings.Contains(text, "Subscribe") || strings.Contains(text, "Sponsored by") || strings.Contains(text, "Noise that should not be included") {
		t.Fatalf("expected page chrome to be removed, got %q", text)
	}
	if !strings.Contains(text, "First useful paragraph.") || !strings.Contains(text, "Second useful paragraph.") {
		t.Fatalf("expected main content, got %q", text)
	}
}

func TestFetchArticleTextPrefersAnthropicBodyContainer(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
  <main>
    <div class="PostDetail-module-scss-module__UQuRMa__header">
      <span>Announcements</span><h1>Anthropic invests</h1><div>Mar 12, 2026</div>
    </div>
    <div class="Body-module-scss-module__z40yvW__body">
      <p class="Body-module-scss-module__z40yvW__reading-column">We are launching the partner network.</p>
      <p class="Body-module-scss-module__z40yvW__reading-column">This is the second paragraph.</p>
    </div>
  </main>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, html)
	}))
	defer server.Close()

	text, err := fetchArticleText(server.URL)
	if err != nil {
		t.Fatalf("fetch article text: %v", err)
	}
	if strings.Contains(text, "AnnouncementsAnthropic invests") || strings.Contains(text, "Mar 12, 2026") {
		t.Fatalf("expected header chrome to be excluded, got %q", text)
	}
	if !strings.Contains(text, "We are launching the partner network.") || !strings.Contains(text, "This is the second paragraph.") {
		t.Fatalf("expected body content, got %q", text)
	}
}

func TestFetchArticleTextPrefersSimonLinkAndBeatBodies(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
  <div class="entry">
    <p class="mobile-date-eyebrow">24th March 2026 - Link Blog</p>
    <div data-permalink-context="/example/post">
      <p>Link post body.</p>
    </div>
  </div>
  <div class="beat">
    <div class="beat-row1">
      <span class="beat-label release">Release</span>
      <span class="beat-title">Package 1.0</span>
    </div>
    <div class="beat-note blogmark-body">
      <p>Beat body text.</p>
    </div>
  </div>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, html)
	}))
	defer server.Close()

	text, err := fetchArticleText(server.URL)
	if err != nil {
		t.Fatalf("fetch article text: %v", err)
	}
	if strings.Contains(text, "Link Blog") || strings.Contains(text, "Release") {
		t.Fatalf("expected metadata labels to be excluded, got %q", text)
	}
	if !strings.Contains(text, "Link post body.") {
		t.Fatalf("expected primary content, got %q", text)
	}
}

func TestFetchArticleTextPrefersBeatNoteBody(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
  <div class="entry">
    <p class="mobile-date-eyebrow">23rd March 2026</p>
    <div class="beat">
      <div class="beat-row1">
        <span class="beat-label release">Release</span>
        <span class="beat-title">Package 1.0</span>
      </div>
      <div class="beat-note blogmark-body">
        <p>Beat body text.</p>
      </div>
    </div>
  </div>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, html)
	}))
	defer server.Close()

	text, err := fetchArticleText(server.URL)
	if err != nil {
		t.Fatalf("fetch article text: %v", err)
	}
	if strings.Contains(text, "Release") || strings.Contains(text, "Package 1.0") {
		t.Fatalf("expected beat metadata to be excluded, got %q", text)
	}
	if !strings.Contains(text, "Beat body text.") {
		t.Fatalf("expected beat body text, got %q", text)
	}
}

func TestFetchArticleTextPrefersObligacjeContentExcerpt(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
  <div id="Top">wtorek, 24 marca 2026 Zaloguj się</div>
  <nav id="MainMenu">Newsroom</nav>
  <div id="Main">
    <div class="title-4">Marvipol stoi przed wyraźnym wzrostem zadłużenia</div>
    <div class="author">msd | 24 marca 2026</div>
    <div class="social">share buttons</div>
    <div class="content-txt">
      <div class="skrot">Deweloper szykuje się do znaczącego rozszerzenia oferty.</div>
      <div class="banner">ad slot</div>
      <div class="note-4">Żeby dokończyć lekturę należy wykupić abonament</div>
    </div>
    <ul class="news-list">
      <li>related story</li>
    </ul>
  </div>
  <div class="bg-popup"></div>
  <div class="popup-1">
    <div class="title"><h2>Rejestracja</h2></div>
    <div class="content-txt">
      <p><strong>INFORMACJE O PRZETWARZANIU TWOICH DANYCH OSOBOWYCH</strong></p>
      <p>Administratorem Twoich danych osobowych jest Obligacje.pl sp. z o.o.</p>
      <a href="/pl/rejestracja">Załóż konto</a>
    </div>
  </div>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, html)
	}))
	defer server.Close()

	text, err := fetchArticleText(server.URL)
	if err != nil {
		t.Fatalf("fetch article text: %v", err)
	}
	if strings.Contains(text, "Zaloguj się") || strings.Contains(text, "share buttons") || strings.Contains(text, "wykupić abonament") || strings.Contains(text, "related story") || strings.Contains(text, "INFORMACJE O PRZETWARZANIU") || strings.Contains(text, "Załóż konto") {
		t.Fatalf("expected obligacje chrome to be removed, got %q", text)
	}
	if !strings.Contains(text, "Deweloper szykuje się do znaczącego rozszerzenia oferty.") {
		t.Fatalf("expected obligacje excerpt, got %q", text)
	}
}
