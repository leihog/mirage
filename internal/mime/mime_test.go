package mime

import (
	"strings"
	"testing"
)

func TestParseNestedMultipartAlternativeInsideMixed(t *testing.T) {
	raw := strings.Join([]string{
		"From: Sender <sender@example.com>",
		"To: User <user@example.com>",
		"Subject: Nested hello",
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
	}, "\r\n")

	msg, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Nested hello" {
		t.Fatalf("unexpected subject: %s", msg.Subject)
	}
	if msg.Text != "Plain nested" {
		t.Fatalf("unexpected text body: %q", msg.Text)
	}
	if msg.HTML != "<strong>Nested</strong>" {
		t.Fatalf("unexpected html body: %q", msg.HTML)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected one attachment, got %d", len(msg.Attachments))
	}
	if msg.Attachments[0].Name != "notes.txt" || msg.Attachments[0].Size != int64(len("attachment body")) {
		t.Fatalf("unexpected attachment: %#v", msg.Attachments[0])
	}
}

func TestParseDecodesTransferEncodingForSinglePartMessage(t *testing.T) {
	raw := strings.Join([]string{
		"From: Sender <sender@example.com>",
		"To: User <user@example.com>",
		"Subject: Encoded",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"Hello=2C world",
	}, "\r\n")

	msg, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "Hello, world" {
		t.Fatalf("unexpected decoded text: %q", msg.Text)
	}
}

func TestParseAddressFieldsUsesUnquotedDisplayNames(t *testing.T) {
	got := ParseAddressFields([]string{`"HTML User" <html@example.com>, Plain User <plain@example.com>`})
	want := []string{"HTML User <html@example.com>", "Plain User <plain@example.com>"}
	if len(got) != len(want) {
		t.Fatalf("expected %d addresses, got %#v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("address %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestParseCapturesInlineAttachmentContentIDAndData(t *testing.T) {
	raw := strings.Join([]string{
		"From: Sender <sender@example.com>",
		"To: User <user@example.com>",
		"Subject: Inline image",
		"Content-Type: multipart/related; boundary=related",
		"",
		"--related",
		"Content-Type: text/html; charset=utf-8",
		"",
		`<img src="cid:mirage-logo">`,
		"--related",
		`Content-Type: image/gif; name="logo.gif"`,
		"Content-Disposition: inline; filename=logo.gif",
		"Content-ID: <mirage-logo>",
		"Content-Transfer-Encoding: base64",
		"",
		"R0lGODdhAQABAIABAP8AAP///ywAAAAAAQABAAACAkQBADs=",
		"--related--",
	}, "\r\n")

	msg, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.HTML != `<img src="cid:mirage-logo">` {
		t.Fatalf("unexpected html body: %q", msg.HTML)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected one inline attachment, got %d", len(msg.Attachments))
	}
	attachment := msg.Attachments[0]
	if attachment.Name != "logo.gif" || attachment.ContentID != "mirage-logo" || !attachment.Inline {
		t.Fatalf("unexpected inline attachment metadata: %#v", attachment)
	}
	if len(attachment.Data) == 0 || attachment.Size != int64(len(attachment.Data)) {
		t.Fatalf("expected decoded attachment data: %#v", attachment)
	}
}
