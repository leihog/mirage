package web

import (
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
