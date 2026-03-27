package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	DefaultModel           = "gpt-5.4-nano"
	DefaultMaxRequestBytes = 40960
	DefaultSystemPrompt    = `You are a concise blog article summarizer. Summarize the following article text in 100 to 400 words. Focus on the key points, main arguments, and conclusions. Ignore navigation, cookie/privacy/legal notices, login or registration prompts, subscription/paywall prompts, social-sharing UI, ads, and related/recent article lists if they appear in the text. Use clear, informative language. Do not include greetings, preamble, or meta-commentary; output only the summary text. Use the same language as the blog article.`
	DefaultInterestPrompt  = `You are classifying whether a blog article is worth prioritizing for the user. Return strict JSON with keys "state" and "reason". Allowed states are "prefer", "normal", and "hide". Use "prefer" for unusually relevant or high-signal articles that should be prioritized, "normal" for acceptable articles worth keeping visible, and "hide" for low-signal, repetitive, promotional, or clearly uninteresting articles. Keep "reason" under 25 words.`
)

type SummaryConfig struct {
	OpenAIAPIKey    string `toml:"openai_api_key"`
	Model           string `toml:"model"`
	SystemPrompt    string `toml:"system_prompt"`
	MaxRequestBytes int    `toml:"max_request_bytes"`
}

type DefaultsConfig struct {
	Model          string `toml:"model"`
	SystemPrompt   string `toml:"system_prompt"`
	InterestPrompt string `toml:"interest_prompt"`
}

type InterestBlogConfig struct {
	InterestPrompt string `toml:"interest_prompt"`
}

type InterestConfig struct {
	OpenAIAPIKey  string                        `toml:"openai_api_key"`
	Model         string                        `toml:"model"`
	SystemPrompt  string                        `toml:"system_prompt"`
	Prompt        string                        `toml:"prompt"`
	DefaultPrompt string                        `toml:"-"`
	Blogs         map[string]InterestBlogConfig `toml:"blogs"`
}

func (cfg InterestConfig) PromptForBlog(blogName string) string {
	prompt := cfg.DefaultPrompt
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
	Summary  SummaryConfig  `toml:"summary"`
	Defaults DefaultsConfig `toml:"defaults"`
	Interest InterestConfig `toml:"interest"`
}

func DefaultConfig() Config {
	return Config{
		Summary: SummaryConfig{
			Model:           DefaultModel,
			SystemPrompt:    DefaultSystemPrompt,
			MaxRequestBytes: DefaultMaxRequestBytes,
		},
		Defaults: DefaultsConfig{
			Model:        DefaultModel,
			SystemPrompt: DefaultInterestPrompt,
		},
		Interest: InterestConfig{
			Blogs: map[string]InterestBlogConfig{},
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

	if cfg.Summary.Model == "" {
		cfg.Summary.Model = DefaultModel
	}
	if cfg.Summary.SystemPrompt == "" {
		cfg.Summary.SystemPrompt = DefaultSystemPrompt
	}
	if cfg.Summary.MaxRequestBytes <= 0 {
		cfg.Summary.MaxRequestBytes = DefaultMaxRequestBytes
	}
	if cfg.Defaults.Model == "" {
		if cfg.Interest.Model != "" {
			cfg.Defaults.Model = cfg.Interest.Model
		} else {
			cfg.Defaults.Model = DefaultModel
		}
	}
	if cfg.Defaults.SystemPrompt == "" {
		if cfg.Interest.SystemPrompt != "" {
			cfg.Defaults.SystemPrompt = cfg.Interest.SystemPrompt
		} else {
			cfg.Defaults.SystemPrompt = DefaultInterestPrompt
		}
	}
	if cfg.Defaults.InterestPrompt == "" {
		cfg.Defaults.InterestPrompt = cfg.Interest.Prompt
	}
	cfg.Interest.Model = cfg.Defaults.Model
	cfg.Interest.SystemPrompt = cfg.Defaults.SystemPrompt
	cfg.Interest.DefaultPrompt = cfg.Defaults.InterestPrompt
	if cfg.Interest.Blogs == nil {
		cfg.Interest.Blogs = map[string]InterestBlogConfig{}
	}

	return cfg, nil
}
