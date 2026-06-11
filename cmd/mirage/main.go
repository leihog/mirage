package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/leihog/mirage/internal/adapters/mailgun"
	smtpadapter "github.com/leihog/mirage/internal/adapters/smtp"
	"github.com/leihog/mirage/internal/mailbox"
	"github.com/leihog/mirage/internal/web"
)

func main() {
	httpAddr := flag.String("http-addr", ":8025", "HTTP listen address")
	smtpAddr := flag.String("smtp-addr", ":1025", "SMTP listen address")
	maxMessages := flag.Int("max-messages", mailbox.DefaultMaxMessages, "maximum stored messages before the oldest are dropped (0 = unlimited)")
	flag.Parse()

	store := mailbox.NewStore()
	store.SetMaxMessages(*maxMessages)
	mux := http.NewServeMux()

	mailgun.Register(mux, store)
	web.Register(mux, store)

	serverCtx, cancelServerCtx := context.WithCancel(context.Background())
	defer cancelServerCtx()

	server := &http.Server{
		Addr:              *httpAddr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return serverCtx
		},
	}
	smtpServer := smtpadapter.New(*smtpAddr, store)

	errCh := make(chan error, 2)

	go func() {
		slog.Info("mirage http listening", "addr", *httpAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server failed: %w", err)
		}
	}()

	go func() {
		slog.Info("mirage smtp listening", "addr", *smtpAddr)
		if err := smtpServer.ListenAndServe(); err != nil {
			errCh <- fmt.Errorf("smtp server failed: %w", err)
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	var runErr error
	select {
	case sig := <-signalCh:
		slog.Info("Shutting down", "reason", "signal", "signal", sig.String())
	case err := <-errCh:
		runErr = err
		slog.Error("server failed", "error", err)
		slog.Info("Shutting down", "reason", "server error")
	}

	cancelServerCtx()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var shutdownErr error
	shutdownErrCh := make(chan error, 2)
	go func() {
		if err := server.Shutdown(shutdownCtx); err != nil {
			shutdownErrCh <- fmt.Errorf("http shutdown failed: %w", err)
			return
		}
		shutdownErrCh <- nil
	}()
	go func() {
		if err := smtpServer.Shutdown(shutdownCtx); err != nil {
			shutdownErrCh <- fmt.Errorf("smtp shutdown failed: %w", err)
			return
		}
		shutdownErrCh <- nil
	}()
	for range 2 {
		shutdownErr = errors.Join(shutdownErr, <-shutdownErrCh)
	}
	if shutdownErr != nil {
		slog.Error("shutdown failed", "error", shutdownErr)
		fmt.Fprintf(os.Stderr, "shutdown failed: %v\n", shutdownErr)
		os.Exit(1)
	}
	slog.Info("Shutdown complete, exiting")
	if runErr != nil {
		os.Exit(1)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}
