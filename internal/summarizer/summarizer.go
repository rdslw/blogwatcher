package summarizer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"github.com/rdslw/blogwatcher/internal/config"
)

const (
	defaultTimeout     = 30 * time.Second
	maxLocalChars      = 2000
	verbatimWordLimit  = 250
	openAIAPIURL       = "https://api.openai.com/v1/chat/completions"
	articleFetchUA     = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"
	articleFetchAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
)

type Options struct {
	OpenAIAPIKey    string
	Model           string
	SystemPrompt    string
	MaxRequestBytes int
}

type Result struct {
	Summary string
	Engine  string
	Warning string
}

const (
	EngineOpenAI   = "openai"
	EngineSnippet  = "snippet"
	EngineVerbatim = "verbatim"
)

func OptionsFromConfig(cfg config.SummaryConfig) Options {
	return Options{
		OpenAIAPIKey:    cfg.OpenAIAPIKey,
		Model:           cfg.Model,
		SystemPrompt:    cfg.SystemPrompt,
		MaxRequestBytes: cfg.MaxRequestBytes,
	}
}

// SummarizeArticle fetches the article at the given URL and returns a summary.
// If forceExtractive is true, always uses the local non-LLM path.
// Otherwise, it uses OpenAI when configured and falls back to a snippet on failure.
func resolveAPIKey(opts Options) string {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return key
	}
	return opts.OpenAIAPIKey
}

func SummarizeArticle(articleURL string, forceExtractive bool, opts Options) (Result, error) {
	text, err := fetchArticleText(articleURL)
	if err != nil {
		return Result{}, err
	}
	if text == "" {
		return Result{}, fmt.Errorf("no text content found in article")
	}
	if wordCount(text) < verbatimWordLimit {
		return Result{Summary: text, Engine: EngineVerbatim}, nil
	}

	if !forceExtractive {
		apiKey := resolveAPIKey(opts)
		if apiKey != "" {
			summary, err := summarizeWithLLM(text, apiKey, opts)
			if err == nil {
				return Result{Summary: summary, Engine: EngineOpenAI}, nil
			}

			return Result{
				Summary: summarizeSnippet(text),
				Engine:  EngineSnippet,
				Warning: fmt.Sprintf("OpenAI summarization failed: %v. Fell back to snippet summarization.", err),
			}, nil
		}
	}
	return Result{Summary: summarizeSnippet(text), Engine: EngineSnippet}, nil
}

func fetchArticleText(articleURL string) (string, error) {
	client := &http.Client{Timeout: defaultTimeout}
	req, err := http.NewRequest(http.MethodGet, articleURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create article request for %s: %v", articleURL, err)
	}
	req.Header.Set("User-Agent", articleFetchUA)
	req.Header.Set("Accept", articleFetchAccept)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch article %s: %v", articleURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("failed to fetch article %s: status %d", fetchErrorURL(articleURL, resp), resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to parse article: %v", err)
	}

	doc.Find(strings.Join([]string{
		"script",
		"style",
		"nav",
		"header",
		"footer",
		"aside",
		".sidebar",
		".comments",
		".menu",
		".navigation",
		".social",
		".share",
		".banner",
		".adsbygoogle",
		".popup",
		".popup-1",
		".modal",
		".dialog",
		".overlay",
		".cookie",
		".consent",
		".newsletter",
		".subscribe",
		".paywall",
		"[role='dialog']",
	}, ", ")).Remove()

	text := extractPreferredContent(doc)

	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n"), nil
}

func fetchErrorURL(articleURL string, resp *http.Response) string {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return articleURL
	}

	finalURL := strings.TrimSpace(resp.Request.URL.String())
	if finalURL == "" || finalURL == articleURL {
		return articleURL
	}
	return fmt.Sprintf("%s (final URL %s)", articleURL, finalURL)
}

