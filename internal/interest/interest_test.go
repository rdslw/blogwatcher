package interest

import (
	"strings"
	"testing"

	"github.com/rdslw/blogwatcher/internal/model"
)

func TestParseClassificationAcceptsJSONWrappedInText(t *testing.T) {
	result, err := parseClassification("```json\n{\"state\":\"prefer\",\"reason\":\"Compiler internals\"}\n```")
	if err != nil {
		t.Fatalf("parse classification: %v", err)
	}
	if result.State != model.InterestStatePrefer {
		t.Fatalf("expected prefer state, got %q", result.State)
	}
	if result.Reason != "Compiler internals" {
		t.Fatalf("expected reason, got %q", result.Reason)
	}
}

func TestParseClassificationRejectsInvalidState(t *testing.T) {
	if _, err := parseClassification(`{"state":"unknown","reason":"x"}`); err == nil {
		t.Fatalf("expected invalid state error")
	}
}

func TestClassifySummaryRejectsEmptyPrompt(t *testing.T) {
	if _, err := ClassifySummary("Blog", "Summary", "", Options{}); err == nil {
		t.Fatalf("expected empty prompt error")
	}
}

func TestBuildUserPromptUsesTruncatedSummaryInput(t *testing.T) {
	summary := truncateUTF8ToBytes("abcdef", 3)
	prompt := buildUserPrompt("Blog", summary, "Prefer useful posts.")
	if !strings.Contains(prompt, "Article summary:\nabc") {
		t.Fatalf("expected truncated summary in prompt, got %q", prompt)
	}
	if strings.Contains(prompt, "abcdef") {
		t.Fatalf("expected original summary to be truncated")
	}
}
