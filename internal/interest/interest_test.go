package interest

import (
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
