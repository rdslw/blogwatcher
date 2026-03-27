package interest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rdslw/blogwatcher/internal/config"
	"github.com/rdslw/blogwatcher/internal/model"
)

const (
	EngineOpenAI   = "openai"
	defaultTimeout = 60 * time.Second
	openAIAPIURL   = "https://api.openai.com/v1/chat/completions"
)

type Options struct {
	OpenAIAPIKey string
	Model        string
	SystemPrompt string
}

type Result struct {
	State  string
	Reason string
	Engine string
}

func OptionsFromConfig(cfg config.InterestConfig) Options {
	return Options{
		OpenAIAPIKey: cfg.OpenAIAPIKey,
		Model:        cfg.Model,
		SystemPrompt: cfg.SystemPrompt,
	}
}

func ClassifySummary(blogName string, summary string, prompt string, opts Options) (Result, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return Result{}, fmt.Errorf("cannot classify empty summary")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Result{}, fmt.Errorf("interest classification requires a non-empty interest_prompt")
	}

	apiKey := resolveAPIKey(opts)
	if apiKey == "" {
		return Result{}, fmt.Errorf("interest classification requires OPENAI_API_KEY or interest.openai_api_key")
	}

	modelName := opts.Model
	if modelName == "" {
		modelName = config.DefaultModel
	}
	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = config.DefaultInterestPrompt
	}

	reqBody := chatRequest{
		Model: modelName,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: buildUserPrompt(blogName, summary, prompt)},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return Result{}, fmt.Errorf("failed to marshal request: %v", err)
	}

	req, err := http.NewRequest("POST", openAIAPIURL, bytes.NewReader(jsonData))
	if err != nil {
		return Result{}, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: defaultTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("OpenAI API request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("failed to read OpenAI response: %v", err)
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return Result{}, fmt.Errorf("failed to parse OpenAI response: %v", err)
	}
	if chatResp.Error != nil {
		return Result{}, fmt.Errorf("OpenAI API error: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return Result{}, fmt.Errorf("OpenAI returned no choices")
	}

	result, err := parseClassification(chatResp.Choices[0].Message.Content)
	if err != nil {
		return Result{}, err
	}
	result.Engine = EngineOpenAI
	return result, nil
}

func buildUserPrompt(blogName string, summary string, prompt string) string {
	var b strings.Builder
	b.WriteString("Blog: ")
	if blogName == "" {
		b.WriteString("unknown")
	} else {
		b.WriteString(blogName)
	}
	b.WriteString("\n\n")
	b.WriteString("Classification policy:\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n")
	b.WriteString("\nArticle summary:\n")
	b.WriteString(summary)
	return b.String()
}

func parseClassification(raw string) (Result, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return Result{}, fmt.Errorf("interest classifier returned empty response")
	}

	type response struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	}

	var parsed response
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		start := strings.IndexByte(text, '{')
		end := strings.LastIndexByte(text, '}')
		if start < 0 || end <= start {
			return Result{}, fmt.Errorf("interest classifier returned invalid JSON: %q", text)
		}
		if err := json.Unmarshal([]byte(text[start:end+1]), &parsed); err != nil {
			return Result{}, fmt.Errorf("interest classifier returned invalid JSON: %q", text)
		}
	}

	parsed.State = strings.TrimSpace(strings.ToLower(parsed.State))
	parsed.Reason = strings.TrimSpace(parsed.Reason)
	if !model.IsValidInterestState(parsed.State) {
		return Result{}, fmt.Errorf("interest classifier returned invalid state %q", parsed.State)
	}

	return Result{
		State:  parsed.State,
		Reason: parsed.Reason,
	}, nil
}

func resolveAPIKey(opts Options) string {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return key
	}
	return opts.OpenAIAPIKey
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}
