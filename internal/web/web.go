package web

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"mirage/internal/html"
	"mirage/internal/mailbox"
)

//go:embed assets/templates/*.html assets/static/*
var assetsFS embed.FS

type Store interface {
	List() []mailbox.Message
	Get(string) (mailbox.Message, bool)
	MarkViewed(string) (mailbox.Message, bool)
	SetViewed(string, bool) (mailbox.Message, bool)
	Delete(string) bool
	Clear()
}

func Register(mux *http.ServeMux, store Store) {
	app := &app{
		store: store,
		index: parseTemplate("index"),
		show:  parseTemplate("show"),
	}

	staticFS, err := fs.Sub(assetsFS, "assets/static")
	if err != nil {
		panic(err)
	}

	mux.HandleFunc("GET /", app.indexHandler)
	mux.HandleFunc("GET /messages/{id}", app.showHandler)
	mux.HandleFunc("GET /messages/{id}/html", app.htmlHandler)
	mux.HandleFunc("GET /messages/{id}/attachments/{index}", app.attachmentHandler)
	mux.HandleFunc("POST /messages/{id}/unsubscribe", app.unsubscribeHandler)
	mux.HandleFunc("POST /messages/{id}/delete", app.deleteHandler)
	mux.HandleFunc("POST /messages/clear", app.clearHandler)
	mux.HandleFunc("GET /api/v1/inbox", app.apiV1InboxHandler)
	mux.HandleFunc("DELETE /api/v1/inbox", app.apiV1InboxClearHandler)
	mux.HandleFunc("GET /api/v1/message/{id}", app.apiV1MessageHandler)
	mux.HandleFunc("PATCH /api/v1/message/{id}", app.apiV1MessageUpdateHandler)
	mux.HandleFunc("DELETE /api/v1/message/{id}", app.apiV1MessageDeleteHandler)
	mux.HandleFunc("GET /api/v1/message/{id}/body/{part}", app.apiV1MessageBodyHandler)
	mux.HandleFunc("GET /api/v1/message/{id}/attachment/{index}", app.apiV1MessageAttachmentHandler)
	mux.HandleFunc("GET /api/", apiNotFoundHandler)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(staticFS))))
}

type app struct {
	store Store
	index *template.Template
	show  *template.Template
}

func (a *app) indexHandler(w http.ResponseWriter, r *http.Request) {
	messages := a.store.List()
	data := struct {
		Messages   []mailbox.Message
		SelectedID string
	}{
		Messages: messages,
	}
	render(w, a.index, data)
}

func (a *app) showHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.store.MarkViewed(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	data := struct {
		Messages   []mailbox.Message
		Selected   mailbox.Message
		SelectedID string
	}{
		Messages:   a.store.List(),
		Selected:   msg,
		SelectedID: msg.ID,
	}
	render(w, a.show, data)
}

func (a *app) htmlHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.store.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if strings.TrimSpace(msg.HTML) != "" {
		_, _ = w.Write([]byte(resolveCIDURLs(msg)))
		return
	}
	escaped := template.HTMLEscapeString(msg.Text)
	_, _ = w.Write([]byte("<!doctype html><meta charset=\"utf-8\"><pre style=\"white-space:pre-wrap;font:14px/1.5 system-ui,sans-serif\">" + escaped + "</pre>"))
}

