package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"mirage/internal/mailbox"
)

//go:embed assets/templates/*.html assets/static/*
var assetsFS embed.FS

type Store interface {
	List() []mailbox.Message
	Get(string) (mailbox.Message, bool)
	MarkViewed(string) (mailbox.Message, bool)
	Delete(string) bool
	Clear()
}

func Register(mux *http.ServeMux, store Store) {
	app := &app{
		store: store,
		index: parseTemplate("index"),
		show:  parseTemplate("show"),
	}

	staticFS, err := fs.Sub(assetsFS, "assets/static")
	if err != nil {
		panic(err)
	}

	mux.HandleFunc("GET /", app.indexHandler)
	mux.HandleFunc("GET /messages/{id}", app.showHandler)
	mux.HandleFunc("GET /messages/{id}/html", app.htmlHandler)
	mux.HandleFunc("POST /messages/{id}/unsubscribe", app.unsubscribeHandler)
	mux.HandleFunc("POST /messages/{id}/delete", app.deleteHandler)
	mux.HandleFunc("POST /messages/clear", app.clearHandler)
	mux.HandleFunc("GET /api/messages", app.apiMessagesHandler)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(staticFS))))
}

type app struct {
	store Store
	index *template.Template
	show  *template.Template
}

func (a *app) indexHandler(w http.ResponseWriter, r *http.Request) {
	messages := a.store.List()
	data := struct {
		Messages   []mailbox.Message
		SelectedID string
	}{
		Messages: messages,
	}
	render(w, a.index, data)
}

func (a *app) showHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.store.MarkViewed(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	data := struct {
		Messages   []mailbox.Message
		Selected   mailbox.Message
		SelectedID string
	}{
		Messages:   a.store.List(),
		Selected:   msg,
		SelectedID: msg.ID,
	}
	render(w, a.show, data)
}

func (a *app) htmlHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.store.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if strings.TrimSpace(msg.HTML) != "" {
		_, _ = w.Write([]byte(msg.HTML))
		return
	}
	escaped := template.HTMLEscapeString(msg.Text)
	_, _ = w.Write([]byte("<!doctype html><meta charset=\"utf-8\"><pre style=\"white-space:pre-wrap;font:14px/1.5 system-ui,sans-serif\">" + escaped + "</pre>"))
}

func (a *app) deleteHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.store.Delete(id)

	redirect := "/"
	for _, msg := range a.store.List() {
		if msg.ID != id {
			redirect = "/messages/" + msg.ID
			break
		}
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *app) unsubscribeHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.store.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}

	action := unsubscribeAction(msg)
	if action == nil {
		http.NotFound(w, r)
		return
	}

	result := sendOneClickUnsubscribe(action.URL)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (a *app) clearHandler(w http.ResponseWriter, r *http.Request) {
	a.store.Clear()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) apiMessagesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a.store.List())
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
		"dateLine":    dateLine,
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
		"htmlSource":  htmlSource,
		"unsubscribe": unsubscribeAction,
		"rawMessage":  rawMessage,
		"headerRows":  headerRows,
	}
}

type unsubscribeInfo struct {
	URL string
}

type unsubscribeResult struct {
	URL        string `json:"url"`
	Success    bool   `json:"success"`
	StatusCode int    `json:"statusCode"`
	Status     string `json:"status"`
	JSON       any    `json:"json,omitempty"`
	Error      string `json:"error,omitempty"`
}

func unsubscribeAction(msg mailbox.Message) *unsubscribeInfo {
	post := strings.TrimSpace(headerValue(msg.Headers, "List-Unsubscribe-Post"))
	if !strings.EqualFold(post, "List-Unsubscribe=One-Click") {
		return nil
	}

	for _, candidate := range unsubscribeCandidates(headerValue(msg.Headers, "List-Unsubscribe")) {
		parsed, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return &unsubscribeInfo{URL: parsed.String()}
		}
	}
	return nil
}

func sendOneClickUnsubscribe(target string) unsubscribeResult {
	result := unsubscribeResult{URL: target}
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer res.Body.Close()

	result.StatusCode = res.StatusCode
	result.Status = http.StatusText(res.StatusCode)
	result.Success = res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "application/json") && len(strings.TrimSpace(string(body))) > 0 {
		var decoded any
		if err := json.Unmarshal(body, &decoded); err == nil {
			result.JSON = decoded
		}
	}
	return result
}

