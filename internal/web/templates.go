package web

import (
	"fmt"
	"html/template"
	"net/http"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/leihog/mirage/internal/html"
	"github.com/leihog/mirage/internal/mailbox"
)

type headerRow struct {
	Key   string
	Value string
}

func render(w http.ResponseWriter, tmpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseTemplate(name string) *template.Template {
	return template.Must(template.New(name).Funcs(funcs()).ParseFS(
		assetsFS,
		"assets/templates/layout.html",
		"assets/templates/"+name+".html",
	))
}

func funcs() template.FuncMap {
	return template.FuncMap{
		"join": func(values []string) string {
			return strings.Join(values, ", ")
		},
		"has": func(value string) bool {
			return strings.TrimSpace(value) != ""
		},
		"mailboxLine": mailboxLine,
		"senderName":  senderName,
		"messageID":   messageID,
		"dateLine":    dateLine,
		"utcTime": func(value time.Time) string {
			return value.UTC().Format(time.RFC3339Nano)
		},
		"utcClock": func(value time.Time) string {
			return value.UTC().Format("15:04")
		},
		"utcDateTime": func(value time.Time) string {
			return value.UTC().Format("Mon, 2 Jan 2006, 3:04 pm")
		},
		"messageSize": func(msg mailbox.Message) string {
			size := len(msg.Raw) + len(msg.Text) + len(msg.HTML)
			for key, value := range msg.Headers {
				size += len(key) + len(value) + 4
			}
			if size < 1024 {
				return fmt.Sprintf("%d B", size)
			}
			return fmt.Sprintf("%.1f kB", float64(size)/1024)
		},
		"hasHTML":     hasHTML,
		"hasText":     hasText,
		"activeTab":   activeTab,
		"htmlSource":  htmlSource,
		"unsubscribe": unsubscribeAction,
		"rawMessage":  rawMessage,
		"headerRows":  headerRows,
	}
}

func hasHTML(msg mailbox.Message) bool {
	return strings.TrimSpace(msg.HTML) != ""
}

func hasText(msg mailbox.Message) bool {
	return strings.TrimSpace(msg.Text) != ""
}

func activeTab(msg mailbox.Message) string {
	if hasHTML(msg) {
		return "html"
	}
	if hasText(msg) {
		return "text"
	}
	return "raw"
}

func mailboxLine(messages []mailbox.Message) string {
	unread := 0
	for _, msg := range messages {
		if !msg.Viewed {
			unread++
		}
	}
	return fmt.Sprintf("%d %s, %d unread", len(messages), plural(len(messages), "mail", "mails"), unread)
}

func senderName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if address, err := mail.ParseAddress(value); err == nil && strings.TrimSpace(address.Name) != "" {
		return strings.TrimSpace(address.Name)
	}
	if i := strings.Index(value, "<"); i > 0 {
		name := strings.Trim(strings.TrimSpace(value[:i]), `"`)
		if name != "" {
			return name
		}
	}
	if i := strings.Index(value, "@"); i > 0 {
		return value[:i]
	}
	return value
}

func messageID(msg mailbox.Message) string {
	if value := strings.TrimSpace(headerValue(msg.Headers, "Message-Id")); value != "" {
		return value
	}
	return "<" + msg.ID + "@mirage.local>"
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func htmlSource(msg mailbox.Message) template.HTML {
	return html.PrettyHTMLSource(msg.HTML)
}

func dateLine(msg mailbox.Message) string {
	if value := strings.TrimSpace(msg.Headers["Date"]); value != "" {
		return value
	}
	return msg.CreatedAt.UTC().Format(time.RFC1123Z)
}

func headerRows(msg mailbox.Message) []headerRow {
	rows := make([]headerRow, 0, len(msg.Headers)+6)
	seen := map[string]bool{}
	add := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		rows = append(rows, headerRow{Key: key, Value: value})
		seen[strings.ToLower(key)] = true
	}

	add("Content-Type", msg.Headers["Content-Type"])
	add("Date", dateLine(msg))
	add("From", msg.From)
	add("Message-Id", msg.Headers["Message-Id"])
	add("Mime-Version", msg.Headers["Mime-Version"])
	add("Subject", msg.Subject)
	add("To", strings.Join(msg.To, ", "))
	add("Cc", strings.Join(msg.Cc, ", "))
	add("Bcc", strings.Join(msg.Bcc, ", "))

	keys := make([]string, 0, len(msg.Headers))
	for key := range msg.Headers {
		if !seen[strings.ToLower(key)] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		add(key, msg.Headers[key])
	}
	return rows
}
