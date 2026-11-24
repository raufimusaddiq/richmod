package gateway

import (
	"html"
	"regexp"
	"strings"
	"sync"
)

const (
	DefaultEmailBodyMaxBytes    = 32 << 10
	DefaultEmailSubjectMaxBytes = 1024
)

var (
	htmlTagPattern      = regexp.MustCompile(`(?s)<[^>]*>`)
	htmlBlockPattern    = regexp.MustCompile(`(?is)<!--.*?(?:-->|\z)|<style\b[^>]*>.*?(?:</style\s*>|\z)|<script\b[^>]*>.*?(?:</script\s*>|\z)`)
	untrustedTagPattern = regexp.MustCompile(`(?i)<(\s*/?\s*(?:untrusted_|evidence\b))`)
	customTagPatterns   sync.Map
)

func customTagPattern(tag string) *regexp.Regexp {
	key := strings.ToLower(tag)
	if cached, ok := customTagPatterns.Load(key); ok {
		return cached.(*regexp.Regexp)
	}
	compiled := regexp.MustCompile(`(?i)<(\s*/?\s*` + regexp.QuoteMeta(tag) + `\b)`)
	actual, _ := customTagPatterns.LoadOrStore(key, compiled)
	return actual.(*regexp.Regexp)
}

func WrapUntrusted(tag, value string) string {
	escaped := untrustedTagPattern.ReplaceAllString(value, "&lt;$1")
	if tag != "" && !strings.HasPrefix(strings.ToLower(tag), "untrusted_") && !strings.EqualFold(tag, "evidence") {
		escaped = customTagPattern(tag).ReplaceAllString(escaped, "&lt;$1")
	}
	return "<" + tag + ">" + escaped + "</" + tag + ">"
}

func NormalizeEmailBody(raw string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultEmailBodyMaxBytes
	}
	visible := html.UnescapeString(htmlTagPattern.ReplaceAllString(htmlBlockPattern.ReplaceAllString(raw, " "), " "))
	visible = strings.Join(strings.Fields(visible), " ")
	if len(visible) <= maxBytes {
		return visible
	}
	return strings.ToValidUTF8(visible[:maxBytes], "")
}
