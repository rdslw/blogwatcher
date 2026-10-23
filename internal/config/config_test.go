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

func TestLoadResolvesJinaFetch(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		env      string
		wantKey  string
		wantMode string
		wantErr  string
	}{
		{name: "no key defaults to none", wantMode: JinaFetchNone},
		{name: "config key defaults to primary", config: `jina_api_key = "cfg-key"`, wantKey: "cfg-key", wantMode: JinaFetchPrimary},
		{name: "env key defaults to primary", env: "env-key", wantKey: "env-key", wantMode: JinaFetchPrimary},
		{name: "env key wins over config key", config: `jina_api_key = "cfg-key"`, env: "env-key", wantKey: "env-key", wantMode: JinaFetchPrimary},
		{name: "explicit none wins over key", config: "jina_api_key = \"cfg-key\"\njina_fetch = \"none\"", wantKey: "cfg-key", wantMode: JinaFetchNone},
		{name: "explicit fallback", config: "jina_fetch = \"Fallback\"", env: "env-key", wantKey: "env-key", wantMode: JinaFetchFallback},
		{name: "explicit primary without key", config: `jina_fetch = "primary"`, wantErr: `jina_fetch = "primary" requires jina_api_key or JINA_API_KEY`},
		{name: "explicit fallback without key", config: `jina_fetch = "fallback"`, wantErr: `jina_fetch = "fallback" requires jina_api_key or JINA_API_KEY`},
		{name: "invalid mode", config: `jina_fetch = "always"`, env: "env-key", wantErr: `invalid jina_fetch "always" (use none, primary, or fallback)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("JINA_API_KEY", tt.env)
			if tt.config != "" {
				configDir := filepath.Join(home, ".blogwatcher")
				if err := os.MkdirAll(configDir, 0o755); err != nil {
					t.Fatalf("create config directory: %v", err)
				}
				if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(tt.config+"\n"), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}

			cfg, err := Load()
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			if cfg.JinaAPIKey != tt.wantKey {
				t.Fatalf("expected Jina key %q, got %q", tt.wantKey, cfg.JinaAPIKey)
			}
			if cfg.JinaFetch != tt.wantMode {
				t.Fatalf("expected jina_fetch %q, got %q", tt.wantMode, cfg.JinaFetch)
			}
		})
	}
}
