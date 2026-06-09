package web

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirage/internal/mailbox"
)

func TestHTMLHandlerResolvesCIDImageReferences(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{
		HTML: `<p>Logo</p><img src="cid:mirage-logo">`,
		Attachments: []mailbox.Attachment{{
			Name:        "logo.gif",
			ContentType: "image/gif",
			ContentID:   "mirage-logo",
			Inline:      true,
			Data:        []byte("gif-bytes"),
			Size:        int64(len("gif-bytes")),
		}},
	})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/messages/"+msg.ID+"/html", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	wantURL := "/messages/" + msg.ID + "/attachments/0"
	if !strings.Contains(res.Body.String(), `src="`+wantURL+`"`) {
		t.Fatalf("expected CID URL to be rewritten to %s, got %s", wantURL, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, wantURL, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("unexpected attachment content type: %s", res.Header().Get("Content-Type"))
	}
	if res.Body.String() != "gif-bytes" {
		t.Fatalf("unexpected attachment body: %q", res.Body.String())
	}
}

func TestUnsubscribeActionRequiresOneClickPostHeader(t *testing.T) {
	msg := mailbox.Message{
		Headers: map[string]string{
			"List-Unsubscribe":      "<mailto:leave@example.com>, <https://example.com/unsubscribe/abc>",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
	}
	action := unsubscribeAction(msg)
	if action == nil {
		t.Fatal("expected unsubscribe action")
	}
	if action.URL != "https://example.com/unsubscribe/abc" {
		t.Fatalf("unexpected unsubscribe URL: %s", action.URL)
	}

	msg.Headers["List-Unsubscribe-Post"] = "List-Unsubscribe=Manual"
	if action := unsubscribeAction(msg); action != nil {
		t.Fatalf("unexpected unsubscribe action without one-click header: %#v", action)
	}
}

func TestSendOneClickUnsubscribePostsExpectedBodyAndParsesJSON(t *testing.T) {
	var method, contentType, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		contentType = r.Header.Get("Content-Type")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true,"id":"abc"}`))
	}))
	defer server.Close()

	result := sendOneClickUnsubscribe(server.URL)
	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if result.URL != server.URL {
		t.Fatalf("unexpected URL: %s", result.URL)
	}
	if result.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected status code: %d", result.StatusCode)
	}
	if result.JSON == nil {
		t.Fatalf("expected parsed JSON response: %#v", result)
	}
	if method != http.MethodPost {
		t.Fatalf("expected POST, got %s", method)
	}
	if contentType != "application/x-www-form-urlencoded" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	if body != "List-Unsubscribe=One-Click" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestSendOneClickUnsubscribeReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusGone)
	}))
	defer server.Close()

	result := sendOneClickUnsubscribe(server.URL)
	if result.Success {
		t.Fatalf("expected failure, got %#v", result)
	}
	if result.StatusCode != http.StatusGone {
		t.Fatalf("unexpected status code: %d", result.StatusCode)
	}
}

func TestMessageListRendersUTCTimestampForClientConversion(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{Subject: "timestamp"})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	timestamp := msg.CreatedAt.UTC().Format(time.RFC3339Nano)
	if !strings.Contains(res.Body.String(), `datetime="`+timestamp+`"`) {
		t.Fatalf("expected UTC datetime %q in response: %s", timestamp, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `data-timestamp="`+timestamp+`"`) {
		t.Fatalf("expected UTC data timestamp %q in response: %s", timestamp, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `data-timestamp-format="inbox"`) {
		t.Fatalf("expected inbox timestamp format marker in response: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `data-time-24-toggle`) || !strings.Contains(res.Body.String(), `data-date-format-select`) {
		t.Fatalf("expected time display settings in response: %s", res.Body.String())
	}
}

func TestMessageDetailDateUsesDisplayTimestampAndHeadersUseRawDate(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{
		Subject: "timestamp",
		Headers: map[string]string{
			"Date": "Tue, 09 Jun 2026 08:15:00 +0000",
		},
	})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/messages/"+msg.ID, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	timestamp := msg.CreatedAt.UTC().Format(time.RFC3339Nano)
	if !strings.Contains(res.Body.String(), `data-timestamp="`+timestamp+`" data-timestamp-format="datetime"`) {
		t.Fatalf("expected detail date to use localizable timestamp %q: %s", timestamp, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `<strong>Date</strong><span>Tue, 09 Jun 2026 08:15:00 &#43;0000</span>`) {
		t.Fatalf("expected headers tab to keep raw Date header: %s", res.Body.String())
	}
}

