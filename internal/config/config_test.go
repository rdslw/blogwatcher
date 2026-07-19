package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rdslw/blogwatcher/internal/sitehttp"
)

func TestDefaultConfigLeavesInterestPromptEmpty(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.UserAgent != sitehttp.UserAgent() {
		t.Fatalf("expected default user agent %q, got %q", sitehttp.UserAgent(), cfg.UserAgent)
	}
	if cfg.Interest.InterestPrompt != "" {
		t.Fatalf("expected default interest prompt to be empty, got %q", cfg.Interest.InterestPrompt)
	}
	if cfg.Summary.HackerNews {
		t.Fatalf("expected Hacker News enrichment disabled by default")
	}
	if cfg.Summary.HackerNewsPrompt == "" {
		t.Fatalf("expected default Hacker News prompt")
	}
	if cfg.Summary.HackerNewsMaxRequestBytes != DefaultHackerNewsMaxRequestBytes {
		t.Fatalf("expected default HN max request bytes %d, got %d", DefaultHackerNewsMaxRequestBytes, cfg.Summary.HackerNewsMaxRequestBytes)
	}
	if cfg.Interest.MaxRequestBytes != DefaultInterestMaxRequestBytes {
		t.Fatalf("expected default interest max request bytes %d, got %d", DefaultInterestMaxRequestBytes, cfg.Interest.MaxRequestBytes)
	}
}

func TestLoadUsesConfiguredUserAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".blogwatcher")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("user_agent = \"blogwatcher/v1.2.3 (+https://github.com/rdslw/blogwatcher)\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.UserAgent != "blogwatcher/v1.2.3 (+https://github.com/rdslw/blogwatcher)" {
		t.Fatalf("expected configured user agent, got %q", cfg.UserAgent)
	}
}

func TestInterestPromptForBlogUsesOverrideWhenPresent(t *testing.T) {
	cfg := InterestConfig{
		InterestPrompt: "Default prompt",
		Blogs: map[string]InterestBlogConfig{
			"Prompt Blog": {
				InterestPrompt: "Custom prompt",
			},
			"Empty Blog": {},
		},
	}

	if got := cfg.PromptForBlog("Missing"); got != "Default prompt" {
		t.Fatalf("expected default prompt, got %q", got)
	}
	if got := cfg.PromptForBlog("Prompt Blog"); got != "Custom prompt" {
		t.Fatalf("expected custom prompt, got %q", got)
	}
	if got := cfg.PromptForBlog("Empty Blog"); got != "Default prompt" {
		t.Fatalf("expected fallback default prompt, got %q", got)
	}
}
