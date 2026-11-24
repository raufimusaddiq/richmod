package emailingress

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"

	"golang.org/x/net/html"
)

type ParsedEmail struct {
	Sender                       string
	Subject                      string
	Date                         string
	MessageID                    string
	AuthenticationResults        string
	ARCAuthenticationResults     string
	AuthenticationResultsHeaders []string
	HTMLBody                     string
	TextBody                     string
}

const (
	maxMIMEDepth          = 8
	maxMIMEParts          = 64
	maxSubjectBytes       = 1024
	maxDateBytes          = 128
	maxMessageIDBytes     = 998
	maxAuthResultsBytes   = 16 << 10
	maxStoredBodyBytes    = 512 << 10
	maxAuthResultsHeaders = 16
)

func boundText(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	return strings.ToValidUTF8(value[:maxBytes], "")
}

func parseMIME(raw []byte) (ParsedEmail, error) {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ParsedEmail{}, fmt.Errorf("parse MIME: %w", err)
	}
	if len(message.Header["From"]) != 1 {
		return ParsedEmail{}, fmt.Errorf("email must have exactly one From header")
	}
	sender, err := parseAddress(message.Header.Get("From"))
	if err != nil {
		return ParsedEmail{}, err
	}
	parsed := ParsedEmail{
		Sender: sender, Subject: boundText(decodeHeader(message.Header.Get("Subject")), maxSubjectBytes), Date: boundText(message.Header.Get("Date"), maxDateBytes),
		MessageID:                    boundText(strings.TrimSpace(message.Header.Get("Message-ID")), maxMessageIDBytes),
		AuthenticationResults:        boundText(joinHeader(message.Header, "Authentication-Results"), maxAuthResultsBytes),
		ARCAuthenticationResults:     boundText(joinHeader(message.Header, "ARC-Authentication-Results"), maxAuthResultsBytes),
		AuthenticationResultsHeaders: message.Header[textproto.CanonicalMIMEHeaderKey("Authentication-Results")],
	}
	if len(parsed.AuthenticationResultsHeaders) > maxAuthResultsHeaders {
		parsed.AuthenticationResultsHeaders = parsed.AuthenticationResultsHeaders[:maxAuthResultsHeaders]
	}
	contentType := message.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}
	budget := maxMIMEParts
	htmlBody, textBody, err := parsePart(contentType, message.Header.Get("Content-Transfer-Encoding"), message.Body, 0, &budget)
	if err != nil {
		return ParsedEmail{}, err
	}
	parsed.HTMLBody, parsed.TextBody = htmlBody, textBody
	if strings.TrimSpace(parsed.HTMLBody) == "" && strings.TrimSpace(parsed.TextBody) == "" {
		return ParsedEmail{}, fmt.Errorf("email has no text body")
	}
	return parsed, nil
}

func decodeHeader(value string) string {
	decoded, err := new(mime.WordDecoder).DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}

func joinHeader(header mail.Header, name string) string {
	return strings.Join(header[textproto.CanonicalMIMEHeaderKey(name)], "\n")
}

func parsePart(contentType, transfer string, body io.Reader, depth int, budget *int) (string, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", "", fmt.Errorf("invalid content type")
	}
	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		if depth >= maxMIMEDepth {
			return "", "", fmt.Errorf("multipart email nested too deeply")
		}
		boundary := params["boundary"]
		if boundary == "" {
			return "", "", fmt.Errorf("multipart email missing boundary")
		}
		reader := multipart.NewReader(body, boundary)
		var htmlBody, textBody string
		for {
			part, nextErr := reader.NextPart()
			if nextErr == io.EOF {
				break
			}
			if nextErr != nil {
				return "", "", fmt.Errorf("read multipart email")
			}
			if *budget--; *budget < 0 {
				_ = part.Close()
				return "", "", fmt.Errorf("multipart email has too many parts")
			}
			htmlValue, textValue, partErr := parsePart(part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"), part, depth+1, budget)
			_ = part.Close()
			if partErr != nil {
				continue
			}
			if htmlBody == "" {
				htmlBody = htmlValue
			}
			if textBody == "" {
				textBody = textValue
			}
		}
		return htmlBody, textBody, nil
	}
	if !strings.EqualFold(mediaType, "text/html") && !strings.EqualFold(mediaType, "text/plain") {
		return "", "", nil
	}
	decoded, err := decodeTransfer(body, transfer)
	if err != nil {
		return "", "", err
	}
	if strings.EqualFold(mediaType, "text/html") {
		return string(decoded), "", nil
	}
	return "", string(decoded), nil
}

func decodeTransfer(body io.Reader, transfer string) ([]byte, error) {
	var reader io.Reader = body
	switch strings.ToLower(strings.TrimSpace(transfer)) {
	case "base64":
		reader = base64.NewDecoder(base64.StdEncoding, body)
	case "quoted-printable":
		reader = quotedprintable.NewReader(body)
	}
	return io.ReadAll(io.LimitReader(reader, 2<<20))
}

func hiddenElement(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}
	switch node.Data {
	case "style", "script", "template", "noscript", "head":
		return true
	}
	return false
}

func visibleHTML(value string) string {
	document, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return ""
	}
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if hiddenElement(node) || out.Len() > maxStoredBodyBytes*2 {
			return
		}
		if node.Type == html.TextNode {
			out.WriteString(node.Data)
			out.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return boundText(strings.Join(strings.Fields(out.String()), " "), maxStoredBodyBytes)
}