func (a *app) attachmentHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.store.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	attachment, _, ok := attachmentByIndex(msg, r.PathValue("index"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeAttachment(w, attachment)
}

func (a *app) deleteHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.store.Delete(id)

	redirect := "/"
	for _, msg := range a.store.List() {
		if msg.ID != id {
			redirect = "/messages/" + msg.ID
			break
		}
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *app) unsubscribeHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.store.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}

	action := unsubscribeAction(msg)
	if action == nil {
		http.NotFound(w, r)
		return
	}

	result := sendOneClickUnsubscribe(action.URL)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (a *app) clearHandler(w http.ResponseWriter, r *http.Request) {
	a.store.Clear()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

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

type inboxResponse struct {
	Messages      []messageSummary `json:"messages"`
	Total         int              `json:"total"`
	FilteredTotal int              `json:"filteredTotal"`
	UnreadTotal   int              `json:"unreadTotal"`
	Limit         int              `json:"limit"`
	Offset        int              `json:"offset"`
	HasMore       bool             `json:"hasMore"`
}

type messageResponse struct {
	Message messageDetail `json:"message"`
}

type messageSummary struct {
	ID              string            `json:"id"`
	Subject         string            `json:"subject"`
	From            string            `json:"from"`
	To              []string          `json:"to"`
	Cc              []string          `json:"cc"`
	Bcc             []string          `json:"bcc"`
	Provider        string            `json:"provider"`
	Domain          string            `json:"domain"`
	CreatedAt       time.Time         `json:"createdAt"`
	Viewed          bool              `json:"viewed"`
	HasText         bool              `json:"hasText"`
	HasHTML         bool              `json:"hasHTML"`
	AttachmentCount int               `json:"attachmentCount"`
	Headers         map[string]string `json:"headers,omitempty"`
}

type messageDetail struct {
	ID          string                 `json:"id"`
	Subject     string                 `json:"subject"`
	From        string                 `json:"from"`
	To          []string               `json:"to"`
	Cc          []string               `json:"cc"`
	Bcc         []string               `json:"bcc"`
	Provider    string                 `json:"provider"`
	Domain      string                 `json:"domain"`
	CreatedAt   time.Time              `json:"createdAt"`
	Viewed      bool                   `json:"viewed"`
	Headers     map[string]string      `json:"headers"`
	Variables   map[string]string      `json:"variables"`
	Options     map[string]string      `json:"options"`
	Attachments []attachmentResponse   `json:"attachments"`
	Bodies      map[string]bodySummary `json:"bodies"`
	Unsubscribe *unsubscribeSummary    `json:"unsubscribe,omitempty"`
}

type attachmentResponse struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	ContentID   string `json:"contentId,omitempty"`
	Inline      bool   `json:"inline"`
	URL         string `json:"url,omitempty"`
}

type attachmentBodyResponse struct {
	ID          string `json:"id"`
	Index       int    `json:"index"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	ContentID   string `json:"contentId,omitempty"`
	Inline      bool   `json:"inline"`
	BodyBase64  string `json:"bodyBase64"`
}

type bodySummary struct {
	Available   bool   `json:"available"`
	Size        int    `json:"size"`
	ContentType string `json:"contentType,omitempty"`
	URL         string `json:"url,omitempty"`
}

type bodyResponse struct {
	ID          string `json:"id"`
	Part        string `json:"part"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Body        string `json:"body"`
}

type unsubscribeSummary struct {
	OneClick bool   `json:"oneClick"`
	URL      string `json:"url,omitempty"`
}

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
	w.Header().Set("Content-Type", attachmentContentType(attachment))
	w.Header().Set("Content-Length", strconv.Itoa(len(attachment.Data)))
	if attachment.Name != "" {
		disposition := "attachment"
		if attachment.Inline {
			disposition = "inline"
		}
		w.Header().Set("Content-Disposition", disposition+`; filename="`+strings.ReplaceAll(attachment.Name, `"`, `\"`)+`"`)
	}
	_, _ = w.Write(attachment.Data)
}

func attachmentContentType(attachment mailbox.Attachment) string {
	if attachment.ContentType != "" {
		return attachment.ContentType
	}
	return "application/octet-stream"
}

func attachmentAPIURL(messageID string, index int) string {
	return "/api/v1/message/" + url.PathEscape(messageID) + "/attachment/" + strconv.Itoa(index)
}

var cidURLPattern = regexp.MustCompile(`(?i)\b(src|href)\s*=\s*(['"]?)cid:([^'"\s>]+)(['"]?)`)

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

func stringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
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
	return "message-" + safeFilenamePart(msg.ID) + "." + extension
}

