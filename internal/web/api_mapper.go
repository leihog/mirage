package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/leihog/mirage/internal/mailbox"
)

func summarizeMessage(msg mailbox.Message, includeHeaders bool) messageSummary {
	summary := messageSummary{
		ID:              msg.ID,
		Subject:         msg.Subject,
		From:            msg.From,
		To:              stringSlice(msg.To),
		Cc:              stringSlice(msg.Cc),
		Bcc:             stringSlice(msg.Bcc),
		Provider:        msg.Provider,
		Domain:          msg.Domain,
		CreatedAt:       msg.CreatedAt.UTC(),
		Viewed:          msg.Viewed,
		HasText:         strings.TrimSpace(msg.Text) != "",
		HasHTML:         strings.TrimSpace(msg.HTML) != "",
		AttachmentCount: len(msg.Attachments),
	}
	if includeHeaders {
		summary.Headers = msg.Headers
	}
	return summary
}

func detailMessage(msg mailbox.Message) messageDetail {
	detail := messageDetail{
		ID:          msg.ID,
		Subject:     msg.Subject,
		From:        msg.From,
		To:          stringSlice(msg.To),
		Cc:          stringSlice(msg.Cc),
		Bcc:         stringSlice(msg.Bcc),
		Provider:    msg.Provider,
		Domain:      msg.Domain,
		CreatedAt:   msg.CreatedAt.UTC(),
		Viewed:      msg.Viewed,
		Headers:     msg.Headers,
		Variables:   msg.Variables,
		Options:     msg.Options,
		Attachments: make([]attachmentResponse, 0, len(msg.Attachments)),
		Bodies:      bodySummaries(msg),
	}
	for i, attachment := range msg.Attachments {
		attachmentURL := ""
		if len(attachment.Data) > 0 {
			attachmentURL = attachmentAPIURL(msg.ID, i)
		}
		detail.Attachments = append(detail.Attachments, attachmentResponse{
			Name:        attachment.Name,
			ContentType: attachment.ContentType,
			Size:        attachment.Size,
			ContentID:   attachment.ContentID,
			Inline:      attachment.Inline,
			URL:         attachmentURL,
		})
	}
	if action := unsubscribeAction(msg); action != nil {
		detail.Unsubscribe = &unsubscribeSummary{OneClick: true, URL: action.URL}
	}
	return detail
}

func bodySummaries(msg mailbox.Message) map[string]bodySummary {
	base := "/api/v1/message/" + url.PathEscape(msg.ID) + "/body/"
	out := map[string]bodySummary{}
	for _, part := range []string{"text", "html", "raw"} {
		body, contentType, ok := messageBody(msg, part)
		summary := bodySummary{Available: ok}
		if ok {
			summary.Size = len(body)
			summary.ContentType = contentType
			summary.URL = base + part
		}
		out[part] = summary
	}
	return out
}

func pagination(r *http.Request) (int, int, error) {
	limit := 50
	offset := 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return 0, 0, fmt.Errorf("invalid limit")
		}
	}
	if limit > 500 {
		limit = 500
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return 0, 0, fmt.Errorf("invalid offset")
		}
	}
	return limit, offset, nil
}

func optionalBool(value string) (bool, bool, error) {
	if value == "" {
		return false, false, nil
	}
	parsed, err := strconv.ParseBool(value)
	return parsed, true, err
}

func boolParam(r *http.Request, key string) (bool, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return false, nil
	}
	return strconv.ParseBool(value)
}

func paginate(messages []mailbox.Message, limit, offset int) []mailbox.Message {
	if offset >= len(messages) {
		return nil
	}
	end := offset + limit
	if end > len(messages) {
		end = len(messages)
	}
	return messages[offset:end]
}

func unreadCount(messages []mailbox.Message) int {
	unread := 0
	for _, msg := range messages {
		if !msg.Viewed {
			unread++
		}
	}
	return unread
}
