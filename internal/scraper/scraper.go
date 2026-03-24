package scraper

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var nowFn = time.Now

type ScrapedArticle struct {
	Title         string
	URL           string
	PublishedDate *time.Time
}

type ScrapeError struct {
	Message string
}

func (e ScrapeError) Error() string {
	return e.Message
}

func ScrapeBlog(blogURL string, selector string, timeout time.Duration) ([]ScrapedArticle, error) {
	client := &http.Client{Timeout: timeout}
	response, err := client.Get(blogURL)
	if err != nil {
		return nil, ScrapeError{Message: fmt.Sprintf("failed to fetch page: %v", err)}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ScrapeError{Message: fmt.Sprintf("failed to fetch page: status %d", response.StatusCode)}
	}

	base, err := url.Parse(blogURL)
	if err != nil {
		return nil, ScrapeError{Message: "invalid blog url"}
	}

	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return nil, ScrapeError{Message: fmt.Sprintf("failed to parse page: %v", err)}
	}

	seen := make(map[string]struct{})
	var articles []ScrapedArticle

	doc.Find(selector).Each(func(_ int, selection *goquery.Selection) {
		link := selection
		if goquery.NodeName(selection) != "a" {
			link = selection.Find("a").First()
		}
		if link.Length() == 0 {
			return
		}
		href, exists := link.Attr("href")
		if !exists {
			return
		}
		resolved := resolveURL(base, href)
		if resolved == "" {
			return
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}

		title := extractTitle(link, selection)
		if title == "" {
			return
		}
		articles = append(articles, ScrapedArticle{
			Title:         title,
			URL:           resolved,
			PublishedDate: extractPublishedDate(selection),
		})
	})

	return articles, nil
}

func extractTitle(link *goquery.Selection, _ *goquery.Selection) string {
	// Prefer heading elements inside the link for a clean title
	heading := link.Find("h1, h2, h3, h4, h5, h6").First()
	if heading.Length() > 0 {
		if text := strings.TrimSpace(heading.Text()); text != "" {
			return text
		}
	}

	// Try elements whose class includes a title segment such as
	// "title", "post-title", or "article__title", but not "subtitle".
	var titleEl *goquery.Selection
	link.Find("[class]").Each(func(_ int, s *goquery.Selection) {
		if titleEl != nil {
			return
		}
		class, _ := s.Attr("class")
		if hasTitleClass(class) {
			if text := strings.TrimSpace(s.Text()); text != "" {
				titleEl = s
			}
		}
	})
	if titleEl != nil {
		if text := strings.TrimSpace(titleEl.Text()); text != "" {
			return text
		}
	}

	// Fall back to title attribute
	if title, exists := link.Attr("title"); exists {
		title = strings.TrimSpace(title)
		if title != "" {
			return title
		}
	}

	// Plain text fallback
	if text := strings.TrimSpace(link.Text()); text != "" {
		return text
	}
	return ""
}

func hasTitleClass(classAttr string) bool {
	for _, className := range strings.Fields(strings.ToLower(classAttr)) {
		for _, part := range strings.FieldsFunc(className, func(r rune) bool {
			return r == '-' || r == '_'
		}) {
			if part == "title" {
				return true
			}
		}
	}
	return false
}

func extractPublishedDate(selection *goquery.Selection) *time.Time {
	candidates := []string{}

	if timeNode := selection.Find("time").First(); timeNode.Length() > 0 {
		if datetime, exists := timeNode.Attr("datetime"); exists {
			candidates = append(candidates, datetime)
		}
		if timeText := strings.TrimSpace(timeNode.Text()); timeText != "" {
			candidates = append(candidates, timeText)
		}
	}
	selection.Find("[class]").Each(func(_ int, s *goquery.Selection) {
		class, _ := s.Attr("class")
		class = strings.ToLower(class)
		if !strings.Contains(class, "date") && !strings.Contains(class, "eyebrow") {
			return
		}
		if text := strings.TrimSpace(s.Text()); text != "" {
			candidates = append(candidates, text)
		}
	})
	for _, attr := range []string{"data-date", "data-published", "data-published-at"} {
		if value, exists := selection.Attr(attr); exists && strings.TrimSpace(value) != "" {
			candidates = append(candidates, strings.TrimSpace(value))
		}
	}

	candidates = uniqueNonEmptyStrings(candidates)

	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02",
		"2006/01/02",
		"2006.01.02",
		"Jan 2, 2006",
		"January 2, 2006",
		"2 Jan 2006",
		"2 January 2006",
		"Jan 2 2006",
		"January 2 2006",
		"2.01.2006",
		"02.01.2006",
	}

	for _, candidate := range candidates {
		candidate = normalizeDateCandidate(candidate)
		for _, layout := range layouts {
			if parsed, err := time.Parse(layout, candidate); err == nil {
				if !isReasonablePublishedDate(parsed) {
					return nil
				}
				return &parsed
			}
		}
	}

	return nil
}

var nonLetterDateRune = regexp.MustCompile(`[^[:alpha:]]+`)

func normalizeDateCandidate(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return candidate
	}

	replacements := map[string]string{
		"stycznia":     "January",
		"lutego":       "February",
		"marca":        "March",
		"kwietnia":     "April",
		"maja":         "May",
		"czerwca":      "June",
		"lipca":        "July",
		"sierpnia":     "August",
		"wrzesnia":     "September",
		"września":     "September",
		"pazdziernika": "October",
		"października": "October",
		"listopada":    "November",
		"grudnia":      "December",
	}

	words := strings.Fields(candidate)
	for i, word := range words {
		key := strings.ToLower(nonLetterDateRune.ReplaceAllString(word, ""))
		if replacement, ok := replacements[key]; ok {
			words[i] = replacement
		}
	}

	return strings.Join(words, " ")
}

func isReasonablePublishedDate(published time.Time) bool {
	cutoff := nowFn().AddDate(0, 0, -180)
	upperBound := nowFn().Add(48 * time.Hour)
	return !published.Before(cutoff) && !published.After(upperBound)
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func resolveURL(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return base.ResolveReference(parsed).String()
}

func IsScrapeError(err error) bool {
	var scrapeErr ScrapeError
	return errors.As(err, &scrapeErr)
}
