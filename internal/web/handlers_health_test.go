package web

import (
	"github.com/leihog/mirage/internal/mailbox"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzReturnsNoContent(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", res.Code, res.Body.String())
	}
	if res.Body.Len() != 0 {
		t.Fatalf("expected empty healthz body, got %q", res.Body.String())
	}
}