func TestAPIV1InboxPaginatesAndOmitsBodies(t *testing.T) {
	store := mailbox.NewStore()
	first := store.Add(mailbox.Message{Subject: "first", Text: "first body"})
	second := store.Add(mailbox.Message{Subject: "second", HTML: "<p>second</p>"})
	_, _ = first, second

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox?limit=1&offset=0", nil)
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Messages      []map[string]any `json:"messages"`
		Total         int              `json:"total"`
		FilteredTotal int              `json:"filteredTotal"`
		UnreadTotal   int              `json:"unreadTotal"`
		Limit         int              `json:"limit"`
		Offset        int              `json:"offset"`
		HasMore       bool             `json:"hasMore"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 2 || body.FilteredTotal != 2 || body.UnreadTotal != 2 || body.Limit != 1 || body.Offset != 0 || !body.HasMore {
		t.Fatalf("unexpected pagination metadata: %#v", body)
	}
	if len(body.Messages) != 1 {
		t.Fatalf("expected one message, got %d", len(body.Messages))
	}
	createdAt, ok := body.Messages[0]["createdAt"].(string)
	if !ok {
		t.Fatalf("expected createdAt string: %#v", body.Messages[0])
	}
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		t.Fatalf("expected RFC3339 timestamp, got %q: %v", createdAt, err)
	}
	if parsed.Location() != time.UTC {
		t.Fatalf("expected UTC timestamp, got %s", parsed.Location())
	}
	if _, ok := body.Messages[0]["text"]; ok {
		t.Fatalf("inbox summary should not include text body: %#v", body.Messages[0])
	}
	if _, ok := body.Messages[0]["html"]; ok {
		t.Fatalf("inbox summary should not include html body: %#v", body.Messages[0])
	}
	if _, ok := body.Messages[0]["hasRaw"]; ok {
		t.Fatalf("inbox summary should not expose hasRaw because raw source is always available: %#v", body.Messages[0])
	}
}

func TestAPIV1InboxReportsGlobalAndFilteredTotals(t *testing.T) {
	store := mailbox.NewStore()
	viewed := store.Add(mailbox.Message{Subject: "viewed"})
	store.Add(mailbox.Message{Subject: "unread"})
	if _, ok := store.MarkViewed(viewed.ID); !ok {
		t.Fatal("failed to mark message viewed")
	}

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inbox?unread=true", nil)
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Messages      []map[string]any `json:"messages"`
		Total         int              `json:"total"`
		FilteredTotal int              `json:"filteredTotal"`
		UnreadTotal   int              `json:"unreadTotal"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 2 || body.FilteredTotal != 1 || body.UnreadTotal != 1 {
		t.Fatalf("unexpected count metadata: %#v", body)
	}
	if len(body.Messages) != 1 || body.Messages[0]["subject"] != "unread" {
		t.Fatalf("unexpected filtered messages: %#v", body.Messages)
	}
}

func TestLegacyAPIMessagesRouteIsNotRegistered(t *testing.T) {
	store := mailbox.NewStore()

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected JSON response, got %q", res.Header().Get("Content-Type"))
	}
}

func TestAPIV1InboxClearDeletesAllMessages(t *testing.T) {
	store := mailbox.NewStore()
	store.Add(mailbox.Message{Subject: "first"})
	store.Add(mailbox.Message{Subject: "second"})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/inbox", nil)
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", res.Code, res.Body.String())
	}
	if got := len(store.List()); got != 0 {
		t.Fatalf("expected inbox to be empty, got %d messages", got)
	}
}

func TestAPIV1MessageDeleteRemovesMessage(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{Subject: "delete"})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/message/"+msg.ID, nil)
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", res.Code, res.Body.String())
	}
	if got := len(store.List()); got != 0 {
		t.Fatalf("expected message to be deleted, got %d messages", got)
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/message/"+msg.ID, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing message, got %d: %s", res.Code, res.Body.String())
	}
}