func extractPreferredContent(doc *goquery.Document) string {
	selectors := []string{
		"[data-permalink-context]",
		".beat-note",
		".blogmark-body",
		".skrot",
		".note",
		".post-content",
		".entry-content",
		".article-content",
		"[class*='article-body']",
		"[class*='post-body']",
		"[class*='content-body']",
		"[class*='Body-module'][class*='__body']",
		".content-txt",
		".entry",
		"article",
		"main",
		"body",
	}

	bestText := ""
	bestScore := math.MinInt

	for _, selector := range selectors {
		doc.Find(selector).Each(func(_ int, selection *goquery.Selection) {
			text := strings.TrimSpace(selection.Text())
			if text == "" {
				return
			}

			score := scoreContentCandidate(selector, selection, text)
			if score > bestScore || (score == bestScore && len(text) > len(bestText)) {
				bestScore = score
				bestText = text
			}
		})
		if bestScore > 0 {
			return bestText
		}
	}

	return bestText
}

func scoreContentCandidate(selector string, selection *goquery.Selection, text string) int {
	score := len(text)

	switch selector {
	case "[data-permalink-context]", ".beat-note", ".blogmark-body", ".skrot",
		".post-content", ".entry-content", ".article-content",
		"[class*='article-body']", "[class*='post-body']", "[class*='content-body']",
		"[class*='Body-module'][class*='__body']", "article":
		score += 600
	case ".note", ".entry", "main":
		score += 250
	}

	if selection.Closest(".popup, .popup-1, .modal, .dialog, [role='dialog'], .overlay, .cookie, .consent, .newsletter, .subscribe, .paywall").Length() > 0 {
		score -= 2000
	}

	lower := strings.ToLower(text)
	for _, phrase := range []string{
		"privacy policy",
		"cookie policy",
		"register",
		"registration",
		"log in",
		"sign in",
		"subscribe",
		"buy now",
		"paywall",
		"related articles",
		"recent articles",
		"informacje o przetwarzaniu twoich danych osobowych",
		"załóż konto",
		"zaloguj się",
		"wykupić abonament",
		"polityka prywatności",
		"dane osobowe",
	} {
		if strings.Contains(lower, phrase) {
			score -= 900
		}
	}

	linkPenalty := selection.Find("a").Length() * 40
	if linkPenalty > 0 {
		score -= linkPenalty
	}

	buttonPenalty := selection.Find("button, input, form").Length() * 100
	if buttonPenalty > 0 {
		score -= buttonPenalty
	}

	return score
}

func summarizeSnippet(text string) string {
	if utf8.RuneCountInString(text) <= maxLocalChars {
		return text
	}

	truncated := string([]rune(text)[:maxLocalChars])
	if idx := strings.LastIndexAny(truncated, ".!?"); idx > 0 {
		truncated = truncated[:idx+1]
	} else if idx := strings.LastIndex(truncated, " "); idx > 0 {
		truncated = truncated[:idx]
	}
	return truncated + "..."
}

func wordCount(text string) int {
	return len(strings.Fields(text))
}

func truncateUTF8ToBytes(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}

	cutoff := 0
	for i := range text {
		if i > maxBytes {
			break
		}
		cutoff = i
	}
	return text[:cutoff]
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func summarizeWithLLM(text string, apiKey string, opts Options) (string, error) {
	maxChars := opts.MaxRequestBytes
	if maxChars <= 0 {
		maxChars = config.DefaultMaxRequestBytes
	}
	if len(text) > maxChars {
		text = truncateUTF8ToBytes(text, maxChars)
	}

	model := opts.Model
	if model == "" {
		model = config.DefaultModel
	}
	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = config.DefaultSystemPrompt
	}

	reqBody := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: text},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %v", err)
	}

	req, err := http.NewRequest("POST", openAIAPIURL, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OpenAI API request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read OpenAI response: %v", err)
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("failed to parse OpenAI response: %v", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("OpenAI API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("OpenAI returned no choices")
	}

	return strings.TrimSpace(chatResp.Choices[0].Message.Content), nil
}
