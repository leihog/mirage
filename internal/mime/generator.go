package mime

import (
	"bytes"
	"encoding/base64"
	"fmt"
	stdmime "mime"
	"mime/quotedprintable"
	"net/mail"
	"sort"
	"strings"
	"time"
)

type GenerateOptions struct {
	ID        string
	CreatedAt time.Time
}

func Generate(msg Message, opts GenerateOptions) string {
	var buf bytes.Buffer
	hasText := strings.TrimSpace(msg.Text) != ""
	hasHTML := strings.TrimSpace(msg.HTML) != ""
	hasAttachments := len(msg.Attachments) > 0

	switch {
	case hasAttachments:
		mixedBoundary := uniqueMIMEBoundary("mirage-mixed-"+safeFilenamePart(opts.ID), msg.Text, msg.HTML)
		alternativeBoundary := uniqueMIMEBoundary("mirage-alt-"+safeFilenamePart(opts.ID), msg.Text, msg.HTML, mixedBoundary)
		writeGeneratedMessageHeaders(&buf, msg, opts, stdmime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mixedBoundary}), "")
		if hasText || hasHTML {
			writeMIMEBoundary(&buf, mixedBoundary)
			if hasText && hasHTML {
				writeGeneratedHeader(&buf, "Content-Type", stdmime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alternativeBoundary}))
				buf.WriteString("\r\n")
				writeAlternativeParts(&buf, alternativeBoundary, msg)
			} else if hasHTML {
				writeTextPart(&buf, "text/html; charset=utf-8", msg.HTML)
			} else {
				writeTextPart(&buf, "text/plain; charset=utf-8", msg.Text)
			}
		}
		for _, attachment := range msg.Attachments {
			if len(attachment.Data) == 0 {
				continue
			}
			writeMIMEBoundary(&buf, mixedBoundary)
			writeAttachmentPart(&buf, attachment)
		}
		writeClosingMIMEBoundary(&buf, mixedBoundary)
	case hasText && hasHTML:
		boundary := uniqueMIMEBoundary("mirage-alt-"+safeFilenamePart(opts.ID), msg.Text, msg.HTML)
		writeGeneratedMessageHeaders(&buf, msg, opts, stdmime.FormatMediaType("multipart/alternative", map[string]string{"boundary": boundary}), "")
		writeAlternativeParts(&buf, boundary, msg)
	case hasHTML:
		writeGeneratedMessageHeaders(&buf, msg, opts, "text/html; charset=utf-8", "quoted-printable")
		buf.WriteString(encodeQuotedPrintable(msg.HTML))
		ensureTrailingCRLF(&buf)
	default:
		writeGeneratedMessageHeaders(&buf, msg, opts, "text/plain; charset=utf-8", "quoted-printable")
		buf.WriteString(encodeQuotedPrintable(msg.Text))
		ensureTrailingCRLF(&buf)
	}

	return buf.String()
}

