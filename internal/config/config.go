package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/rdslw/blogwatcher/internal/sitehttp"
)

const (
	DefaultModel                     = "gpt-5.4-nano"
	DefaultMaxRequestBytes           = 40960
	DefaultInterestMaxRequestBytes   = 12288
	DefaultHackerNewsMaxRequestBytes = 204800
	DefaultSystemPrompt              = `You are a concise blog article summarizer. Summarize the following article text in 80 to 320 words. Focus on the key points, main arguments, and conclusions. Ignore navigation, cookie/privacy/legal notices, login or registration prompts, subscription/paywall prompts, social-sharing UI, ads, and related/recent article lists if they appear in the text. Use clear, informative language. Do not include greetings, preamble, or meta-commentary; output only the summary text. Use the same language as the blog article.`
	DefaultInterestPrompt            = `You are classifying whether a blog article is worth prioritizing for the user. Return strict JSON with keys "state" and "reason". Allowed states are "prefer", "normal", and "hide". Use "prefer" for unusually relevant or high-signal articles that should be prioritized, "normal" for acceptable articles worth keeping visible, and "hide" for low-signal, repetitive, promotional, or clearly uninteresting articles. Keep "reason" under 25 words.`
	DefaultHackerNewsPrompt          = `You are analyzing a Hacker News discussion about one linked article. The input is a threaded comment tree in Path ID format.

Input rules:
- Each comment block is separated by a blank line.
- A comment starts with its path ID in brackets, then the author, then the comment text.
- Children are replies to a parent: [2.1] and [2.2] are replies to [2].
- Lower sibling numbers usually rank higher on Hacker News, so earlier siblings and especially [1] and direct replies to [1] often deserve more attention.
- No other metadata is guaranteed. Treat everything after the first colon as comment text.
- Comments and authors may be wrong or misleasing; do not assume correctness when analyzing.

Write a concise 80 to 320 word summary of the discussion for someone reading the article. Cover the main themes and conclusions, including substantive reactions, corrections, objections, useful technical details, and notable disagreements. Ignore jokes, low-effort praise, and off-topic tangents unless they explain the thread's reception. Output only the summary text.`
)

type SummaryConfig struct {
	OpenAIAPIKey              string `toml:"openai_api_key"`
	Model                     string `toml:"model"`
	SystemPrompt              string `toml:"system_prompt"`
	MaxRequestBytes           int    `toml:"max_request_bytes"`
	HackerNews                bool   `toml:"hackernews"`
	HackerNewsPrompt          string `toml:"hackernews_prompt"`
	HackerNewsMaxRequestBytes int    `toml:"hackernews_max_request_bytes"`
}

type InterestBlogConfig struct {
	InterestPrompt string `toml:"interest_prompt"`
}

type InterestConfig struct {
	OpenAIAPIKey    string                        `toml:"openai_api_key"`
	Model           string                        `toml:"model"`
	SystemPrompt    string                        `toml:"system_prompt"`
	MaxRequestBytes int                           `toml:"max_request_bytes"`
	InterestPrompt  string                        `toml:"interest_prompt"`
	Blogs           map[string]InterestBlogConfig `toml:"blogs"`
}

func (cfg InterestConfig) PromptForBlog(blogName string) string {
	prompt := cfg.InterestPrompt
	blogRule, ok := cfg.Blogs[blogName]
	if !ok {
		return prompt
	}
	if blogRule.InterestPrompt != "" {
		return blogRule.InterestPrompt
	}
	return prompt
}

type Config struct {
	UserAgent string         `toml:"user_agent"`
	Summary   SummaryConfig  `toml:"summary"`
	Interest  InterestConfig `toml:"interest"`
}

func DefaultConfig() Config {
	return Config{
		UserAgent: sitehttp.UserAgent(),
		Summary: SummaryConfig{
			Model:                     DefaultModel,
			SystemPrompt:              DefaultSystemPrompt,
			MaxRequestBytes:           DefaultMaxRequestBytes,
			HackerNewsPrompt:          DefaultHackerNewsPrompt,
			HackerNewsMaxRequestBytes: DefaultHackerNewsMaxRequestBytes,
		},
		Interest: InterestConfig{
			Model:           DefaultModel,
			SystemPrompt:    DefaultInterestPrompt,
			MaxRequestBytes: DefaultInterestMaxRequestBytes,
			Blogs:           map[string]InterestBlogConfig{},
		},
	}
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".blogwatcher", "config.toml"), nil
}

func Load() (Config, error) {
	cfg := DefaultConfig()

	path, err := configPath()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	cfg.UserAgent = sitehttp.ResolveUserAgent(cfg.UserAgent)
	if cfg.Summary.Model == "" {
		cfg.Summary.Model = DefaultModel
	}
	if cfg.Summary.SystemPrompt == "" {
		cfg.Summary.SystemPrompt = DefaultSystemPrompt
	}
	if cfg.Summary.MaxRequestBytes <= 0 {
		cfg.Summary.MaxRequestBytes = DefaultMaxRequestBytes
	}
	if cfg.Summary.HackerNewsPrompt == "" {
		cfg.Summary.HackerNewsPrompt = DefaultHackerNewsPrompt
	}
	if cfg.Summary.HackerNewsMaxRequestBytes <= 0 {
		cfg.Summary.HackerNewsMaxRequestBytes = DefaultHackerNewsMaxRequestBytes
	}
	if cfg.Interest.Model == "" {
		cfg.Interest.Model = DefaultModel
	}
	if cfg.Interest.SystemPrompt == "" {
		cfg.Interest.SystemPrompt = DefaultInterestPrompt
	}
	if cfg.Interest.MaxRequestBytes <= 0 {
		cfg.Interest.MaxRequestBytes = DefaultInterestMaxRequestBytes
	}
	if cfg.Interest.Blogs == nil {
		cfg.Interest.Blogs = map[string]InterestBlogConfig{}
	}

	return cfg, nil
}