func headerValue(headers map[string]string, key string) string {
	for headerKey, value := range headers {
		if strings.EqualFold(headerKey, key) {
			return value
		}
	}
	return ""
}

func unsubscribeCandidates(value string) []string {
	var candidates []string
	for i := 0; i < len(value); i++ {
		if value[i] != '<' {
			continue
		}
		end := strings.IndexByte(value[i+1:], '>')
		if end == -1 {
			break
		}
		candidate := strings.TrimSpace(value[i+1 : i+1+end])
		if candidate != "" {
			candidates = append(candidates, candidate)
		}
		i += end + 1
	}
	if len(candidates) > 0 {
		return candidates
	}

	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
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

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func htmlSource(msg mailbox.Message) template.HTML {
	if strings.TrimSpace(msg.HTML) == "" {
		return template.HTML(`<span class="html-muted">(no HTML body)</span>`)
	}
	return template.HTML(colorHTMLSource(prettyHTMLSource(msg.HTML)))
}

func prettyHTMLSource(source string) string {
	source = strings.TrimSpace(source)
	var out strings.Builder
	indent := 0

	writeLine := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(strings.Repeat("  ", max(indent, 0)))
		out.WriteString(value)
	}

	for i := 0; i < len(source); {
		if source[i] != '<' {
			next := strings.IndexByte(source[i:], '<')
			if next == -1 {
				next = len(source) - i
			}
			text := strings.Join(strings.Fields(source[i:i+next]), " ")
			if text != "" {
				writeLine(text)
			}
			i += next
			continue
		}

		end := htmlTagEnd(source, i)
		if end == -1 {
			writeLine(source[i:])
			break
		}

		tag := strings.TrimSpace(source[i : end+1])
		if htmlClosingTag(tag) {
			indent--
		}
		writeLine(tag)
		if htmlOpeningTag(tag) && !htmlVoidTag(tag) {
			indent++
		}
		i = end + 1
	}

	return out.String()
}

func htmlTagEnd(source string, start int) int {
	var quote byte
	for i := start + 1; i < len(source); i++ {
		switch source[i] {
		case '\'', '"':
			if quote == 0 {
				quote = source[i]
			} else if quote == source[i] {
				quote = 0
			}
		case '>':
			if quote == 0 {
				return i
			}
		}
	}
	return -1
}

func htmlClosingTag(tag string) bool {
	return strings.HasPrefix(tag, "</")
}

func htmlOpeningTag(tag string) bool {
	if !strings.HasPrefix(tag, "<") || htmlClosingTag(tag) || strings.HasPrefix(tag, "<!") || strings.HasPrefix(tag, "<?") {
		return false
	}
	return !strings.HasSuffix(strings.TrimSpace(tag), "/>")
}

