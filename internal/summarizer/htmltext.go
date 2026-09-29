package summarizer

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// blockElements start and end on their own line in extracted text.
var blockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"dd": true, "details": true, "div": true, "dl": true, "dt": true,
	"fieldset": true, "figcaption": true, "figure": true, "footer": true,
	"form": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "header": true, "hr": true, "li": true, "main": true,
	"nav": true, "ol": true, "p": true, "pre": true, "section": true,
	"summary": true, "table": true, "tr": true, "ul": true,
}

// skippedElements have no readable text.
var skippedElements = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true,
}

var whitespaceRun = regexp.MustCompile(`\s+`)

// selectionText extracts readable text from HTML nodes. Unlike
// goquery's Text(), it puts block elements and <br> on separate lines, so
// adjacent blocks such as "</p><div>" do not run together. Whitespace inside
// text collapses to one space, except in <pre>. List items get a "- " prefix.
// The result has one trimmed, non-empty line per block.
func selectionText(sel *goquery.Selection) string {
	var b strings.Builder
	for _, node := range sel.Nodes {
		writeNodeText(&b, node, false)
	}
	return cleanLines(b.String())
}

func writeNodeText(b *strings.Builder, n *html.Node, pre bool) {
	switch n.Type {
	case html.TextNode:
		if pre {
			b.WriteString(n.Data)
		} else {
			b.WriteString(whitespaceRun.ReplaceAllString(n.Data, " "))
		}
		return
	case html.ElementNode:
		if skippedElements[n.Data] {
			return
		}
		switch {
		case n.Data == "br":
			b.WriteString("\n")
			return
		case n.Data == "li":
			b.WriteString("\n- ")
		case n.Data == "td" || n.Data == "th":
			b.WriteString(" ")
		case blockElements[n.Data]:
			writeBlockBreak(b)
		}
		pre = pre || n.Data == "pre"
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeNodeText(b, c, pre)
	}
	if n.Type == html.ElementNode && blockElements[n.Data] {
		writeBlockBreak(b)
	}
}

// writeBlockBreak starts a new line, except directly after a list marker,
// so that "<li><p>text</p></li>" keeps the text on the marker line.
func writeBlockBreak(b *strings.Builder) {
	if strings.HasSuffix(b.String(), "\n- ") {
		return
	}
	b.WriteString("\n")
}

// cleanLines trims each line and drops empty lines. A list marker without
// text is dropped too.
func cleanLines(text string) string {
	var cleaned []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "-" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "- "); ok {
			line = "- " + strings.TrimSpace(rest)
		}
		cleaned = append(cleaned, line)
	}
	return strings.Join(cleaned, "\n")
}
