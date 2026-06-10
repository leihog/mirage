package web

import (
	stdmime "mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/leihog/mirage/internal/mailbox"
	mailmime "github.com/leihog/mirage/internal/mime"
)

var cidURLPattern = regexp.MustCompile(`(?i)\b(src|href)\s*=\s*(['"]?)cid:([^'"\s>]+)(['"]?)`)

func attachmentByIndex(msg mailbox.Message, rawIndex string) (mailbox.Attachment, int, bool) {
	index, err := strconv.Atoi(rawIndex)
	if err != nil || index < 0 || index >= len(msg.Attachments) {
		return mailbox.Attachment{}, 0, false
	}
	attachment := msg.Attachments[index]
	if len(attachment.Data) == 0 {
		return mailbox.Attachment{}, 0, false
	}
	return attachment, index, true
}

func writeAttachment(w http.ResponseWriter, attachment mailbox.Attachment) {
	w.Header().Set("Content-Type", mailmime.ContentTypeOrDefault(attachment.ContentType))
	w.Header().Set("Content-Length", strconv.Itoa(len(attachment.Data)))
	w.Header().Set("Content-Security-Policy", "sandbox")
	if attachment.Name != "" {
		disposition := "attachment"
		if attachment.Inline {
			disposition = "inline"
		}
		if value := stdmime.FormatMediaType(disposition, map[string]string{"filename": attachment.Name}); value != "" {
			w.Header().Set("Content-Disposition", value)
		}
	}
	_, _ = w.Write(attachment.Data)
}

func attachmentAPIURL(messageID string, index int) string {
	return "/api/v1/message/" + url.PathEscape(messageID) + "/attachment/" + strconv.Itoa(index)
}

func resolveCIDURLs(msg mailbox.Message) string {
	if strings.TrimSpace(msg.HTML) == "" {
		return msg.HTML
	}

	byContentID := map[string]string{}
	for i, attachment := range msg.Attachments {
		if attachment.ContentID == "" || len(attachment.Data) == 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(attachment.ContentID))
		byContentID[key] = "/messages/" + url.PathEscape(msg.ID) + "/attachments/" + strconv.Itoa(i)
	}
	if len(byContentID) == 0 {
		return msg.HTML
	}

	return cidURLPattern.ReplaceAllStringFunc(msg.HTML, func(match string) string {
		parts := cidURLPattern.FindStringSubmatch(match)
		if len(parts) != 5 {
			return match
		}
		cid, err := url.PathUnescape(parts[3])
		if err != nil {
			cid = parts[3]
		}
		replacement, ok := byContentID[strings.ToLower(strings.TrimSpace(cid))]
		if !ok {
			return match
		}
		quote := parts[2]
		if quote == "" {
			quote = parts[4]
		}
		if quote == "" {
			quote = `"`
		}
		return parts[1] + "=" + quote + replacement + quote
	})
}
