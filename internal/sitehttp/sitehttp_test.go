package sitehttp

import "testing"

func TestResolveUserAgent(t *testing.T) {
	if got := ResolveUserAgent(""); got != UserAgent() {
		t.Fatalf("expected default user agent %q, got %q", UserAgent(), got)
	}
	if got := ResolveUserAgent("  blogwatcher/v1.2.3 (+https://github.com/rdslw/blogwatcher)  "); got != "blogwatcher/v1.2.3 (+https://github.com/rdslw/blogwatcher)" {
		t.Fatalf("expected configured user agent, got %q", got)
	}
}
