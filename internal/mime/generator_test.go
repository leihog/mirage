package mime

import (
	stdmime "mime"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestGenerateSanitizesHeadersAndEncodesTextBody(t *testing.T) {
	raw := Generate(Message{
		From:    "Sender <sender@example.com>",
		To:      []string{"User <user@example.com>"},
		Subject: "Hello\r\nX-Injected: yes",
		Text:    "Héllo\n" + strings.Repeat("a", 1200),
		Headers: map[string]string{
			"X-Test":             "kept\r\nX-Evil: yes",
			"Bad\r\nHeader-Name": "dropped",
		},
	}, GenerateOptions{
		ID:        "20260609T120000-1",
		CreatedAt: time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	})

	parsed, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Get("X-Injected") != "" || parsed.Header.Get("X-Evil") != "" || parsed.Header.Get("Bad") != "" {
		t.Fatalf("generated raw message allowed header injection:\n%s", raw)
	}
	if parsed.Header.Get("MIME-Version") != "1.0" {
		t.Fatalf("expected MIME-Version header, got %q", parsed.Header.Get("MIME-Version"))
	}
	if parsed.Header.Get("Message-ID") == "" {
		t.Fatal("expected generated Message-ID header")
	}
	if parsed.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
		t.Fatalf("expected quoted-printable transfer encoding, got %q", parsed.Header.Get("Content-Transfer-Encoding"))
	}
	if hasBareLF(raw) {
		t.Fatalf("generated raw message contains bare LF:\n%q", raw)
	}
	if longLine := firstLineLongerThan(raw, 998); longLine != "" {
		t.Fatalf("generated raw message contains an overlong line of %d bytes", len(longLine))
	}

	decoded, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(decoded.Text, "Héllo") {
		t.Fatalf("expected decoded text body, got %q", decoded.Text)
	}
}

func TestGenerateSerializesAttachmentsAsMultipartMixed(t *testing.T) {
	raw := Generate(Message{
		From:    "Sender <sender@example.com>",
		To:      []string{"User <user@example.com>"},
		Subject: "with attachment",
		Text:    "Plain body",
		HTML:    "<p>HTML body</p>",
		Attachments: []Attachment{{
			Name:        "notes.txt",
			ContentType: "text/plain; charset=utf-8",
			ContentID:   "notes",
			Data:        []byte("attachment body"),
			Size:        int64(len("attachment body")),
		}},
	}, GenerateOptions{
		ID:        "20260609T120000-1",
		CreatedAt: time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	})

	parsed, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, _, err := stdmime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "multipart/mixed" {
		t.Fatalf("expected multipart/mixed top-level content type, got %q", parsed.Header.Get("Content-Type"))
	}
	if parsed.Header.Get("MIME-Version") != "1.0" {
		t.Fatalf("expected MIME-Version header, got %q", parsed.Header.Get("MIME-Version"))
	}
	if !strings.Contains(raw, "Content-Disposition: attachment; filename=notes.txt") {
		t.Fatalf("expected attachment disposition in raw message:\n%s", raw)
	}
	if !strings.Contains(raw, "Content-Transfer-Encoding: base64") {
		t.Fatalf("expected base64 attachment transfer encoding:\n%s", raw)
	}

	decoded, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Text != "Plain body" || decoded.HTML != "<p>HTML body</p>" {
		t.Fatalf("unexpected decoded alternatives: text=%q html=%q", decoded.Text, decoded.HTML)
	}
	if len(decoded.Attachments) != 1 || decoded.Attachments[0].Name != "notes.txt" || string(decoded.Attachments[0].Data) != "attachment body" {
		t.Fatalf("unexpected decoded attachments: %#v", decoded.Attachments)
	}
}

func TestGenerateAvoidsBoundaryCollisions(t *testing.T) {
	id := "20260609T120000-1"
	collidingBoundary := "mirage-alt-" + SafeFilenamePart(id)
	raw := Generate(Message{
		From: "Sender <sender@example.com>",
		To:   []string{"User <user@example.com>"},
		Text: "This body contains a would-be delimiter.\r\n--" + collidingBoundary + "\r\n",
		HTML: "<p>HTML</p>",
	}, GenerateOptions{
		ID:        id,
		CreatedAt: time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	})

	parsed, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := stdmime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if params["boundary"] == collidingBoundary {
		t.Fatalf("expected collision-safe boundary, got %q", params["boundary"])
	}
	if _, err := Parse([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func hasBareLF(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] == '\n' && (i == 0 || value[i-1] != '\r') {
			return true
		}
	}
	return false
}

func firstLineLongerThan(value string, limit int) string {
	for _, line := range strings.Split(value, "\r\n") {
		if len(line) > limit {
			return line
		}
	}
	return ""
}
