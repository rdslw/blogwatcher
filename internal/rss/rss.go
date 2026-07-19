package rss

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
	"github.com/rdslw/blogwatcher/internal/sitehttp"
)

type FeedArticle struct {
	Title         string
	URL           string
	Description   string
	PublishedDate *time.Time
}

type FeedParseError struct {
	Message string
}

func (e FeedParseError) Error() string {
	return e.Message
}

func ParseFeed(feedURL string, timeout time.Duration) ([]FeedArticle, error) {
	return ParseFeedWithUserAgent(feedURL, timeout, "")
}

func ParseFeedWithUserAgent(feedURL string, timeout time.Duration, userAgent string) ([]FeedArticle, error) {
	client := &http.Client{Timeout: timeout}
	response, err := sitehttp.Get(client, feedURL, userAgent)
	if err != nil {
		if IsTimeoutError(err) {
			return nil, FeedParseError{Message: fmt.Sprintf("feed fetch timeout after %s: %v", timeout, err)}
		}
		return nil, FeedParseError{Message: fmt.Sprintf("failed to fetch feed: %v", err)}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, FeedParseError{Message: fmt.Sprintf("failed to fetch feed: status %d", response.StatusCode)}
	}

	parser := gofeed.NewParser()
	feed, err := parser.Parse(response.Body)
	if err != nil {
		if IsTimeoutError(err) {
			return nil, FeedParseError{Message: fmt.Sprintf("feed read timeout after %s while parsing: %v", timeout, err)}
		}
		return nil, FeedParseError{Message: fmt.Sprintf("failed to parse feed: %v", err)}
	}

	var articles []FeedArticle
	for _, item := range feed.Items {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.Link)
		if title == "" || link == "" {
			continue
		}
		articles = append(articles, FeedArticle{
			Title:         title,
			URL:           link,
			Description:   strings.TrimSpace(item.Description),
			PublishedDate: pickPublishedDate(item),
		})
	}

	return articles, nil
}

func DiscoverFeedURL(blogURL string, timeout time.Duration) (string, error) {
	return DiscoverFeedURLWithUserAgent(blogURL, timeout, "")
}

func DiscoverFeedURLWithUserAgent(blogURL string, timeout time.Duration, userAgent string) (string, error) {
	client := &http.Client{Timeout: timeout}
	response, err := sitehttp.Get(client, blogURL, userAgent)
	if err != nil {
		return "", fmt.Errorf("failed to fetch blog page: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("failed to fetch blog page: status %d", response.StatusCode)
	}

	base, err := url.Parse(blogURL)
	if err != nil {
		return "", fmt.Errorf("invalid blog URL: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return "", fmt.Errorf("failed to parse blog page: %w", err)
	}

	feedTypes := []string{
		"application/rss+xml",
		"application/atom+xml",
		"application/feed+json",
		"application/xml",
		"text/xml",
	}

	for _, feedType := range feedTypes {
		selection := doc.Find(fmt.Sprintf("link[rel='alternate'][type='%s']", feedType)).First()
		if selection.Length() == 0 {
			continue
		}
		href, exists := selection.Attr("href")
		if !exists {
			continue
		}
		resolved := resolveURL(base, href)
		if resolved != "" {
			return resolved, nil
		}
	}

	commonPaths := []string{
		"/feed",
		"/feed/",
		"/rss",
		"/rss/",
		"/feed.xml",
		"/rss.xml",
		"/atom.xml",
		"/index.xml",
	}

	for _, path := range commonPaths {
		resolved := resolveURL(base, path)
		if resolved == "" {
			continue
		}
		ok, err := isValidFeedWithUserAgent(resolved, timeout, userAgent)
		if err == nil && ok {
			return resolved, nil
		}
	}

	return "", nil
}

func isValidFeed(feedURL string, timeout time.Duration) (bool, error) {
	return isValidFeedWithUserAgent(feedURL, timeout, "")
}

func isValidFeedWithUserAgent(feedURL string, timeout time.Duration, userAgent string) (bool, error) {
	client := &http.Client{Timeout: timeout}
	response, err := sitehttp.Get(client, feedURL, userAgent)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, nil
	}

	parser := gofeed.NewParser()
	feed, err := parser.Parse(response.Body)
	if err != nil {
		return false, err
	}

	return len(feed.Items) > 0 || strings.TrimSpace(feed.Title) != "", nil
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

func pickPublishedDate(item *gofeed.Item) *time.Time {
	if item == nil {
		return nil
	}
	if item.PublishedParsed != nil {
		return item.PublishedParsed
	}
	if item.UpdatedParsed != nil {
		return item.UpdatedParsed
	}
	return nil
}

func IsFeedError(err error) bool {
	var parseErr FeedParseError
	return errors.As(err, &parseErr)
}

func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
}