func TestAPIV1MessagePatchSetsReadState(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{Subject: "read state"})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/message/"+msg.ID, strings.NewReader(`{"viewed":true}`))
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if !store.List()[0].Viewed {
		t.Fatal("expected message to be marked viewed")
	}
	var body struct {
		Message struct {
			Viewed bool `json:"viewed"`
		} `json:"message"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Message.Viewed {
		t.Fatalf("expected viewed response, got %#v", body.Message)
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/message/"+msg.ID, strings.NewReader(`{"viewed":false}`))
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if store.List()[0].Viewed {
		t.Fatal("expected message to be marked unread")
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/message/missing", strings.NewReader(`{"viewed":true}`))
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing message, got %d: %s", res.Code, res.Body.String())
	}
}

func TestAPIV1MessagePatchRejectsInvalidReadState(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{Subject: "read state"})

	mux := http.NewServeMux()
	Register(mux, store)

	for _, body := range []string{`{}`, `{"viewed":"true"}`, `{"read":true}`, `{"viewed":true} {"viewed":false}`} {
		res := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/message/"+msg.ID, strings.NewReader(body))
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d: %s", body, res.Code, res.Body.String())
		}
	}
}

func TestAPIV1MessageDoesNotMarkViewedUnlessRequested(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{
		Subject: "metadata",
		Text:    "plain",
		HTML:    "<p>html</p>",
		Headers: map[string]string{
			"X-Test": "yes",
		},
	})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/message/"+msg.ID, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if store.List()[0].Viewed {
		t.Fatal("API metadata fetch should not mark viewed by default")
	}
	var body struct {
		Message struct {
			ID      string `json:"id"`
			Viewed  bool   `json:"viewed"`
			Headers map[string]string
			Bodies  map[string]bodySummary `json:"bodies"`
		} `json:"message"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Message.ID != msg.ID || body.Message.Viewed {
		t.Fatalf("unexpected message response: %#v", body.Message)
	}
	if body.Message.Headers["X-Test"] != "yes" {
		t.Fatalf("expected headers in detail response: %#v", body.Message.Headers)
	}
	if body.Message.Bodies["text"].URL == "" || body.Message.Bodies["html"].URL == "" || body.Message.Bodies["raw"].URL == "" {
		t.Fatalf("expected body URLs: %#v", body.Message.Bodies)
	}
	if !body.Message.Bodies["raw"].Available {
		t.Fatalf("expected generated raw body to be available: %#v", body.Message.Bodies["raw"])
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/message/"+msg.ID+"?markViewed=true", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if !store.List()[0].Viewed {
		t.Fatal("markViewed=true should mark message as viewed")
	}
}

func TestAPIV1MessageExposesInspectableAttachments(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{
		Subject: "attachment",
		Attachments: []mailbox.Attachment{{
			Name:        "logo.gif",
			ContentType: "image/gif",
			ContentID:   "mirage-logo",
			Inline:      true,
			Data:        []byte("gif-bytes"),
			Size:        int64(len("gif-bytes")),
		}},
	})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/message/"+msg.ID, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var detail struct {
		Message struct {
			Attachments []attachmentResponse `json:"attachments"`
		} `json:"message"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Message.Attachments) != 1 {
		t.Fatalf("expected one attachment, got %#v", detail.Message.Attachments)
	}
	attachment := detail.Message.Attachments[0]
	wantURL := "/api/v1/message/" + msg.ID + "/attachment/0"
	if attachment.URL != wantURL || attachment.ContentID != "mirage-logo" || !attachment.Inline {
		t.Fatalf("unexpected attachment metadata: %#v", attachment)
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, wantURL, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("unexpected attachment content type: %s", res.Header().Get("Content-Type"))
	}
	if res.Body.String() != "gif-bytes" {
		t.Fatalf("unexpected attachment body: %q", res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, wantURL+"?format=json", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var body attachmentBodyResponse
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.BodyBase64 != base64.StdEncoding.EncodeToString([]byte("gif-bytes")) || body.Size != len("gif-bytes") {
		t.Fatalf("unexpected attachment json body: %#v", body)
	}
	if store.List()[0].Viewed {
		t.Fatal("attachment API should not mark viewed by default")
	}
}

func TestAPIV1MessageBodySupportsNativeAndJSONFormats(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{Subject: "body", HTML: "<h1>Hello</h1>"})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/message/"+msg.ID+"/body/html", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if contentType := res.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	if res.Body.String() != "<h1>Hello</h1>" {
		t.Fatalf("unexpected html body: %q", res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/message/"+msg.ID+"/body/html?format=json", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var body bodyResponse
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ContentType != "text/html; charset=utf-8" || body.Body != "<h1>Hello</h1>" {
		t.Fatalf("unexpected json body response: %#v", body)
	}
	if store.List()[0].Viewed {
		t.Fatal("body API should not mark viewed by default")
	}
}

func TestAPIV1MessageBodyDownloadUsesAttachmentFilenames(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{
		Subject: "body",
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Text:    "Plain body",
	})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/message/"+msg.ID+"/body/raw?download=1", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if contentType := res.Header().Get("Content-Type"); contentType != "message/rfc822" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	wantDisposition := `attachment; filename="message-` + msg.ID + `.eml"`
	if disposition := res.Header().Get("Content-Disposition"); disposition != wantDisposition {
		t.Fatalf("unexpected content disposition: %s", disposition)
	}
	if !strings.Contains(res.Body.String(), "Content-Type: text/plain; charset=utf-8") {
		t.Fatalf("expected generated raw message, got %q", res.Body.String())
	}
}