func writeGeneratedMessageHeaders(buf *bytes.Buffer, msg Message, opts GenerateOptions, contentType, contentTransferEncoding string) {
	writeGeneratedHeader(buf, "From", formatAddressHeader([]string{msg.From}))
	writeGeneratedHeader(buf, "To", formatAddressHeader(msg.To))
	writeGeneratedHeader(buf, "Cc", formatAddressHeader(msg.Cc))
	writeGeneratedHeader(buf, "Bcc", formatAddressHeader(msg.Bcc))
	writeGeneratedHeader(buf, "Subject", encodeUnstructuredHeader(msg.Subject))
	writeGeneratedHeader(buf, "Date", opts.CreatedAt.UTC().Format(time.RFC1123Z))
	writeGeneratedHeader(buf, "Message-ID", messageID(msg, opts))
	writeGeneratedHeader(buf, "MIME-Version", "1.0")
	keys := make([]string, 0, len(msg.Headers))
	for key := range msg.Headers {
		lower := strings.ToLower(key)
		if lower == "from" || lower == "to" || lower == "cc" || lower == "bcc" || lower == "subject" || lower == "date" || lower == "message-id" || lower == "mime-version" || strings.HasPrefix(lower, "content-") {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeGeneratedHeader(buf, key, encodeUnstructuredHeader(msg.Headers[key]))
	}
	writeGeneratedHeader(buf, "Content-Type", contentType)
	writeGeneratedHeader(buf, "Content-Transfer-Encoding", contentTransferEncoding)
	buf.WriteString("\r\n")
}

func messageID(msg Message, opts GenerateOptions) string {
	if value := strings.TrimSpace(headerValue(msg.Headers, "Message-Id")); value != "" {
		return sanitizeHeaderValue(value)
	}
	id := safeFilenamePart(opts.ID)
	if id == "" {
		id = "message"
	}
	return "<" + id + "@mirage.local>"
}

func headerValue(headers map[string]string, key string) string {
	for headerKey, value := range headers {
		if strings.EqualFold(headerKey, key) {
			return value
		}
	}
	return ""
}

func writeAlternativeParts(buf *bytes.Buffer, boundary string, msg Message) {
	writeMIMEBoundary(buf, boundary)
	writeTextPart(buf, "text/plain; charset=utf-8", msg.Text)
	writeMIMEBoundary(buf, boundary)
	writeTextPart(buf, "text/html; charset=utf-8", msg.HTML)
	writeClosingMIMEBoundary(buf, boundary)
}

func writeTextPart(buf *bytes.Buffer, contentType, body string) {
	if contentType != "" {
		writeGeneratedHeader(buf, "Content-Type", contentType)
	}
	writeGeneratedHeader(buf, "Content-Transfer-Encoding", "quoted-printable")
	buf.WriteString("\r\n")
	buf.WriteString(encodeQuotedPrintable(body))
	ensureTrailingCRLF(buf)
}

func writeAttachmentPart(buf *bytes.Buffer, attachment Attachment) {
	contentType := attachmentContentType(attachment)
	if attachment.Name != "" {
		mediaType, params, err := stdmime.ParseMediaType(contentType)
		if err != nil {
			mediaType = "application/octet-stream"
			params = map[string]string{}
		}
		if params == nil {
			params = map[string]string{}
		}
		if params["name"] == "" {
			params["name"] = attachment.Name
		}
		contentType = stdmime.FormatMediaType(mediaType, params)
	}

	disposition := "attachment"
	if attachment.Inline {
		disposition = "inline"
	}
	dispositionParams := map[string]string{}
	if attachment.Name != "" {
		dispositionParams["filename"] = attachment.Name
	}

	writeGeneratedHeader(buf, "Content-Type", contentType)
	writeGeneratedHeader(buf, "Content-Transfer-Encoding", "base64")
	writeGeneratedHeader(buf, "Content-Disposition", stdmime.FormatMediaType(disposition, dispositionParams))
	if contentID := sanitizeContentID(attachment.ContentID); contentID != "" {
		writeGeneratedHeader(buf, "Content-ID", "<"+contentID+">")
	}
	buf.WriteString("\r\n")
	writeBase64Lines(buf, attachment.Data)
	ensureTrailingCRLF(buf)
}

func attachmentContentType(attachment Attachment) string {
	if attachment.ContentType != "" {
		return attachment.ContentType
	}
	return "application/octet-stream"
}

func writeMIMEBoundary(buf *bytes.Buffer, boundary string) {
	buf.WriteString("--")
	buf.WriteString(boundary)
	buf.WriteString("\r\n")
}

func writeClosingMIMEBoundary(buf *bytes.Buffer, boundary string) {
	buf.WriteString("--")
	buf.WriteString(boundary)
	buf.WriteString("--\r\n")
}

func writeGeneratedHeader(buf *bytes.Buffer, key, value string) {
	key = strings.TrimSpace(key)
	value = sanitizeHeaderValue(value)
	if key == "" || value == "" || !validHeaderFieldName(key) {
		return
	}
	writeFoldedHeader(buf, key, value)
}

func writeFoldedHeader(buf *bytes.Buffer, key, value string) {
	prefix := key + ": "
	remaining := value
	for {
		limit := 78 - len(prefix)
		if limit < 20 {
			limit = 78
		}
		if len(prefix)+len(remaining) <= 78 {
			buf.WriteString(prefix)
			buf.WriteString(remaining)
			buf.WriteString("\r\n")
			return
		}
		cut := headerFoldCut(remaining, limit)
		buf.WriteString(prefix)
		buf.WriteString(strings.TrimRight(remaining[:cut], " \t"))
		buf.WriteString("\r\n")
		remaining = strings.TrimLeft(remaining[cut:], " \t")
		if remaining == "" {
			return
		}
		prefix = " "
	}
}

func headerFoldCut(value string, preferred int) int {
	if preferred > len(value) {
		preferred = len(value)
	}
	if cut := strings.LastIndexAny(value[:preferred], " \t"); cut > 0 {
		return cut
	}
	hardLimit := 900
	if hardLimit > len(value) {
		hardLimit = len(value)
	}
	if cut := strings.LastIndexAny(value[:hardLimit], " \t"); cut > 0 {
		return cut
	}
	return hardLimit
}

func validHeaderFieldName(key string) bool {
	for _, ch := range key {
		if ch < 33 || ch > 126 || ch == ':' {
			return false
		}
	}
	return true
}

func sanitizeHeaderValue(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	return strings.Join(strings.Fields(value), " ")
}

func encodeUnstructuredHeader(value string) string {
	value = sanitizeHeaderValue(value)
	for _, ch := range value {
		if ch < 32 || ch > 126 {
			return stdmime.QEncoding.Encode("utf-8", value)
		}
	}
	return value
}

func formatAddressHeader(values []string) string {
	formatted := make([]string, 0, len(values))
	for _, value := range values {
		value = sanitizeHeaderValue(value)
		if value == "" {
			continue
		}
		addresses, err := mail.ParseAddressList(value)
		if err != nil {
			formatted = append(formatted, encodeUnstructuredHeader(value))
			continue
		}
		for _, address := range addresses {
			formatted = append(formatted, address.String())
		}
	}
	return strings.Join(formatted, ", ")
}

func encodeQuotedPrintable(value string) string {
	var buf bytes.Buffer
	writer := quotedprintable.NewWriter(&buf)
	_, _ = writer.Write([]byte(normalizeCRLF(value)))
	_ = writer.Close()
	return normalizeCRLF(buf.String())
}

func normalizeCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func ensureTrailingCRLF(buf *bytes.Buffer) {
	if !strings.HasSuffix(buf.String(), "\r\n") {
		buf.WriteString("\r\n")
	}
}

func writeBase64Lines(buf *bytes.Buffer, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	for len(encoded) > 76 {
		buf.WriteString(encoded[:76])
		buf.WriteString("\r\n")
		encoded = encoded[76:]
	}
	if encoded != "" {
		buf.WriteString(encoded)
		buf.WriteString("\r\n")
	}
}

func uniqueMIMEBoundary(base string, values ...string) string {
	base = safeFilenamePart(base)
	if base == "" {
		base = "mirage-boundary"
	}
	if len(base) > 60 {
		base = base[:60]
	}
	candidate := base
	for i := 2; boundaryInValues(candidate, values); i++ {
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return candidate
}

func boundaryInValues(boundary string, values []string) bool {
	delimiter := "--" + boundary
	for _, value := range values {
		if strings.Contains(value, delimiter) {
			return true
		}
	}
	return false
}

func safeFilenamePart(value string) string {
	var out strings.Builder
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			out.WriteRune(ch)
		}
	}
	if out.Len() == 0 {
		return "email"
	}
	return out.String()
}

func sanitizeContentID(value string) string {
	value = sanitizeHeaderValue(value)
	value = strings.TrimPrefix(value, "<")
	value = strings.TrimSuffix(value, ">")
	return value
}
