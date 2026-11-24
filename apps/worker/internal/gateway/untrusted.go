package gateway

import (
	"html"
	"regexp"
	"strings"
)

const DefaultEmailBodyMaxBytes = 32 << 10

var (
	htmlTagPattern      = regexp.MustCompile(`(?s)<[^>]*>`)
	untrustedTagPattern = regexp.MustCompile(`(?i)<(\s*/?\s*(?:untrusted_|evidence\b))`)
)

func WrapUntrusted(tag, value string) string {
	escaped := untrustedTagPattern.ReplaceAllString(value, "&lt;$1")
	if tag != "" && !strings.HasPrefix(strings.ToLower(tag), "untrusted_") && !strings.EqualFold(tag, "evidence") {
		custom := regexp.MustCompile(`(?i)<(\s*/?\s*` + regexp.QuoteMeta(tag) + `\b)`)
		escaped = custom.ReplaceAllString(escaped, "&lt;$1")
	}
	return "<" + tag + ">" + escaped + "</" + tag + ">"
}

func NormalizeEmailBody(raw string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultEmailBodyMaxBytes
	}
	visible := html.UnescapeString(htmlTagPattern.ReplaceAllString(raw, " "))
	visible = strings.Join(strings.Fields(visible), " ")
	if len(visible) <= maxBytes {
		return visible
	}
	return strings.ToValidUTF8(visible[:maxBytes], "")
}
