package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fatih/color"
	"golang.org/x/term"
)

const (
	// fieldIndent aligns detail lines under the article title.
	fieldIndent = "       "
	// blockIndent indents wrapped continuation lines and text blocks.
	blockIndent = "         "
	// maxTextWidth caps line length on wide terminals to keep prose readable.
	maxTextWidth = 100
	minTextWidth = 40
	// displayTimeLayout is the format of all timestamps in human output.
	displayTimeLayout = "2006-01-02 15:04"
)

var listMarkerPattern = regexp.MustCompile(`^([-*•]|\d+[.)])\s+`)

// textWidth returns the line width for wrapped output: the terminal width
// capped at maxTextWidth, or maxTextWidth when stdout is not a terminal.
func textWidth() int {
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return maxTextWidth
	}
	width, _, err := term.GetSize(fd)
	if err != nil || width <= 0 {
		return maxTextWidth
	}
	return min(max(width-1, minTextWidth), maxTextWidth)
}

// formatResultHeader builds the "[id] blog [tags] date | title" line. The
// date is the published date; without one, it is the discovered date with a
// "~" marker (the same fallback as --since).
func formatResultHeader(id string, blogName string, tags []string, published *time.Time, discovered *time.Time, title string) string {
	parts := []string{id}
	if blogName != "" {
		parts = append(parts, blogName)
	}
	for _, tag := range tags {
		if tag != "" {
			parts = append(parts, tag)
		}
	}
	switch {
	case published != nil:
		parts = append(parts, published.Format("2006-01-02"))
	case discovered != nil:
		parts = append(parts, "~"+discovered.Local().Format("2006-01-02"))
	}
	return strings.Join(parts, " ") + " | " + title
}

// formatDisplayTime formats a stored timestamp in local time. Stored values
// mix UTC and local offsets, so they are converted before formatting.
func formatDisplayTime(t time.Time) string {
	return t.Local().Format(displayTimeLayout)
}

// formatSizeLabel describes a text size in characters and words.
func formatSizeLabel(text string) string {
	return fmt.Sprintf("%d chars, %d words", len(text), len(strings.Fields(text)))
}

// printField prints one "Label: value" detail line, wrapping long values
// with a hanging indent.
func printField(label string, value string) {
	for _, line := range formatField(label, value, textWidth()) {
		fmt.Println(line)
	}
}

// printNoteField prints a wrapped detail line in the warning color.
func printNoteField(label string, value string) {
	yellow := color.New(color.FgYellow)
	for _, line := range formatField(label, value, textWidth()) {
		yellow.Println(line)
	}
}

// printTextBlock prints a label line followed by the text as indented,
// wrapped paragraphs.
func printTextBlock(label string, text string) {
	fmt.Printf("%s%s:\n", fieldIndent, label)
	for _, line := range formatTextBlock(text, textWidth(), blockIndent) {
		fmt.Println(line)
	}
}

func formatField(label string, value string, width int) []string {
	value = strings.Join(strings.Fields(value), " ")
	return wrapLine(value, width, fieldIndent+label+": ", blockIndent)
}

// formatTextBlock wraps each source paragraph. Paragraphs are separated by a
// blank line, except inside lists and between a lead-in line ending with ":"
// and the list that follows it.
func formatTextBlock(text string, width int, indent string) []string {
	var out []string
	prevList := false
	prevLeadIn := false
	for _, line := range joinSoftWraps(strings.Split(text, "\n")) {
		if line == "" {
			continue
		}
		marker := listMarkerPattern.FindString(line)
		isList := marker != ""
		if len(out) > 0 && !(isList && (prevList || prevLeadIn)) {
			out = append(out, "")
		}
		if isList {
			body := strings.TrimSpace(line[len(marker):])
			bullet := strings.TrimSpace(marker) + " "
			out = append(out, wrapLine(body, width, indent+bullet, indent+strings.Repeat(" ", utf8.RuneCountInString(bullet)))...)
		} else {
			out = append(out, wrapLine(line, width, indent, indent)...)
		}
		prevList = isList
		prevLeadIn = strings.HasSuffix(line, ":")
	}
	return out
}

// joinSoftWraps trims lines and joins hard-wrapped lines of one paragraph.
// Feed text often uses a single newline both for paragraph breaks and for
// wrapping, so a line continues into the next one only when it has no
// sentence-end punctuation and the next line starts in lowercase.
func joinSoftWraps(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if n := len(out); n > 0 && isSoftWrap(out[n-1], line) {
			out[n-1] += " " + line
			continue
		}
		out = append(out, line)
	}
	return out
}

func isSoftWrap(prev string, next string) bool {
	if prev == "" || next == "" || listMarkerPattern.MatchString(next) {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(prev)
	if strings.ContainsRune(".!?:;\"”…", last) {
		return false
	}
	first, _ := utf8.DecodeRuneInString(next)
	return unicode.IsLower(first)
}

func joinLabelWords(words []string) []string {
	out := make([]string, 0, len(words))
	for i := 0; i < len(words); i++ {
		word := words[i]
		if strings.HasSuffix(word, ":") && i+1 < len(words) {
			i++
			word += " " + words[i]
		}
		out = append(out, word)
	}
	return out
}

// wrapLine greedily wraps text into lines of at most width runes. Words
// longer than the available space (for example URLs) stay on one line, and a
// label word ending with ":" stays on the same line as the word after it.
func wrapLine(text string, width int, firstPrefix string, restPrefix string) []string {
	words := joinLabelWords(strings.Fields(text))
	if len(words) == 0 {
		return []string{strings.TrimRight(firstPrefix, " ")}
	}
	var lines []string
	current := firstPrefix + words[0]
	currentLen := utf8.RuneCountInString(current)
	for _, word := range words[1:] {
		wordLen := utf8.RuneCountInString(word)
		if currentLen+1+wordLen > width {
			lines = append(lines, current)
			current = restPrefix + word
			currentLen = utf8.RuneCountInString(current)
			continue
		}
		current += " " + word
		currentLen += 1 + wordLen
	}
	return append(lines, current)
}
