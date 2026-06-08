package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mirage/internal/mailbox"
)

func TestPrettyHTMLSourceIndentsNestedMarkup(t *testing.T) {
	got := prettyHTMLSource(`<div><h1 class="title">Hello</h1><p>World</p></div>`)
	want := strings.Join([]string{
		`<div>`,
		`  <h1 class="title">`,
		`    Hello`,
		`  </h1>`,
		`  <p>`,
		`    World`,
		`  </p>`,
		`</div>`,
	}, "\n")
	if got != want {
		t.Fatalf("unexpected pretty HTML:\n%s", got)
	}
}

func TestHTMLSourceEscapesAndHighlightsMarkup(t *testing.T) {
	got := string(htmlSource(mailbox.Message{
		HTML: `<img src=x onerror="alert(1)">`,
	}))
	if !strings.Contains(got, `&lt;`) || !strings.Contains(got, `class="html-tag-name"`) {
		t.Fatalf("expected escaped highlighted HTML, got %s", got)
	}
	if strings.Contains(got, `<img`) || strings.Contains(got, `onerror="alert(1)"`) {
		t.Fatalf("source HTML was not safely escaped: %s", got)
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
		Messages []map[string]any `json:"messages"`
		Total    int              `json:"total"`
		Unread   int              `json:"unread"`
		Limit    int              `json:"limit"`
		Offset   int              `json:"offset"`
		HasMore  bool             `json:"hasMore"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 2 || body.Unread != 2 || body.Limit != 1 || body.Offset != 0 || !body.HasMore {
		t.Fatalf("unexpected pagination metadata: %#v", body)
	}
	if len(body.Messages) != 1 {
		t.Fatalf("expected one message, got %d", len(body.Messages))
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
