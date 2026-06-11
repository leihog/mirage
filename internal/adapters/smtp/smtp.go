package smtp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"github.com/leihog/mirage/internal/ingest"
	"github.com/leihog/mirage/internal/mailbox"
	mailmime "github.com/leihog/mirage/internal/mime"
)

type Store interface {
	Add(mailbox.Message) mailbox.Message
}

type Server struct {
	Addr            string
	Hostname        string
	MaxMessageBytes int64
	Store           Store

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

func New(addr string, store Store) *Server {
	return &Server{
		Addr:            addr,
		Hostname:        "mirage.local",
		MaxMessageBytes: mailbox.DefaultMaxMessageBytes,
		Store:           store,
	}
}

func (s *Server) ListenAndServe() error {
	listener, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	return s.Serve(listener)
}

func (s *Server) Serve(listener net.Listener) error {
	s.mu.Lock()
	s.listener = listener
	if s.conns == nil {
		s.conns = map[net.Conn]struct{}{}
	}
	s.mu.Unlock()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	listener := s.listener
	s.mu.Unlock()

	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			return err
		}
	}

	s.mu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (s *Server) handleConn(raw net.Conn) {
	s.mu.Lock()
	if s.conns == nil {
		s.conns = map[net.Conn]struct{}{}
	}
	s.conns[raw] = struct{}{}
	s.mu.Unlock()

	defer raw.Close()
	defer func() {
		s.mu.Lock()
		delete(s.conns, raw)
		s.mu.Unlock()
	}()

	_ = raw.SetDeadline(time.Now().Add(10 * time.Minute))
	conn := textproto.NewConn(raw)
	defer conn.Close()

	hostname := s.Hostname
	if hostname == "" {
		hostname = "mirage.local"
	}
	maxBytes := s.MaxMessageBytes
	if maxBytes <= 0 {
		maxBytes = mailbox.DefaultMaxMessageBytes
	}

	conn.PrintfLine("220 %s ESMTP Mirage", hostname)

	var mailStarted bool
	var envelopeFrom string
	var recipients []string

	for {
		line, err := conn.ReadLine()
		if err != nil {
			return
		}
		cmd, arg := splitCommand(line)

		switch cmd {
		case "HELO", "EHLO":
			mailStarted = false
			envelopeFrom = ""
			recipients = nil
			conn.PrintfLine("250-%s", hostname)
			conn.PrintfLine("250-8BITMIME")
			conn.PrintfLine("250-SMTPUTF8")
			conn.PrintfLine("250 SIZE %d", maxBytes)

		case "MAIL":
			value, ok := pathArg(arg, "FROM:")
			if !ok {
				conn.PrintfLine("501 5.5.4 Syntax: MAIL FROM:<address>")
				continue
			}
			mailStarted = true
			envelopeFrom = value
			recipients = nil
			conn.PrintfLine("250 2.1.0 OK")

		case "RCPT":
			if !mailStarted {
				conn.PrintfLine("503 5.5.1 MAIL command required first")
				continue
			}
			value, ok := pathArg(arg, "TO:")
			if !ok {
				conn.PrintfLine("501 5.5.4 Syntax: RCPT TO:<address>")
				continue
			}
			recipients = append(recipients, value)
			conn.PrintfLine("250 2.1.5 OK")

		case "DATA":
			if !mailStarted || len(recipients) == 0 {
				conn.PrintfLine("503 5.5.1 MAIL and RCPT commands required first")
				continue
			}
			conn.PrintfLine("354 End data with <CR><LF>.<CR><LF>")
			msg, err := s.readMessage(conn.DotReader(), maxBytes, envelopeFrom, recipients)
			if err != nil {
				slog.Warn("smtp message rejected", "error", err)
				conn.PrintfLine("552 5.3.4 Message rejected: %v", err)
				return
			}
			msg = s.Store.Add(msg)
			mailStarted = false
			envelopeFrom = ""
			recipients = nil
			conn.PrintfLine("250 2.0.0 OK: queued as %s", msg.ID)

		case "RSET":
			mailStarted = false
			envelopeFrom = ""
			recipients = nil
			conn.PrintfLine("250 2.0.0 OK")

		case "NOOP":
			conn.PrintfLine("250 2.0.0 OK")

		case "QUIT":
			conn.PrintfLine("221 2.0.0 Bye")
			return

		default:
			conn.PrintfLine("502 5.5.1 Command not implemented")
		}
	}
}

func (s *Server) readMessage(body io.Reader, maxBytes int64, envelopeFrom string, recipients []string) (mailbox.Message, error) {
	limited := &io.LimitedReader{R: body, N: maxBytes + 1}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return mailbox.Message{}, err
	}
	if int64(len(raw)) > maxBytes {
		return mailbox.Message{}, fmt.Errorf("message exceeds %d bytes", maxBytes)
	}

	msg, err := ingest.ParseRaw(raw)
	if err != nil {
		return mailbox.Message{}, err
	}
	msg.Provider = "smtp"
	msg.Raw = ingest.SanitizeRaw(raw)
	if msg.From == "" {
		msg.From = mailmime.ParseAddressField(envelopeFrom)
	}
	if len(msg.To) == 0 {
		msg.To = parseEnvelopeRecipients(recipients)
	}
	return msg, nil
}

func splitCommand(line string) (string, string) {
	line = strings.TrimRight(line, "\r\n")
	cmd, arg, ok := strings.Cut(line, " ")
	if !ok {
		return strings.ToUpper(line), ""
	}
	return strings.ToUpper(cmd), strings.TrimSpace(arg)
}

func pathArg(arg, prefix string) (string, bool) {
	if !strings.HasPrefix(strings.ToUpper(arg), prefix) {
		return "", false
	}
	value := strings.TrimSpace(arg[len(prefix):])
	if value == "" {
		return "", false
	}
	if strings.HasPrefix(value, "<") {
		end := strings.IndexByte(value, '>')
		if end < 0 {
			return "", false
		}
		value = value[1:end]
	} else {
		value = strings.Fields(value)[0]
	}
	if prefix == "TO:" && strings.TrimSpace(value) == "" {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func parseEnvelopeRecipients(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		parsed := mailmime.ParseAddressField(value)
		if parsed != "" {
			out = append(out, parsed)
		}
	}
	return out
}