func safeFilenamePart(value string) string {
	var out strings.Builder
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			out.WriteRune(ch)
		}
	}
	if out.Len() == 0 {
		return "email"
	}
	return out.String()
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func render(w http.ResponseWriter, tmpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseTemplate(name string) *template.Template {
	return template.Must(template.New(name).Funcs(funcs()).ParseFS(
		assetsFS,
		"assets/templates/layout.html",
		"assets/templates/"+name+".html",
	))
}

func funcs() template.FuncMap {
	return template.FuncMap{
		"join": func(values []string) string {
			return strings.Join(values, ", ")
		},
		"has": func(value string) bool {
			return strings.TrimSpace(value) != ""
		},
		"mailboxLine": mailboxLine,
		"senderName":  senderName,
		"messageID":   messageID,
		"dateLine":    dateLine,
		"utcTime": func(value time.Time) string {
			return value.UTC().Format(time.RFC3339Nano)
		},
		"utcClock": func(value time.Time) string {
			return value.UTC().Format("15:04")
		},
		"utcDateTime": func(value time.Time) string {
			return value.UTC().Format("Mon, 2 Jan 2006, 3:04 pm")
		},
		"messageSize": func(msg mailbox.Message) string {
			size := len(msg.Raw) + len(msg.Text) + len(msg.HTML)
			for key, value := range msg.Headers {
				size += len(key) + len(value) + 4
			}
			if size < 1024 {
				return fmt.Sprintf("%d B", size)
			}
			return fmt.Sprintf("%.1f kB", float64(size)/1024)
		},
		"htmlSource":  htmlSource,
		"unsubscribe": unsubscribeAction,
		"rawMessage":  rawMessage,
		"headerRows":  headerRows,
	}
}

type unsubscribeInfo struct {
	URL string
}

type unsubscribeResult struct {
	URL        string `json:"url"`
	Success    bool   `json:"success"`
	StatusCode int    `json:"statusCode"`
	Status     string `json:"status"`
	JSON       any    `json:"json,omitempty"`
	Error      string `json:"error,omitempty"`
}

func unsubscribeAction(msg mailbox.Message) *unsubscribeInfo {
	post := strings.TrimSpace(headerValue(msg.Headers, "List-Unsubscribe-Post"))
	if !strings.EqualFold(post, "List-Unsubscribe=One-Click") {
		return nil
	}

	for _, candidate := range unsubscribeCandidates(headerValue(msg.Headers, "List-Unsubscribe")) {
		parsed, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return &unsubscribeInfo{URL: parsed.String()}
		}
	}
	return nil
}

func sendOneClickUnsubscribe(target string) unsubscribeResult {
	result := unsubscribeResult{URL: target}
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer res.Body.Close()

	result.StatusCode = res.StatusCode
	result.Status = http.StatusText(res.StatusCode)
	result.Success = res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "application/json") && len(strings.TrimSpace(string(body))) > 0 {
		var decoded any
		if err := json.Unmarshal(body, &decoded); err == nil {
			result.JSON = decoded
		}
	}
	return result
}

func headerValue(headers map[string]string, key string) string {
	for headerKey, value := range headers {
		if strings.EqualFold(headerKey, key) {
			return value
		}
	}
	return ""
}

func unsubscribeCandidates(value string) []string {
	var candidates []string
	for i := 0; i < len(value); i++ {
		if value[i] != '<' {
			continue
		}
		end := strings.IndexByte(value[i+1:], '>')
		if end == -1 {
			break
		}
		candidate := strings.TrimSpace(value[i+1 : i+1+end])
		if candidate != "" {
			candidates = append(candidates, candidate)
		}
		i += end + 1
	}
	if len(candidates) > 0 {
		return candidates
	}

	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func mailboxLine(messages []mailbox.Message) string {
	unread := 0
	for _, msg := range messages {
		if !msg.Viewed {
			unread++
		}
	}
	return fmt.Sprintf("%d %s, %d unread", len(messages), plural(len(messages), "mail", "mails"), unread)
}

func senderName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if address, err := mail.ParseAddress(value); err == nil && strings.TrimSpace(address.Name) != "" {
		return strings.TrimSpace(address.Name)
	}
	if i := strings.Index(value, "<"); i > 0 {
		name := strings.Trim(strings.TrimSpace(value[:i]), `"`)
		if name != "" {
			return name
		}
	}
	if i := strings.Index(value, "@"); i > 0 {
		return value[:i]
	}
	return value
}

