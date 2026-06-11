package mime

import (
	"encoding/base64"
	"fmt"
	"io"
	stdmime "mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"slices"
	"sort"
	"strings"
)

type Message struct {
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	Headers     map[string][]string
	Text        string
	HTML        string
	Attachments []Attachment
}

type Attachment struct {
	Name        string
	ContentType string
	Size        int64
	ContentID   string
	Inline      bool
	Data        []byte
}

func Parse(raw []byte) (Message, error) {
	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return Message{}, err
	}

	msg := Message{
		From:    ParseAddressField(parsed.Header.Get("From")),
		To:      ParseAddressFields(parsed.Header["To"]),
		Cc:      ParseAddressFields(parsed.Header["Cc"]),
		Bcc:     ParseAddressFields(parsed.Header["Bcc"]),
		Subject: parsed.Header.Get("Subject"),
		Headers: map[string][]string{},
	}
	for key, values := range parsed.Header {
		msg.Headers[key] = slices.Clone(values)
	}

	if err := parseEntity(parsed.Header, parsed.Body, &msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func HeaderValue(headers map[string][]string, key string) string {
	values := HeaderValues(headers, key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func HeaderValues(headers map[string][]string, key string) []string {
	var matched []string
	for headerKey := range headers {
		if strings.EqualFold(headerKey, key) {
			matched = append(matched, headerKey)
		}
	}
	sort.Strings(matched)
	var out []string
	for _, headerKey := range matched {
		out = append(out, headers[headerKey]...)
	}
	return out
}

func ParseAddressField(value string) string {
	addresses := ParseAddressFields([]string{value})
	if len(addresses) == 0 {
		return strings.TrimSpace(value)
	}
	return addresses[0]
}

func ParseAddressFields(values []string) []string {
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
			addresses = append(addresses, DisplayAddress(addr))
		}
	}
	return addresses
}

func DisplayAddress(addr *mail.Address) string {
	if addr == nil {
		return ""
	}
	name := strings.TrimSpace(strings.Trim(addr.Name, `"`))
	address := strings.TrimSpace(addr.Address)
	if name == "" {
		return address
	}
	if address == "" {
		return name
	}
	return name + " <" + address + ">"
}

func parseEntity(header mail.Header, body io.Reader, msg *Message) error {
	contentType := header.Get("Content-Type")
	mediaType, params, err := stdmime.ParseMediaType(contentType)
	if err != nil {
		mediaType = "text/plain"
	}

	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return fmt.Errorf("missing multipart boundary")
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if err := parsePart(part, msg); err != nil {
				return err
			}
		}
	}

	decoded, err := readDecodedBody(header, body)
	if err != nil {
		return err
	}
	assignBodyOrAttachment(mediaType, header, decoded, msg)
	return nil
}

func parsePart(part *multipart.Part, msg *Message) error {
	header := mail.Header(part.Header)
	contentType := header.Get("Content-Type")
	mediaType, params, err := stdmime.ParseMediaType(contentType)
	if err != nil {
		mediaType = "text/plain"
	}

	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return fmt.Errorf("missing multipart boundary")
		}
		reader := multipart.NewReader(part, boundary)
		for {
			nested, err := reader.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if err := parsePart(nested, msg); err != nil {
				return err
			}
		}
	}

	decoded, err := readDecodedBody(header, part)
	if err != nil {
		return err
	}
	assignBodyOrAttachment(mediaType, header, decoded, msg)
	return nil
}

func assignBodyOrAttachment(mediaType string, header mail.Header, body []byte, msg *Message) {
	disposition, _, _ := stdmime.ParseMediaType(header.Get("Content-Disposition"))
	filename := attachmentName(header)
	contentID := NormalizeContentID(header.Get("Content-ID"))
	if filename != "" || strings.EqualFold(disposition, "attachment") || strings.EqualFold(disposition, "inline") || (contentID != "" && !strings.HasPrefix(strings.ToLower(mediaType), "text/")) {
		name := filename
		if name == "" {
			name = contentID
		}
		msg.Attachments = append(msg.Attachments, Attachment{
			Name:        name,
			ContentType: header.Get("Content-Type"),
			Size:        int64(len(body)),
			ContentID:   contentID,
			Inline:      strings.EqualFold(disposition, "inline"),
			Data:        body,
		})
		return
	}

	switch strings.ToLower(mediaType) {
	case "text/plain":
		if msg.Text == "" {
			msg.Text = string(body)
		}
	case "text/html":
		if msg.HTML == "" {
			msg.HTML = string(body)
		}
	}
}

func NormalizeContentID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "<")
	value = strings.TrimSuffix(value, ">")
	return strings.TrimSpace(value)
}

func attachmentName(header mail.Header) string {
	if filename := header.Get("Content-Disposition"); filename != "" {
		_, params, err := stdmime.ParseMediaType(filename)
		if err == nil && params["filename"] != "" {
			return params["filename"]
		}
	}
	if contentType := header.Get("Content-Type"); contentType != "" {
		_, params, err := stdmime.ParseMediaType(contentType)
		if err == nil && params["name"] != "" {
			return params["name"]
		}
	}
	return ""
}

func readDecodedBody(header mail.Header, body io.Reader) ([]byte, error) {
	switch strings.ToLower(header.Get("Content-Transfer-Encoding")) {
	case "base64":
		return io.ReadAll(base64.NewDecoder(base64.StdEncoding, body))
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(body))
	default:
		return io.ReadAll(body)
	}
}
