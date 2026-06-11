package smtp

import (
	"context"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/leihog/mirage/internal/mailbox"
)

func TestSMTPServerCapturesMessage(t *testing.T) {
	store := mailbox.NewStore()
	_, addr, stop := startTestServer(t, store)
	defer stop()

	client := dialSMTP(t, addr)
	defer client.Close()

	readResponse(t, client, 220)
	writeLine(t, client, "EHLO local.test")
	readResponse(t, client, 250)
	writeLine(t, client, "MAIL FROM:<sender@example.com> BODY=8BITMIME")
	readResponse(t, client, 250)
	writeLine(t, client, "RCPT TO:<user@example.com>")
	readResponse(t, client, 250)
	writeLine(t, client, "DATA")
	readResponse(t, client, 354)
	writeData(t, client, strings.Join([]string{
		"From: Sender <sender@example.com>",
		"To: User <user@example.com>",
		"Subject: SMTP capture",
		"Received: from upstream.example by local.test",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Plain text body.",
	}, "\r\n"))
	readResponse(t, client, 250)
	writeLine(t, client, "QUIT")
	readResponse(t, client, 221)

	messages := store.List()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	msg := messages[0]
	if msg.Provider != "smtp" {
		t.Fatalf("provider = %q, want smtp", msg.Provider)
	}
	if msg.Subject != "SMTP capture" {
		t.Fatalf("subject = %q", msg.Subject)
	}
	if msg.From != "Sender <sender@example.com>" {
		t.Fatalf("from = %q", msg.From)
	}
	if len(msg.To) != 1 || msg.To[0] != "User <user@example.com>" {
		t.Fatalf("to = %#v", msg.To)
	}
	if strings.TrimSpace(msg.Text) != "Plain text body." {
		t.Fatalf("text = %q", msg.Text)
	}
	if _, ok := msg.Headers["Received"]; ok {
		t.Fatalf("expected Received header to be sanitized from headers")
	}
	if strings.Contains(string(msg.Raw), "Received:") {
		t.Fatalf("expected Received header to be sanitized from raw message")
	}
}

func TestSMTPServerUsesEnvelopeFallbacks(t *testing.T) {
	store := mailbox.NewStore()
	_, addr, stop := startTestServer(t, store)
	defer stop()

	client := dialSMTP(t, addr)
	defer client.Close()

	readResponse(t, client, 220)
	writeLine(t, client, "HELO local.test")
	readResponse(t, client, 250)
	writeLine(t, client, "MAIL FROM:<fallback@example.com>")
	readResponse(t, client, 250)
	writeLine(t, client, "RCPT TO:<first@example.com>")
	readResponse(t, client, 250)
	writeLine(t, client, "RCPT TO:<second@example.com>")
	readResponse(t, client, 250)
	writeLine(t, client, "DATA")
	readResponse(t, client, 354)
	writeData(t, client, "Subject: Missing address headers\r\n\r\nBody")
	readResponse(t, client, 250)

	messages := store.List()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	msg := messages[0]
	if msg.From != "fallback@example.com" {
		t.Fatalf("from = %q", msg.From)
	}
	if got, want := strings.Join(msg.To, ","), "first@example.com,second@example.com"; got != want {
		t.Fatalf("to = %q, want %q", got, want)
	}
}

func TestSMTPServerRejectsOversizeMessage(t *testing.T) {
	store := mailbox.NewStore()
	_, addr, stop := startTestServer(t, store, func(server *Server) {
		server.MaxMessageBytes = 64
	})
	defer stop()

	client := dialSMTP(t, addr)
	defer client.Close()

	readResponse(t, client, 220)
	writeLine(t, client, "EHLO local.test")
	readResponse(t, client, 250)
	writeLine(t, client, "MAIL FROM:<sender@example.com>")
	readResponse(t, client, 250)
	writeLine(t, client, "RCPT TO:<user@example.com>")
	readResponse(t, client, 250)
	writeLine(t, client, "DATA")
	readResponse(t, client, 354)
	writeData(t, client, "Subject: Too big\r\n\r\n"+strings.Repeat("a", 200))
	readResponse(t, client, 552)

	if got := len(store.List()); got != 0 {
		t.Fatalf("expected oversize message to be rejected, got %d stored", got)
	}
}

func startTestServer(t *testing.T, store *mailbox.Store, configure ...func(*Server)) (*Server, string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := New(listener.Addr().String(), store)
	for _, fn := range configure {
		fn(server)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	stop := func() {
		shutdownServer(t, server)
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("server returned error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("server did not stop")
		}
	}
	return server, listener.Addr().String(), stop
}

func shutdownServer(t *testing.T, server *Server) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func dialSMTP(t *testing.T, addr string) *textproto.Conn {
	t.Helper()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return textproto.NewConn(conn)
}

func writeLine(t *testing.T, client *textproto.Conn, line string) {
	t.Helper()

	if err := client.PrintfLine("%s", line); err != nil {
		t.Fatal(err)
	}
}

func writeData(t *testing.T, client *textproto.Conn, raw string) {
	t.Helper()

	writer := client.DotWriter()
	if _, err := writer.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func readResponse(t *testing.T, client *textproto.Conn, code int) string {
	t.Helper()

	_, message, err := client.ReadResponse(code)
	if err != nil {
		t.Fatal(err)
	}
	return message
}
