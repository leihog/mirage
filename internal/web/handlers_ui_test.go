package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirage/internal/mailbox"
)

func TestHTMLHandlerResolvesCIDImageReferences(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{
		HTML: `<p>Logo</p><img src="cid:mirage-logo">`,
		Attachments: []mailbox.Attachment{{
			Name:        "logo.gif",
			ContentType: "image/gif",
			ContentID:   "mirage-logo",
			Inline:      true,
			Data:        []byte("gif-bytes"),
			Size:        int64(len("gif-bytes")),
		}},
	})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/messages/"+msg.ID+"/html", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	wantURL := "/messages/" + msg.ID + "/attachments/0"
	if !strings.Contains(res.Body.String(), `src="`+wantURL+`"`) {
		t.Fatalf("expected CID URL to be rewritten to %s, got %s", wantURL, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, wantURL, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "image/gif" {
		t.Fatalf("unexpected attachment content type: %s", res.Header().Get("Content-Type"))
	}
	if res.Body.String() != "gif-bytes" {
		t.Fatalf("unexpected attachment body: %q", res.Body.String())
	}
}

func TestMessageListRendersUTCTimestampForClientConversion(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{Subject: "timestamp"})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	timestamp := msg.CreatedAt.UTC().Format(time.RFC3339Nano)
	if !strings.Contains(res.Body.String(), `datetime="`+timestamp+`"`) {
		t.Fatalf("expected UTC datetime %q in response: %s", timestamp, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `data-timestamp="`+timestamp+`"`) {
		t.Fatalf("expected UTC data timestamp %q in response: %s", timestamp, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `data-timestamp-format="inbox"`) {
		t.Fatalf("expected inbox timestamp format marker in response: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `data-time-24-toggle`) || !strings.Contains(res.Body.String(), `data-date-format-select`) {
		t.Fatalf("expected time display settings in response: %s", res.Body.String())
	}
}

func TestMessageDetailDateUsesDisplayTimestampAndHeadersUseRawDate(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{
		Subject: "timestamp",
		Headers: map[string]string{
			"Date": "Tue, 09 Jun 2026 08:15:00 +0000",
		},
	})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/messages/"+msg.ID, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	timestamp := msg.CreatedAt.UTC().Format(time.RFC3339Nano)
	if !strings.Contains(res.Body.String(), `data-timestamp="`+timestamp+`" data-timestamp-format="datetime"`) {
		t.Fatalf("expected detail date to use localizable timestamp %q: %s", timestamp, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `<strong>Date</strong><span>Tue, 09 Jun 2026 08:15:00 &#43;0000</span>`) {
		t.Fatalf("expected headers tab to keep raw Date header: %s", res.Body.String())
	}
}
