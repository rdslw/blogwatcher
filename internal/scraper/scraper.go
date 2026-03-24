package scraper

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

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
			Title: title,
			URL:   resolved,
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
