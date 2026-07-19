package sitehttp

import (
	"net/http"
	"strings"

	"github.com/rdslw/blogwatcher/internal/version"
)

const projectURL = "https://github.com/rdslw/blogwatcher"

// UserAgent identifies Blogwatcher to sites instead of using Go's generic
// default, which some CDNs reject.
func UserAgent() string {
	return "blogwatcher/" + version.Version + " (+" + projectURL + ")"
}

// ResolveUserAgent returns a configured site identity or Blogwatcher's default.
func ResolveUserAgent(configured string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	return UserAgent()
}

// SetUserAgent applies the standard identity used for requests to blog sites.
func SetUserAgent(req *http.Request, configured string) {
	req.Header.Set("User-Agent", ResolveUserAgent(configured))
}

// Get performs a GET request with Blogwatcher's standard site identity.
func Get(client *http.Client, url string, userAgent string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	SetUserAgent(req, userAgent)
	return client.Do(req)
}
