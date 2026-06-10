package web

import (
	"strings"

	"github.com/leihog/mirage/internal/mailbox"
	mailmime "github.com/leihog/mirage/internal/mime"
)

func messageBody(msg mailbox.Message, part string) (string, string, bool) {
	switch part {
	case "text":
		if strings.TrimSpace(msg.Text) == "" {
			return "", "", false
		}
		return msg.Text, "text/plain; charset=utf-8", true
	case "html":
		if strings.TrimSpace(msg.HTML) == "" {
			return "", "", false
		}
		return msg.HTML, "text/html; charset=utf-8", true
	case "raw":
		if len(msg.Raw) > 0 {
			return string(msg.Raw), "message/rfc822", true
		}
		raw := rawMessage(msg)
		if strings.TrimSpace(raw) == "" {
			return "", "", false
		}
		return raw, "message/rfc822", true
	default:
		return "", "", false
	}
}

func messageBodyFilename(msg mailbox.Message, part string) string {
	extension := map[string]string{
		"html": "html",
		"text": "txt",
		"raw":  "eml",
	}[part]
	if extension == "" {
		extension = "txt"
	}
	return "message-" + mailmime.SafeFilenamePart(msg.ID) + "." + extension
}

func rawMessage(msg mailbox.Message) string {
	if len(msg.Raw) > 0 {
		return string(msg.Raw)
	}
	return mailmime.Generate(mailmime.Message{
		From:        msg.From,
		To:          msg.To,
		Cc:          msg.Cc,
		Bcc:         msg.Bcc,
		Subject:     msg.Subject,
		Headers:     msg.Headers,
		Text:        msg.Text,
		HTML:        msg.HTML,
		Attachments: mimeAttachments(msg.Attachments),
	}, mailmime.GenerateOptions{
		ID:        msg.ID,
		CreatedAt: msg.CreatedAt,
	})
}

func mimeAttachments(attachments []mailbox.Attachment) []mailmime.Attachment {
	out := make([]mailmime.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		out = append(out, mailmime.Attachment{
			Name:        attachment.Name,
			ContentType: attachment.ContentType,
			Size:        attachment.Size,
			ContentID:   attachment.ContentID,
			Inline:      attachment.Inline,
			Data:        attachment.Data,
		})
	}
	return out
}

func stringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
