package config

import "testing"

func TestDefaultConfigLeavesInterestPromptEmpty(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Interest.InterestPrompt != "" {
		t.Fatalf("expected default interest prompt to be empty, got %q", cfg.Interest.InterestPrompt)
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
