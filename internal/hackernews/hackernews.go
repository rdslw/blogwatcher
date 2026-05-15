package hackernews

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rdslw/blogwatcher/internal/config"
	"github.com/rdslw/blogwatcher/internal/model"
	"github.com/rdslw/blogwatcher/internal/summarizer"
)

const (
	algoliaBaseURL = "https://hn.algolia.com/api/v1"
	timeout        = 15 * time.Second
)

type Result struct {
	ID                int64
	URL               string
	Title             string
	Points            int
	Comments          int
	DiscussionSummary string
	Warning           string
	Cached            bool
	NotFound          bool
}

type searchResponse struct {
	Hits []searchHit `json:"hits"`
}

type searchHit struct {
	ObjectID    string `json:"objectID"`
	StoryID     int64  `json:"story_id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Points      int    `json:"points"`
	NumComments int    `json:"num_comments"`
}

type itemNode struct {
	ID       int64      `json:"id"`
	Author   string     `json:"author"`
	Text     string     `json:"text"`
	Title    string     `json:"title"`
	URL      string     `json:"url"`
	Points   *int       `json:"points"`
	Children []itemNode `json:"children"`
}

func EnrichArticle(article model.Article, opts summarizer.Options, refresh bool) (*Result, error) {
	if !refresh && strings.TrimSpace(article.HNSummary) != "" && article.HNItemID > 0 {
		return &Result{
			ID:                article.HNItemID,
			URL:               ItemURL(article.HNItemID),
			Points:            article.HNPoints,
			Comments:          article.HNComments,
			DiscussionSummary: article.HNSummary,
			Cached:            true,
		}, nil
	}
	if !refresh && article.HNItemID > 0 && article.HNComments == 0 && article.HNFetched != nil {
		return &Result{
			ID:       article.HNItemID,
			URL:      ItemURL(article.HNItemID),
			Points:   article.HNPoints,
			Comments: article.HNComments,
			Cached:   true,
		}, nil
	}

	itemID := article.HNItemID
	var hit searchHit
	if itemID == 0 {
		var ok bool
		var err error
		hit, ok, err = findSubmission(article)
		if err != nil {
			return nil, err
		}
		if !ok {
			return &Result{NotFound: true}, nil
		}
		itemID = hit.ID()
	}

	root, err := fetchItem(itemID)
	if err != nil {
		return nil, err
	}

	threadText := FormatPathID(root.Children)
	comments := CountComments(root.Children)
	if comments == 0 {
		comments = hit.NumComments
	}
	result := &Result{
		ID:       itemID,
		URL:      ItemURL(itemID),
		Title:    firstNonEmpty(root.Title, hit.Title),
		Points:   hit.Points,
		Comments: comments,
	}
	if root.Points != nil {
		result.Points = *root.Points
	}

	if strings.TrimSpace(threadText) == "" {
		return result, nil
	}

	prompt := opts.HackerNewsPrompt
	if strings.TrimSpace(prompt) == "" {
		prompt = config.DefaultHackerNewsPrompt
	}
	maxRequestBytes := opts.HackerNewsMaxRequestBytes
	if maxRequestBytes <= 0 {
		maxRequestBytes = config.DefaultHackerNewsMaxRequestBytes
	}
	summary, err := summarizer.SummarizeTextWithPromptLimit(threadText, prompt, opts, maxRequestBytes)
	if err != nil {
		result.Warning = fmt.Sprintf("HN discussion summary failed: %v", err)
		return result, nil
	}
	result.DiscussionSummary = summary.Summary
	result.Warning = summary.Warning
	return result, nil
}

func ItemURL(id int64) string {
	return fmt.Sprintf("https://news.ycombinator.com/item?id=%d", id)
}

func findSubmission(article model.Article) (searchHit, bool, error) {
	query := searchQuery(article)
	if query == "" {
		return searchHit{}, false, nil
	}

	endpoint := algoliaBaseURL + "/search?tags=story&hitsPerPage=10&query=" + url.QueryEscape(query)
	var data searchResponse
	if err := getJSON(endpoint, &data); err != nil {
		return searchHit{}, false, err
	}

	articleURL := normalizeArticleURL(article.URL)
	for _, hit := range data.Hits {
		if hit.ID() == 0 {
			continue
		}
		if articleURL != "" && normalizeArticleURL(hit.URL) == articleURL {
			return hit, true, nil
		}
	}

	if articleURL == "" {
		title := normalizeTitle(article.Title)
		for _, hit := range data.Hits {
			if hit.ID() != 0 && title != "" && normalizeTitle(hit.Title) == title {
				return hit, true, nil
			}
		}
	}

	return searchHit{}, false, nil
}

func fetchItem(id int64) (itemNode, error) {
	var item itemNode
	if id <= 0 {
		return item, fmt.Errorf("invalid HN item id %d", id)
	}
	endpoint := fmt.Sprintf("%s/items/%d", algoliaBaseURL, id)
	if err := getJSON(endpoint, &item); err != nil {
		return item, err
	}
	return item, nil
}

func FormatPathID(comments []itemNode) string {
	var lines []string
	for i, child := range comments {
		walkComment(child, strconv.Itoa(i+1), &lines)
	}
	return strings.Join(lines, "\n\n")
}

func CountComments(comments []itemNode) int {
	total := 0
	for _, child := range comments {
		total++
		total += CountComments(child.Children)
	}
	return total
}

func walkComment(node itemNode, path string, lines *[]string) {
	author := strings.TrimSpace(node.Author)
	if author == "" {
		author = "Anonymous"
	}
	text := cleanHTMLText(node.Text)
	if text == "" {
		text = "(deleted)"
	}
	*lines = append(*lines, fmt.Sprintf("[%s] %s: %s", path, author, text))

	for i, child := range node.Children {
		walkComment(child, fmt.Sprintf("%s.%d", path, i+1), lines)
	}
}

func cleanHTMLText(text string) string {
	if text == "" {
		return ""
	}
	text = html.UnescapeString(text)
	text = regexp.MustCompile(`(?i)<\s*(p|br)\s*/?\s*>`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`(?is)<a\s+href="[^"]*"[^>]*>(.*?)</a>`).ReplaceAllString(text, "$1")
	text = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`\[(\d+)\]`).ReplaceAllString(text, "{$1}")
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func (hit searchHit) ID() int64 {
	if hit.StoryID > 0 {
		return hit.StoryID
	}
	id, _ := strconv.ParseInt(hit.ObjectID, 10, 64)
	return id
}

func searchQuery(article model.Article) string {
	if u := normalizeArticleURL(article.URL); u != "" {
		return u
	}
	return strings.TrimSpace(article.Title)
}

func normalizeArticleURL(raw string) string {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "#atom-everything"))
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return strings.TrimSuffix(raw, "/")
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Host), "www.")
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	query := normalizedQuery(parsed.Query())
	if query != "" {
		return host + path + "?" + query
	}
	return host + path
}

func normalizedQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	for key := range values {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" {
			delete(values, key)
		}
	}
	return values.Encode()
}

func normalizeTitle(title string) string {
	return strings.Join(strings.Fields(strings.ToLower(title)), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func getJSON(endpoint string, target any) error {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "blogwatcher/hn-enrichment")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Algolia HN API returned status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("failed to parse Algolia HN response: %w", err)
	}
	return nil
}
