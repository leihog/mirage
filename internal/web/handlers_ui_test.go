package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leihog/mirage/internal/mailbox"
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
	if res.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("expected sandbox CSP on html view, got %q", res.Header().Get("Content-Security-Policy"))
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

func TestUnknownPathsReturnNotFound(t *testing.T) {
	store := mailbox.NewStore()
	mux := http.NewServeMux()
	Register(mux, store)

	for _, path := range []string{"/nope", "/messages/", "/favicon.ico"} {
		res := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for %s, got %d", path, res.Code)
		}
	}
}

func TestAttachmentDispositionQuotesFilename(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{
		Attachments: []mailbox.Attachment{{
			Name:        `report".html\`,
			ContentType: "text/html",
			Data:        []byte("<p>attachment</p>"),
		}},
	})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/messages/"+msg.ID+"/attachments/0", nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("expected sandbox CSP on attachment, got %q", res.Header().Get("Content-Security-Policy"))
	}
	wantDisposition := `attachment; filename="report\".html\\"`
	if disposition := res.Header().Get("Content-Disposition"); disposition != wantDisposition {
		t.Fatalf("unexpected content disposition: %q", disposition)
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

func TestMessageDetailUsesTextTabWhenHTMLBodyIsMissing(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{
		Subject: "text only",
		Text:    "plain text body",
	})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/messages/"+msg.ID, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	body := res.Body.String()
	if !strings.Contains(body, `data-tab="html" disabled aria-disabled="true"`) {
		t.Fatalf("expected HTML tab to be disabled: %s", body)
	}
	if !strings.Contains(body, `data-tab="source" disabled aria-disabled="true"`) {
		t.Fatalf("expected HTML Source tab to be disabled: %s", body)
	}
	if !strings.Contains(body, `class="tab active" type="button" data-tab="text"`) {
		t.Fatalf("expected Text tab to be active: %s", body)
	}
	if !strings.Contains(body, `id="tab-text" class="panel code-panel tab-panel active"`) {
		t.Fatalf("expected Text panel to be active: %s", body)
	}
	if strings.Contains(body, `id="tab-html" class="panel preview tab-panel active"`) {
		t.Fatalf("HTML panel should not be active without HTML: %s", body)
	}
}

func TestMessageDetailHidesTextTabWhenTextBodyIsMissing(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{
		Subject: "html only",
		HTML:    "<p>html body</p>",
	})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/messages/"+msg.ID, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	body := res.Body.String()
	if !strings.Contains(body, `class="tab active" type="button" data-tab="html"`) {
		t.Fatalf("expected HTML tab to be active: %s", body)
	}
	if strings.Contains(body, `data-tab="text"`) {
		t.Fatalf("expected Text tab to be hidden: %s", body)
	}
	if strings.Contains(body, `id="tab-text"`) {
		t.Fatalf("expected Text panel to be hidden: %s", body)
	}
}

func TestMessageDetailFallsBackToRawTabWhenNoRenderedBodiesExist(t *testing.T) {
	store := mailbox.NewStore()
	msg := store.Add(mailbox.Message{Subject: "headers only"})

	mux := http.NewServeMux()
	Register(mux, store)

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/messages/"+msg.ID, nil)
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	body := res.Body.String()
	if !strings.Contains(body, `class="tab active" type="button" data-tab="raw"`) {
		t.Fatalf("expected Raw tab to be active: %s", body)
	}
	if strings.Contains(body, `data-tab="text"`) {
		t.Fatalf("expected Text tab to be hidden: %s", body)
	}
	if strings.Contains(body, `id="tab-html" class="panel preview tab-panel active"`) {
		t.Fatalf("HTML panel should not be active without HTML: %s", body)
	}
}