func messageID(msg mailbox.Message) string {
	if value := strings.TrimSpace(headerValue(msg.Headers, "Message-Id")); value != "" {
		return value
	}
	return "<" + msg.ID + "@mirage.local>"
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func htmlSource(msg mailbox.Message) template.HTML {
	return html.PrettyHTMLSource(msg.HTML)
}

type headerRow struct {
	Key   string
	Value string
}

func dateLine(msg mailbox.Message) string {
	if value := strings.TrimSpace(msg.Headers["Date"]); value != "" {
		return value
	}
	return msg.CreatedAt.UTC().Format(time.RFC1123Z)
}

func headerRows(msg mailbox.Message) []headerRow {
	rows := make([]headerRow, 0, len(msg.Headers)+6)
	seen := map[string]bool{}
	add := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		rows = append(rows, headerRow{Key: key, Value: value})
		seen[strings.ToLower(key)] = true
	}

	add("Content-Type", msg.Headers["Content-Type"])
	add("Date", dateLine(msg))
	add("From", msg.From)
	add("Message-Id", msg.Headers["Message-Id"])
	add("Mime-Version", msg.Headers["Mime-Version"])
	add("Subject", msg.Subject)
	add("To", strings.Join(msg.To, ", "))
	add("Cc", strings.Join(msg.Cc, ", "))
	add("Bcc", strings.Join(msg.Bcc, ", "))

	keys := make([]string, 0, len(msg.Headers))
	for key := range msg.Headers {
		if !seen[strings.ToLower(key)] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		add(key, msg.Headers[key])
	}
	return rows
}

func rawMessage(msg mailbox.Message) string {
	if len(msg.Raw) > 0 {
		return string(msg.Raw)
	}

	var buf bytes.Buffer
	writeHeader := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			fmt.Fprintf(&buf, "%s: %s\r\n", key, value)
		}
	}

	writeHeader("From", msg.From)
	writeHeader("To", strings.Join(msg.To, ", "))
	writeHeader("Cc", strings.Join(msg.Cc, ", "))
	writeHeader("Bcc", strings.Join(msg.Bcc, ", "))
	writeHeader("Subject", msg.Subject)
	writeHeader("Date", msg.CreatedAt.UTC().Format(time.RFC1123Z))

	keys := make([]string, 0, len(msg.Headers))
	for key := range msg.Headers {
		lower := strings.ToLower(key)
		if lower == "from" || lower == "to" || lower == "cc" || lower == "bcc" || lower == "subject" || lower == "date" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeHeader(key, msg.Headers[key])
	}

	if strings.TrimSpace(msg.HTML) != "" && strings.TrimSpace(msg.Text) != "" {
		boundary := "mirage-local-boundary"
		writeHeader("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
		buf.WriteString("\r\n--" + boundary + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
		buf.WriteString(msg.Text)
		buf.WriteString("\r\n--" + boundary + "\r\nContent-Type: text/html; charset=utf-8\r\n\r\n")
		buf.WriteString(msg.HTML)
		buf.WriteString("\r\n--" + boundary + "--\r\n")
		return buf.String()
	}

	if strings.TrimSpace(msg.HTML) != "" {
		writeHeader("Content-Type", "text/html; charset=utf-8")
		buf.WriteString("\r\n")
		buf.WriteString(msg.HTML)
		return buf.String()
	}

	writeHeader("Content-Type", "text/plain; charset=utf-8")
	buf.WriteString("\r\n")
	buf.WriteString(msg.Text)
	return buf.String()
}
