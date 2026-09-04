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
	Sender                   string
	Subject                  string
	Date                     string
	MessageID                string
	AuthenticationResults    string
	ARCAuthenticationResults string
	HTMLBody                 string
	TextBody                 string
}

func parseMIME(raw []byte) (ParsedEmail, error) {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ParsedEmail{}, fmt.Errorf("parse MIME: %w", err)
	}
	sender, err := parseAddress(message.Header.Get("From"))
	if err != nil {
		return ParsedEmail{}, err
	}
	parsed := ParsedEmail{
		Sender: sender, Subject: decodeHeader(message.Header.Get("Subject")), Date: message.Header.Get("Date"),
		MessageID:                strings.TrimSpace(message.Header.Get("Message-ID")),
		AuthenticationResults:    joinHeader(message.Header, "Authentication-Results"),
		ARCAuthenticationResults: joinHeader(message.Header, "ARC-Authentication-Results"),
	}
	contentType := message.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}
	htmlBody, textBody, err := parsePart(contentType, message.Header.Get("Content-Transfer-Encoding"), message.Body)
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

func parsePart(contentType, transfer string, body io.Reader) (string, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", "", fmt.Errorf("invalid content type")
	}
	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
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
			htmlValue, textValue, partErr := parsePart(part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"), part)
			_ = part.Close()
			if partErr != nil {
				continue // attachments or malformed optional alternatives are ignored.
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

func visibleHTML(value string) string {
	document, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return ""
	}
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			out.WriteString(node.Data)
			out.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return strings.Join(strings.Fields(out.String()), " ")
}
