package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"mirage/internal/mailbox"
)

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
		_, _ = w.Write([]byte(resolveCIDURLs(msg)))
		return
	}
	escaped := template.HTMLEscapeString(msg.Text)
	_, _ = w.Write([]byte("<!doctype html><meta charset=\"utf-8\"><pre style=\"white-space:pre-wrap;font:14px/1.5 system-ui,sans-serif\">" + escaped + "</pre>"))
}

func (a *app) attachmentHandler(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.store.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	attachment, _, ok := attachmentByIndex(msg, r.PathValue("index"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeAttachment(w, attachment)
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
