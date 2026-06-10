package web

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/leihog/mirage/internal/mailbox"
)

func apiNotFoundHandler(w http.ResponseWriter, r *http.Request) {
	apiError(w, http.StatusNotFound, "api endpoint not found")
}

func (a *app) apiV1InboxHandler(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pagination(r)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}

	filterUnread, filterByUnread, err := optionalBool(r.URL.Query().Get("unread"))
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid unread filter")
		return
	}
	includeHeaders, err := boolParam(r, "includeHeaders")
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid includeHeaders value")
		return
	}

	allMessages := a.store.List()
	totalUnread := unreadCount(allMessages)
	filtered := make([]mailbox.Message, 0, len(allMessages))
	for _, msg := range allMessages {
		if filterByUnread && msg.Viewed == filterUnread {
			continue
		}
		filtered = append(filtered, msg)
	}

	page := paginate(filtered, limit, offset)
	response := inboxResponse{
		Messages:      make([]messageSummary, 0, len(page)),
		Total:         len(allMessages),
		FilteredTotal: len(filtered),
		UnreadTotal:   totalUnread,
		Limit:         limit,
		Offset:        offset,
		HasMore:       offset+len(page) < len(filtered),
	}
	for _, msg := range page {
		response.Messages = append(response.Messages, summarizeMessage(msg, includeHeaders))
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) apiV1InboxClearHandler(w http.ResponseWriter, r *http.Request) {
	a.store.Clear()
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) apiV1MessageHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok, err := a.apiMessage(r)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		apiError(w, http.StatusNotFound, "message not found")
		return
	}

	writeJSON(w, http.StatusOK, messageResponse{Message: detailMessage(msg)})
}

func (a *app) apiV1MessageUpdateHandler(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Viewed *bool `json:"viewed"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		apiError(w, http.StatusBadRequest, "invalid message update")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		apiError(w, http.StatusBadRequest, "invalid message update")
		return
	}
	if request.Viewed == nil {
		apiError(w, http.StatusBadRequest, "viewed is required")
		return
	}

	msg, ok := a.store.SetViewed(r.PathValue("id"), *request.Viewed)
	if !ok {
		apiError(w, http.StatusNotFound, "message not found")
		return
	}

	writeJSON(w, http.StatusOK, messageResponse{Message: detailMessage(msg)})
}

func (a *app) apiV1MessageDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if !a.store.Delete(r.PathValue("id")) {
		apiError(w, http.StatusNotFound, "message not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) apiV1MessageBodyHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok, err := a.apiMessage(r)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		apiError(w, http.StatusNotFound, "message not found")
		return
	}

	part := r.PathValue("part")
	body, contentType, ok := messageBody(msg, part)
	if !ok {
		apiError(w, http.StatusNotFound, "message body not found")
		return
	}

	if r.URL.Query().Get("format") == "json" {
		writeJSON(w, http.StatusOK, bodyResponse{
			ID:          msg.ID,
			Part:        part,
			ContentType: contentType,
			Size:        len(body),
			Body:        body,
		})
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", "sandbox")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+messageBodyFilename(msg, part)+`"`)
	}
	_, _ = w.Write([]byte(body))
}

func (a *app) apiV1MessageAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok, err := a.apiMessage(r)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		apiError(w, http.StatusNotFound, "message not found")
		return
	}

	attachment, index, ok := attachmentByIndex(msg, r.PathValue("index"))
	if !ok {
		apiError(w, http.StatusNotFound, "attachment not found")
		return
	}

	if r.URL.Query().Get("format") == "json" {
		writeJSON(w, http.StatusOK, attachmentBodyResponse{
			ID:          msg.ID,
			Index:       index,
			Name:        attachment.Name,
			ContentType: attachmentContentType(attachment),
			Size:        len(attachment.Data),
			ContentID:   attachment.ContentID,
			Inline:      attachment.Inline,
			BodyBase64:  base64.StdEncoding.EncodeToString(attachment.Data),
		})
		return
	}

	writeAttachment(w, attachment)
}

func (a *app) apiMessage(r *http.Request) (mailbox.Message, bool, error) {
	markViewed, err := boolParam(r, "markViewed")
	if err != nil {
		return mailbox.Message{}, false, fmt.Errorf("invalid markViewed value")
	}
	if markViewed {
		msg, ok := a.store.MarkViewed(r.PathValue("id"))
		return msg, ok, nil
	}
	msg, ok := a.store.Get(r.PathValue("id"))
	return msg, ok, nil
}
