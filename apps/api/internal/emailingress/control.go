package emailingress

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

const gmailForwardingSubject = "gmail forwarding confirmation - receive mail from "

var (
	httpsURLPattern       = regexp.MustCompile(`https://[^\s<>"']+`)
	confirmationCodeRegex = regexp.MustCompile(`(?i)confirmation code(?:\s+is)?\s*:?\s*([0-9]{6,20})`)
)

type controlAction struct {
	IntegrationType string
	ActionType      string
	Title           string
	Description     string
	ActionURL       string
	ActionCode      string
	DedupeKey       string
	SourceMailbox   string
}

func detectControlAction(email ParsedEmail, contentHash [32]byte) (controlAction, bool) {
	if email.Sender != "forwarding-noreply@google.com" {
		return controlAction{}, false
	}
	subject := strings.TrimSpace(strings.TrimLeft(email.Subject, "(["))
	if !strings.HasPrefix(strings.ToLower(subject), gmailForwardingSubject) {
		return controlAction{}, false
	}
	body := email.TextBody + "\n" + visibleHTML(email.HTMLBody)
	actionURL := firstAllowedGmailVerificationURL(email.HTMLBody, body)
	code := ""
	if match := confirmationCodeRegex.FindStringSubmatch(body); len(match) == 2 {
		code = match[1]
	}
	dedupeSource := strings.TrimSpace(email.MessageID)
	if dedupeSource == "" {
		dedupeSource = hex.EncodeToString(contentHash[:])
	}
	dedupeHash := sha256.Sum256([]byte("gmail-forwarding-confirmation\n" + dedupeSource))
	return controlAction{
		IntegrationType: "EMAIL_FORWARDING",
		ActionType:      "VERIFY_FORWARDING",
		Title:           "Verifikasi penerusan email",
		Description:     "Google meminta konfirmasi sebelum email dapat diteruskan ke Richmod.",
		ActionURL:       actionURL,
		ActionCode:      code,
		DedupeKey:       hex.EncodeToString(dedupeHash[:]),
		SourceMailbox:   strings.TrimSpace(subject[len(gmailForwardingSubject):]),
	}, true
}

func firstAllowedGmailVerificationURL(htmlBody, text string) string {
	for _, candidate := range htmlLinks(htmlBody) {
		if parsed, err := url.Parse(candidate); err == nil && isAllowedGmailVerificationURL(*parsed) {
			return parsed.String()
		}
	}
	for _, candidate := range httpsURLPattern.FindAllString(text, -1) {
		candidate = strings.TrimRight(candidate, ").,;]")
		if parsed, err := url.Parse(candidate); err == nil && isAllowedGmailVerificationURL(*parsed) {
			return parsed.String()
		}
	}
	return ""
}

func htmlLinks(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	doc, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return nil
	}
	var links []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			for _, attr := range node.Attr {
				if strings.EqualFold(attr.Key, "href") {
					links = append(links, strings.TrimSpace(attr.Val))
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return links
}

func isAllowedGmailVerificationURL(candidate url.URL) bool {
	if candidate.Scheme != "https" || candidate.User != nil || candidate.Fragment != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(candidate.Hostname(), "."))
	if host != "mail-settings.google.com" && host != "mail.google.com" {
		return false
	}
	if port := candidate.Port(); port != "" && port != "443" {
		return false
	}
	return strings.HasPrefix(candidate.EscapedPath(), "/mail/vf-")
}
