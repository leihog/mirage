package ingest

import (
	"io"
	"net/mail"
	"slices"
	"sort"
	"strings"

	"github.com/leihog/mirage/internal/mailbox"
	mailmime "github.com/leihog/mirage/internal/mime"
)

func ParseRaw(raw []byte) (mailbox.Message, error) {
	parsed, err := mailmime.Parse(raw)
	if err != nil {
		return mailbox.Message{}, err
	}

	msg := mailbox.Message{
		From:        parsed.From,
		To:          parsed.To,
		Cc:          parsed.Cc,
		Bcc:         parsed.Bcc,
		Subject:     parsed.Subject,
		Text:        parsed.Text,
		HTML:        parsed.HTML,
		Headers:     submittedHeaders(parsed.Headers),
		Attachments: make([]mailbox.Attachment, 0, len(parsed.Attachments)),
	}
	for _, attachment := range parsed.Attachments {
		msg.Attachments = append(msg.Attachments, mailbox.Attachment{
			Name:        attachment.Name,
			ContentType: attachment.ContentType,
			Size:        attachment.Size,
			ContentID:   attachment.ContentID,
			Inline:      attachment.Inline,
			Data:        attachment.Data,
		})
	}
	return msg, nil
}

func SanitizeRaw(raw []byte) []byte {
	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return raw
	}

	var out strings.Builder
	keys := make([]string, 0, len(parsed.Header))
	for key := range parsed.Header {
		if blockedSubmittedHeader(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range parsed.Header[key] {
			out.WriteString(key)
			out.WriteString(": ")
			out.WriteString(value)
			out.WriteString("\r\n")
		}
	}
	out.WriteString("\r\n")
	_, _ = io.Copy(&out, parsed.Body)
	return []byte(out.String())
}

func submittedHeaders(headers map[string][]string) map[string][]string {
	out := map[string][]string{}
	for key, values := range headers {
		if blockedSubmittedHeader(key) {
			continue
		}
		out[key] = slices.Clone(values)
	}
	return out
}

func blockedSubmittedHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "delivered-to", "received", "return-path":
		return true
	default:
		return false
	}
}
