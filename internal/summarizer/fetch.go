package summarizer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdslw/blogwatcher/internal/config"
)

const (
	FetchSourceHTTP = "http"
	FetchSourceJina = "jina"
)

const (
	jinaTimeout      = 60 * time.Second
	jinaMaxBodyBytes = 16 << 20
)

var jinaReaderURL = "https://r.jina.ai/"

type fetchStage struct {
	source string
	fetch  func() (string, error)
}

func fetchArticle(articleURL string, opts Options) (text string, source string, note string, err error) {
	stages := fetchStages(articleURL, opts)
	var failures []string
	var lastErr error
	for _, stage := range stages {
		text, err := stage.fetch()
		if err == nil && text == "" {
			err = errors.New("no text content found in article")
		}
		if err == nil {
			if len(failures) > 0 {
				note = fmt.Sprintf("%s; fetched via %s.", strings.Join(failures, "; "), stage.source)
			}
			return text, stage.source, note, nil
		}
		lastErr = err
		failures = append(failures, fmt.Sprintf("%s fetch failed: %v", stage.source, err))
	}
	if len(stages) == 1 {
		return "", "", "", lastErr
	}
	return "", "", "", errors.New(strings.Join(failures, "; "))
}

func fetchStages(articleURL string, opts Options) []fetchStage {
	httpStage := fetchStage{FetchSourceHTTP, func() (string, error) {
		return fetchArticleTextWithUserAgent(articleURL, opts.UserAgent)
	}}
	if opts.JinaAPIKey == "" {
		return []fetchStage{httpStage}
	}
	jinaStage := fetchStage{FetchSourceJina, func() (string, error) {
		return fetchArticleTextViaJina(articleURL, opts.JinaAPIKey)
	}}
	switch opts.JinaFetch {
	case config.JinaFetchPrimary:
		return []fetchStage{jinaStage, httpStage}
	case config.JinaFetchFallback:
		return []fetchStage{httpStage, jinaStage}
	default:
		return []fetchStage{httpStage}
	}
}

type jinaResponse struct {
	Message         string    `json:"message"`
	ReadableMessage string    `json:"readableMessage"`
	Data            *jinaData `json:"data"`
}

type jinaData struct {
	Content    string          `json:"content"`
	HTTPStatus json.RawMessage `json:"httpStatus"`
}

func fetchArticleTextViaJina(articleURL string, apiKey string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, jinaReaderURL+articleURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create reader request for %s: %v", articleURL, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Retain-Images", "none")

	client := &http.Client{Timeout: jinaTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reader request for %s: %v", articleURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, jinaMaxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to read reader response: %v", err)
	}
	var payload jinaResponse
	jsonErr := json.Unmarshal(body, &payload)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := payload.ReadableMessage
		if msg == "" {
			msg = payload.Message
		}
		if msg == "" {
			return "", fmt.Errorf("reader status %d", resp.StatusCode)
		}
		return "", fmt.Errorf("reader status %d: %s", resp.StatusCode, msg)
	}
	if jsonErr != nil {
		return "", fmt.Errorf("failed to parse reader response: %v", jsonErr)
	}
	if payload.Data == nil {
		return "", errors.New("reader response has no data")
	}
	if status := jinaTargetStatus(payload.Data.HTTPStatus); status != 0 && (status < 200 || status >= 300) {
		return "", fmt.Errorf("target returned status %d", status)
	}
	return compactLines(payload.Data.Content), nil
}

func jinaTargetStatus(raw json.RawMessage) int {
	status, err := strconv.Atoi(strings.Trim(string(raw), `" `))
	if err != nil {
		return 0
	}
	return status
}