func htmlVoidTag(tag string) bool {
	switch htmlTagName(tag) {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}

func htmlTagName(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "</")
	tag = strings.TrimPrefix(tag, "<")
	tag = strings.TrimLeft(tag, "!?")
	end := strings.IndexFunc(tag, func(r rune) bool {
		return r == '>' || r == '/' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if end == -1 {
		end = len(tag)
	}
	return strings.ToLower(tag[:end])
}

func colorHTMLSource(source string) string {
	var out strings.Builder
	for i := 0; i < len(source); {
		if source[i] != '<' {
			next := strings.IndexByte(source[i:], '<')
			if next == -1 {
				next = len(source) - i
			}
			out.WriteString(`<span class="html-text">`)
			out.WriteString(template.HTMLEscapeString(source[i : i+next]))
			out.WriteString(`</span>`)
			i += next
			continue
		}

		end := htmlTagEnd(source, i)
		if end == -1 {
			out.WriteString(template.HTMLEscapeString(source[i:]))
			break
		}
		out.WriteString(colorHTMLTag(source[i : end+1]))
		i = end + 1
	}
	return out.String()
}

func colorHTMLTag(tag string) string {
	if strings.HasPrefix(tag, "<!--") {
		return `<span class="html-comment">` + template.HTMLEscapeString(tag) + `</span>`
	}
	if strings.HasPrefix(tag, "<!") || strings.HasPrefix(tag, "<?") {
		return `<span class="html-punctuation">` + template.HTMLEscapeString(tag) + `</span>`
	}

	var out strings.Builder
	i := 0
	for i < len(tag) {
		if tag[i] == '<' || tag[i] == '>' || tag[i] == '/' || tag[i] == '=' {
			out.WriteString(`<span class="html-punctuation">`)
			out.WriteString(template.HTMLEscapeString(tag[i : i+1]))
			out.WriteString(`</span>`)
			i++
			continue
		}
		if tag[i] == '"' || tag[i] == '\'' {
			end := quotedHTMLValueEnd(tag, i)
			out.WriteString(`<span class="html-attr-value">`)
			out.WriteString(template.HTMLEscapeString(tag[i:end]))
			out.WriteString(`</span>`)
			i = end
			continue
		}
		if isHTMLSpace(tag[i]) {
			out.WriteByte(tag[i])
			i++
			continue
		}

		start := i
		for i < len(tag) && !isHTMLSpace(tag[i]) && !strings.ContainsRune(`<>/="'`, rune(tag[i])) {
			i++
		}
		class := "html-attr-name"
		if previousNonSpace(tag, start) == '<' || previousNonSpace(tag, start) == '/' && start > 1 && tag[start-2] == '<' {
			class = "html-tag-name"
		}
		out.WriteString(`<span class="` + class + `">`)
		out.WriteString(template.HTMLEscapeString(tag[start:i]))
		out.WriteString(`</span>`)
	}
	return out.String()
}

func quotedHTMLValueEnd(tag string, start int) int {
	quote := tag[start]
	for i := start + 1; i < len(tag); i++ {
		if tag[i] == quote {
			return i + 1
		}
	}
	return len(tag)
}

func previousNonSpace(value string, before int) byte {
	for i := before - 1; i >= 0; i-- {
		if !isHTMLSpace(value[i]) {
			return value[i]
		}
	}
	return 0
}

func isHTMLSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

type headerRow struct {
	Key   string
	Value string
}

func dateLine(msg mailbox.Message) string {
	if value := strings.TrimSpace(msg.Headers["Date"]); value != "" {
		return value
	}
	return msg.CreatedAt.Format("Mon, 2 Jan 2006, 3:04 pm")
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

func rawMessage(msg mailbox.Message) string {
	if len(msg.Raw) > 0 {
		return string(msg.Raw)
	}

	var buf bytes.Buffer
	writeHeader := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			fmt.Fprintf(&buf, "%s: %s\r\n", key, value)
		}
	}

	writeHeader("From", msg.From)
	writeHeader("To", strings.Join(msg.To, ", "))
	writeHeader("Cc", strings.Join(msg.Cc, ", "))
	writeHeader("Bcc", strings.Join(msg.Bcc, ", "))
	writeHeader("Subject", msg.Subject)
	writeHeader("Date", msg.CreatedAt.Format(time.RFC1123Z))

	keys := make([]string, 0, len(msg.Headers))
	for key := range msg.Headers {
		lower := strings.ToLower(key)
		if lower == "from" || lower == "to" || lower == "cc" || lower == "bcc" || lower == "subject" || lower == "date" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeHeader(key, msg.Headers[key])
	}

	if strings.TrimSpace(msg.HTML) != "" && strings.TrimSpace(msg.Text) != "" {
		boundary := "mirage-local-boundary"
		writeHeader("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
		buf.WriteString("\r\n--" + boundary + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
		buf.WriteString(msg.Text)
		buf.WriteString("\r\n--" + boundary + "\r\nContent-Type: text/html; charset=utf-8\r\n\r\n")
		buf.WriteString(msg.HTML)
		buf.WriteString("\r\n--" + boundary + "--\r\n")
		return buf.String()
	}

	if strings.TrimSpace(msg.HTML) != "" {
		writeHeader("Content-Type", "text/html; charset=utf-8")
		buf.WriteString("\r\n")
		buf.WriteString(msg.HTML)
		return buf.String()
	}

	writeHeader("Content-Type", "text/plain; charset=utf-8")
	buf.WriteString("\r\n")
	buf.WriteString(msg.Text)
	return buf.String()
}
