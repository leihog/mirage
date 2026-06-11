package mailgun

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/leihog/mirage/internal/mailbox"
)

func TestMessagesEndpointCapturesFormMessage(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store, 0)

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
	if !slices.Equal(msg.Headers["X-Test"], []string{"yes"}) || !slices.Equal(msg.Options["tag"], []string{"welcome"}) {
		t.Fatalf("prefixed fields were not captured: %#v %#v", msg.Headers, msg.Options)
	}
}

func TestMessagesEndpointKeepsRepeatedHeadersAndOptions(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store, 0)

	form := url.Values{}
	form.Set("from", "Sender <sender@example.com>")
	form.Add("to", "User <user@example.com>")
	form.Set("subject", "Hello")
	form.Add("h:X-Tag", "first")
	form.Add("h:X-Tag", "second")
	form.Add("o:tag", "welcome")
	form.Add("o:tag", "onboarding")

	req := httptest.NewRequest(http.MethodPost, "/v3/example.com/messages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	msg := store.List()[0]
	if !slices.Equal(msg.Headers["X-Tag"], []string{"first", "second"}) {
		t.Fatalf("expected repeated header values to be kept: %#v", msg.Headers)
	}
	if !slices.Equal(msg.Options["tag"], []string{"welcome", "onboarding"}) {
		t.Fatalf("expected repeated option values to be kept: %#v", msg.Options)
	}
}

func TestMessagesEndpointRejectsOversizeRequest(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store, 64)

	form := url.Values{}
	form.Set("from", "sender@example.com")
	form.Set("to", "user@example.com")
	form.Set("subject", "Hello")
	form.Set("text", strings.Repeat("a", 200))

	req := httptest.NewRequest(http.MethodPost, "/v3/example.com/messages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversize request, got %d: %s", res.Code, res.Body.String())
	}
	if got := len(store.List()); got != 0 {
		t.Fatalf("expected oversize request to be rejected, got %d stored", got)
	}
}

func TestMessagesEndpointStripsGeneratedHeaders(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store, 0)

	form := url.Values{}
	form.Set("from", "Sender <sender@example.com>")
	form.Add("to", "User <user@example.com>")
	form.Set("subject", "Hello")
	form.Set("h:Date", "Tue, 09 Jun 2026 08:15:00 +0000")
	form.Set("h:Message-ID", "<client-supplied@example.com>")
	form.Set("h:X-Test", "kept")

	req := httptest.NewRequest(http.MethodPost, "/v3/example.com/messages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	msg := store.List()[0]
	if _, ok := msg.Headers["Date"]; ok {
		t.Fatalf("expected Date header to be stripped: %#v", msg.Headers)
	}
	if _, ok := msg.Headers["Message-ID"]; ok {
		t.Fatalf("expected Message-ID header to be stripped: %#v", msg.Headers)
	}
	if !slices.Equal(msg.Headers["X-Test"], []string{"kept"}) {
		t.Fatalf("expected custom header to remain: %#v", msg.Headers)
	}
}

func TestMIMEEndpointCapturesUploadedMessage(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store, 0)

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

func TestMIMEEndpointStripsReceivedOnlyHeaders(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store, 0)

	raw := strings.Join([]string{
		"Return-Path: <bounce@example.com>",
		"Received: by mx.example.test; Tue, 09 Jun 2026 08:15:00 +0000",
		"Delivered-To: inbox@example.com",
		"Date: Tue, 09 Jun 2026 08:15:00 +0000",
		"Message-ID: <client-supplied@example.com>",
		"From: Sender <sender@example.com>",
		"To: User <user@example.com>",
		"Subject: MIME hello",
		"X-Test: kept",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Hello",
	}, "\r\n")

	req := httptest.NewRequest(http.MethodPost, "/v3/example.com/messages.mime", strings.NewReader(raw))
	req.Header.Set("Content-Type", "message/rfc822")
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	msg := store.List()[0]
	for _, key := range []string{"Return-Path", "Received", "Delivered-To"} {
		if _, ok := msg.Headers[key]; ok {
			t.Fatalf("expected %s to be stripped from headers: %#v", key, msg.Headers)
		}
		if strings.Contains(string(msg.Raw), key+":") {
			t.Fatalf("expected %s to be stripped from raw MIME: %s", key, string(msg.Raw))
		}
	}
	if !slices.Equal(msg.Headers["Date"], []string{"Tue, 09 Jun 2026 08:15:00 +0000"}) || !slices.Equal(msg.Headers["Message-Id"], []string{"<client-supplied@example.com>"}) {
		t.Fatalf("expected Date and Message-ID to be preserved: %#v", msg.Headers)
	}
	if !strings.Contains(string(msg.Raw), "Date: Tue, 09 Jun 2026 08:15:00 +0000") || !strings.Contains(string(msg.Raw), "Message-Id: <client-supplied@example.com>") {
		t.Fatalf("expected Date and Message-ID to be preserved in raw MIME: %s", string(msg.Raw))
	}
	if !slices.Equal(msg.Headers["X-Test"], []string{"kept"}) || !strings.Contains(string(msg.Raw), "X-Test: kept") {
		t.Fatalf("expected custom sendable header to remain: %#v raw=%s", msg.Headers, string(msg.Raw))
	}
}

func TestMIMEEndpointCapturesNestedMultipartAlternative(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store, 0)

	var body strings.Builder
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("message", "nested.eml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(strings.Join([]string{
		"From: Sender <sender@example.com>",
		"To: User <user@example.com>",
		"Subject: Nested MIME hello",
		"Content-Type: multipart/mixed; boundary=mixed",
		"",
		"--mixed",
		"Content-Type: multipart/alternative; boundary=alt",
		"",
		"--alt",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Plain nested",
		"--alt",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<strong>Nested</strong>",
		"--alt--",
		"--mixed",
		"Content-Type: text/plain; name=notes.txt",
		"Content-Disposition: attachment; filename=notes.txt",
		"",
		"attachment body",
		"--mixed--",
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
	if msg.Subject != "Nested MIME hello" || msg.Text != "Plain nested" || msg.HTML != "<strong>Nested</strong>" {
		t.Fatalf("unexpected MIME message: %#v", msg)
	}
	if len(msg.Attachments) != 2 {
		t.Fatalf("expected uploaded .eml plus nested attachment metadata, got %#v", msg.Attachments)
	}
	if msg.Attachments[0].Name != "notes.txt" || msg.Attachments[1].Name != "nested.eml" {
		t.Fatalf("unexpected attachments: %#v", msg.Attachments)
	}
	if len(msg.Attachments[0].Data) == 0 || len(msg.Attachments[1].Data) == 0 {
		t.Fatalf("expected nested and uploaded attachments to be inspectable: %#v", msg.Attachments)
	}
}
