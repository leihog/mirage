package mailgun

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"sort"
	"strings"

	"mirage/internal/mailbox"
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

	msg := mailbox.Message{
		Provider:    "mailgun",
		Domain:      r.PathValue("domain"),
		From:        first(r.Form["from"]),
		To:          parseAddressFields(r.Form["to"]),
		Cc:          parseAddressFields(r.Form["cc"]),
		Bcc:         parseAddressFields(r.Form["bcc"]),
		Subject:     first(r.Form["subject"]),
		Text:        first(r.Form["text"]),
		HTML:        first(r.Form["html"]),
		Headers:     prefixedFields(r.Form, "h:"),
		Variables:   prefixedFields(r.Form, "v:"),
		Options:     prefixedFields(r.Form, "o:"),
		Attachments: readAttachmentMetadata(r.MultipartForm),
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
		msg, err := parseMIME(raw)
		if err != nil {
			http.Error(w, "invalid MIME message: "+err.Error(), http.StatusBadRequest)
			return
		}
		msg.Provider = "mailgun"
		msg.Domain = r.PathValue("domain")
		msg.Raw = raw
		if filename != "" {
			msg.Attachments = append(msg.Attachments, mailbox.Attachment{Name: filename, Size: int64(len(raw)), ContentType: "message/rfc822"})
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
	msg, err := parseMIME(raw)
	if err != nil {
		http.Error(w, "invalid MIME message: "+err.Error(), http.StatusBadRequest)
		return
	}
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

func parseAddressFields(values []string) []string {
	var addresses []string
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := mail.ParseAddressList(value)
		if err != nil {
			addresses = append(addresses, strings.TrimSpace(value))
			continue
		}
		for _, addr := range parsed {
			addresses = append(addresses, addr.String())
		}
	}
	return addresses
}

func prefixedFields(values map[string][]string, prefix string) map[string]string {
	out := map[string]string{}
	for key, fieldValues := range values {
		if strings.HasPrefix(key, prefix) {
			out[strings.TrimPrefix(key, prefix)] = first(fieldValues)
		}
	}
	return out
}

func readAttachmentMetadata(form *multipart.Form) []mailbox.Attachment {
	if form == nil {
		return nil
	}

	var attachments []mailbox.Attachment
	for field, files := range form.File {
		if field != "attachment" && field != "inline" {
			continue
		}
		for _, file := range files {
			attachments = append(attachments, mailbox.Attachment{
				Name:        file.Filename,
				ContentType: file.Header.Get("Content-Type"),
				Size:        file.Size,
			})
		}
	}
	sort.SliceStable(attachments, func(i, j int) bool {
		return attachments[i].Name < attachments[j].Name
	})
	return attachments
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

func parseMIME(raw []byte) (mailbox.Message, error) {
	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return mailbox.Message{}, err
	}

	msg := mailbox.Message{
		From:    parsed.Header.Get("From"),
		To:      parseAddressFields(parsed.Header["To"]),
		Cc:      parseAddressFields(parsed.Header["Cc"]),
		Bcc:     parseAddressFields(parsed.Header["Bcc"]),
		Subject: parsed.Header.Get("Subject"),
		Headers: map[string]string{},
	}
	for key, values := range parsed.Header {
		msg.Headers[key] = strings.Join(values, ", ")
	}

	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		body, readErr := io.ReadAll(parsed.Body)
		if readErr != nil {
			return mailbox.Message{}, readErr
		}
		if mediaType == "text/html" {
			msg.HTML = string(body)
		} else {
			msg.Text = string(body)
		}
		return msg, nil
	}

	reader := multipart.NewReader(parsed.Body, params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return mailbox.Message{}, err
		}
		body, err := readPartBody(part)
		if err != nil {
			return mailbox.Message{}, err
		}

		partType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		switch {
		case partType == "text/plain" && msg.Text == "":
			msg.Text = string(body)
		case partType == "text/html" && msg.HTML == "":
			msg.HTML = string(body)
		default:
			if filename := part.FileName(); filename != "" {
				msg.Attachments = append(msg.Attachments, mailbox.Attachment{
					Name:        filename,
					ContentType: part.Header.Get("Content-Type"),
					Size:        int64(len(body)),
				})
			}
		}
	}
	return msg, nil
}

func readPartBody(part *multipart.Part) ([]byte, error) {
	switch strings.ToLower(part.Header.Get("Content-Transfer-Encoding")) {
	case "base64":
		return io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(part))
	default:
		return io.ReadAll(part)
	}
}
