package mailgun

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"mirage/internal/mailbox"
)

func TestMessagesEndpointCapturesFormMessage(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store)

	form := url.Values{}
	form.Set("from", "Sender <sender@example.com>")
	form.Add("to", "User <user@example.com>")
	form.Set("subject", "Hello")
	form.Set("text", "Plain")
	form.Set("html", "<p>HTML</p>")
	form.Set("h:X-Test", "yes")
	form.Set("o:tag", "welcome")

	req := httptest.NewRequest(http.MethodPost, "/v3/example.com/messages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["message"] != "Queued. Thank you." {
		t.Fatalf("unexpected response: %#v", body)
	}

	messages := store.List()
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %d", len(messages))
	}
	msg := messages[0]
	if msg.Domain != "example.com" || msg.Subject != "Hello" || msg.HTML != "<p>HTML</p>" {
		t.Fatalf("message was not captured correctly: %#v", msg)
	}
	if msg.Headers["X-Test"] != "yes" || msg.Options["tag"] != "welcome" {
		t.Fatalf("prefixed fields were not captured: %#v %#v", msg.Headers, msg.Options)
	}
}

func TestMIMEEndpointCapturesUploadedMessage(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store)

	var body strings.Builder
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("message", "email.eml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(strings.Join([]string{
		"From: Sender <sender@example.com>",
		"To: User <user@example.com>",
		"Subject: MIME hello",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<strong>Hello</strong>",
	}, "\r\n")))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v3/example.com/messages.mime", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	msg := store.List()[0]
	if msg.Subject != "MIME hello" || msg.HTML != "<strong>Hello</strong>" {
		t.Fatalf("unexpected MIME message: %#v", msg)
	}
}
