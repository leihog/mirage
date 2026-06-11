package mailgun

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/leihog/mirage/internal/ingest"
	"github.com/leihog/mirage/internal/mailbox"
	mailmime "github.com/leihog/mirage/internal/mime"
)

const maxRequestBody = 32 << 20

type Store interface {
	Add(mailbox.Message) mailbox.Message
}

func Register(mux *http.ServeMux, store Store) {
	mux.HandleFunc("POST /v3/{domain}/messages", func(w http.ResponseWriter, r *http.Request) {
		handleMessage(w, r, store)
	})
	mux.HandleFunc("POST /v3/{domain}/messages.mime", func(w http.ResponseWriter, r *http.Request) {
		handleMIMEMessage(w, r, store)
	})
}

func handleMessage(w http.ResponseWriter, r *http.Request, store Store) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(maxRequestBody); err != nil {
			http.Error(w, "invalid multipart form: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form: "+err.Error(), http.StatusBadRequest)
		return
	}

	attachments, err := readAttachmentMetadata(r.MultipartForm)
	if err != nil {
		http.Error(w, "invalid attachment: "+err.Error(), http.StatusBadRequest)
		return
	}

	msg := mailbox.Message{
		Provider:    "mailgun",
		Domain:      r.PathValue("domain"),
		From:        mailmime.ParseAddressField(first(r.Form["from"])),
		To:          mailmime.ParseAddressFields(r.Form["to"]),
		Cc:          mailmime.ParseAddressFields(r.Form["cc"]),
		Bcc:         mailmime.ParseAddressFields(r.Form["bcc"]),
		Subject:     first(r.Form["subject"]),
		Text:        first(r.Form["text"]),
		HTML:        first(r.Form["html"]),
		Headers:     sendableHeaders(prefixedFields(r.Form, "h:")),
		Variables:   prefixedFields(r.Form, "v:"),
		Options:     prefixedFields(r.Form, "o:"),
		Attachments: attachments,
	}
	msg = store.Add(msg)

	writeMailgunResponse(w, msg)
}

func handleMIMEMessage(w http.ResponseWriter, r *http.Request, store Store) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(maxRequestBody); err != nil {
			http.Error(w, "invalid multipart form: "+err.Error(), http.StatusBadRequest)
			return
		}
		raw, filename, err := firstUploadedFile(r.MultipartForm, "message")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		msg, err := ingest.ParseRaw(raw)
		if err != nil {
			http.Error(w, "invalid MIME message: "+err.Error(), http.StatusBadRequest)
			return
		}
		raw = ingest.SanitizeRaw(raw)
		msg.Provider = "mailgun"
		msg.Domain = r.PathValue("domain")
		msg.Raw = raw
		if filename != "" {
			msg.Attachments = append(msg.Attachments, mailbox.Attachment{
				Name:        filename,
				Size:        int64(len(raw)),
				ContentType: "message/rfc822",
				Data:        raw,
			})
		}
		msg = store.Add(msg)
		writeMailgunResponse(w, msg)
		return
	}

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	msg, err := ingest.ParseRaw(raw)
	if err != nil {
		http.Error(w, "invalid MIME message: "+err.Error(), http.StatusBadRequest)
		return
	}
	raw = ingest.SanitizeRaw(raw)
	msg.Provider = "mailgun"
	msg.Domain = r.PathValue("domain")
	msg.Raw = raw
	msg = store.Add(msg)
	writeMailgunResponse(w, msg)
}

func writeMailgunResponse(w http.ResponseWriter, msg mailbox.Message) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":      "<" + msg.ID + "@mirage.local>",
		"message": "Queued. Thank you.",
	})
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func prefixedFields(values map[string][]string, prefix string) map[string][]string {
	out := map[string][]string{}
	for key, fieldValues := range values {
		if strings.HasPrefix(key, prefix) {
			out[strings.TrimPrefix(key, prefix)] = slices.Clone(fieldValues)
		}
	}
	return out
}

func readAttachmentMetadata(form *multipart.Form) ([]mailbox.Attachment, error) {
	if form == nil {
		return nil, nil
	}

	var attachments []mailbox.Attachment
	for field, files := range form.File {
		if field != "attachment" && field != "inline" {
			continue
		}
		for _, file := range files {
			data, err := readUploadedFile(file)
			if err != nil {
				return nil, err
			}
			attachments = append(attachments, mailbox.Attachment{
				Name:        file.Filename,
				ContentType: file.Header.Get("Content-Type"),
				Size:        int64(len(data)),
				ContentID:   mailmime.NormalizeContentID(file.Header.Get("Content-ID")),
				Inline:      field == "inline",
				Data:        data,
			})
		}
	}
	sort.SliceStable(attachments, func(i, j int) bool {
		return attachments[i].Name < attachments[j].Name
	})
	return attachments, nil
}

func readUploadedFile(header *multipart.FileHeader) ([]byte, error) {
	file, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func firstUploadedFile(form *multipart.Form, field string) ([]byte, string, error) {
	if form == nil || len(form.File[field]) == 0 {
		return nil, "", fmt.Errorf("missing %q file field", field)
	}

	header := form.File[field][0]
	file, err := header.Open()
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	return raw, header.Filename, err
}

func sendableHeaders(headers map[string][]string) map[string][]string {
	out := map[string][]string{}
	for key, values := range headers {
		if blockedGeneratedFormHeader(key) {
			continue
		}
		out[key] = values
	}
	return out
}

func blockedGeneratedFormHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "date", "delivered-to", "message-id", "received", "return-path":
		return true
	default:
		return false
	}
}
