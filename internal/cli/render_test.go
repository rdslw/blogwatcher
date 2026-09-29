package cli

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestWrapLineHangingIndent(t *testing.T) {
	got := wrapLine("alpha beta gamma delta epsilon", 20, "  Label: ", "    ")
	want := []string{
		"  Label: alpha beta",
		"    gamma delta",
		"    epsilon",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wrapLine() = %q, want %q", got, want)
	}
}

func TestWrapLineKeepsLongWordWhole(t *testing.T) {
	url := "https://example.com/a/very/long/path/that/does/not/fit"
	got := wrapLine("see "+url+" now", 20, "", "  ")
	want := []string{"see", "  " + url, "  now"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wrapLine() = %q, want %q", got, want)
	}
}

func TestWrapLineKeepsLabelWithValue(t *testing.T) {
	got := wrapLine("11 points, URL: https://news.example.com/item", 30, "HN: ", "  ")
	want := []string{"HN: 11 points,", "  URL: https://news.example.com/item"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wrapLine() = %q, want %q", got, want)
	}
}

func TestFormatTextBlockParagraphsAndLists(t *testing.T) {
	text := "First paragraph text.\nThe fixes:\n- one item that wraps onto a second line\n- two\n\n\nClosing paragraph."
	got := formatTextBlock(text, 30, "  ")
	want := []string{
		"  First paragraph text.",
		"",
		"  The fixes:",
		"  - one item that wraps onto a",
		"    second line",
		"  - two",
		"",
		"  Closing paragraph.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatTextBlock() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFormatTextBlockJoinsSoftWraps(t *testing.T) {
	text := "In the post we wrote about\nwhere this is going, and\nmore.\nRemote Control\nWhen you start a thread."
	got := formatTextBlock(text, 80, "")
	want := []string{
		"In the post we wrote about where this is going, and more.",
		"",
		"Remote Control",
		"",
		"When you start a thread.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatTextBlock() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFormatTextBlockRespectsWidth(t *testing.T) {
	text := strings.Repeat("word ", 200)
	for _, line := range formatTextBlock(text, 60, blockIndent) {
		if n := utf8.RuneCountInString(line); n > 60 {
			t.Fatalf("line has %d runes, want <= 60: %q", n, line)
		}
		if !strings.HasPrefix(line, blockIndent) {
			t.Fatalf("line is not indented: %q", line)
		}
	}
}

func TestFormatFieldCollapsesWhitespace(t *testing.T) {
	got := formatField("Reason", "short\nreason  text", 80)
	want := []string{fieldIndent + "Reason: short reason text"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatField() = %q, want %q", got, want)
	}
}

func TestFormatResultHeader(t *testing.T) {
	published := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		blog      string
		tags      []string
		published *time.Time
		want      string
	}{
		{name: "all parts", blog: "macrumors", tags: []string{"[new]", "[prefer]"}, published: &published, want: "[1] macrumors [new] [prefer] 2026-09-28 | Title"},
		{name: "empty tags skipped", blog: "macrumors", tags: []string{"", "[prefer]"}, published: &published, want: "[1] macrumors [prefer] 2026-09-28 | Title"},
		{name: "no date", blog: "macrumors", want: "[1] macrumors | Title"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatResultHeader("[1]", tt.blog, tt.tags, tt.published, "Title"); got != tt.want {
				t.Fatalf("formatResultHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}
