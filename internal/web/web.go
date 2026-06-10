package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"mirage/internal/mailbox"
)

//go:embed assets/templates/*.html assets/static/*
var assetsFS embed.FS

type Store interface {
	List() []mailbox.Message
	Get(string) (mailbox.Message, bool)
	MarkViewed(string) (mailbox.Message, bool)
	SetViewed(string, bool) (mailbox.Message, bool)
	Delete(string) bool
	Clear()
}

type app struct {
	store Store
	index *template.Template
	show  *template.Template
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
	mux.HandleFunc("GET /messages/{id}/attachments/{index}", app.attachmentHandler)
	mux.HandleFunc("POST /messages/{id}/unsubscribe", app.unsubscribeHandler)
	mux.HandleFunc("POST /messages/{id}/delete", app.deleteHandler)
	mux.HandleFunc("POST /messages/clear", app.clearHandler)
	mux.HandleFunc("GET /api/v1/inbox", app.apiV1InboxHandler)
	mux.HandleFunc("DELETE /api/v1/inbox", app.apiV1InboxClearHandler)
	mux.HandleFunc("GET /api/v1/message/{id}", app.apiV1MessageHandler)
	mux.HandleFunc("PATCH /api/v1/message/{id}", app.apiV1MessageUpdateHandler)
	mux.HandleFunc("DELETE /api/v1/message/{id}", app.apiV1MessageDeleteHandler)
	mux.HandleFunc("GET /api/v1/message/{id}/body/{part}", app.apiV1MessageBodyHandler)
	mux.HandleFunc("GET /api/v1/message/{id}/attachment/{index}", app.apiV1MessageAttachmentHandler)
	mux.HandleFunc("GET /api/", apiNotFoundHandler)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(staticFS))))
}
