package web

import "time"

type inboxResponse struct {
	Messages      []messageSummary `json:"messages"`
	Total         int              `json:"total"`
	FilteredTotal int              `json:"filteredTotal"`
	UnreadTotal   int              `json:"unreadTotal"`
	Limit         int              `json:"limit"`
	Offset        int              `json:"offset"`
	HasMore       bool             `json:"hasMore"`
}

type messageResponse struct {
	Message messageDetail `json:"message"`
}

type messageSummary struct {
	ID              string              `json:"id"`
	Subject         string              `json:"subject"`
	From            string              `json:"from"`
	To              []string            `json:"to"`
	Cc              []string            `json:"cc"`
	Bcc             []string            `json:"bcc"`
	Provider        string              `json:"provider"`
	Domain          string              `json:"domain"`
	CreatedAt       time.Time           `json:"createdAt"`
	Viewed          bool                `json:"viewed"`
	HasText         bool                `json:"hasText"`
	HasHTML         bool                `json:"hasHTML"`
	AttachmentCount int                 `json:"attachmentCount"`
	Headers         map[string][]string `json:"headers,omitempty"`
}

type messageDetail struct {
	ID          string                 `json:"id"`
	Subject     string                 `json:"subject"`
	From        string                 `json:"from"`
	To          []string               `json:"to"`
	Cc          []string               `json:"cc"`
	Bcc         []string               `json:"bcc"`
	Provider    string                 `json:"provider"`
	Domain      string                 `json:"domain"`
	CreatedAt   time.Time              `json:"createdAt"`
	Viewed      bool                   `json:"viewed"`
	Headers     map[string][]string    `json:"headers"`
	Variables   map[string][]string    `json:"variables"`
	Options     map[string][]string    `json:"options"`
	Attachments []attachmentResponse   `json:"attachments"`
	Bodies      map[string]bodySummary `json:"bodies"`
	Unsubscribe *unsubscribeSummary    `json:"unsubscribe,omitempty"`
}

type attachmentResponse struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	ContentID   string `json:"contentId,omitempty"`
	Inline      bool   `json:"inline"`
	URL         string `json:"url,omitempty"`
}

type attachmentBodyResponse struct {
	ID          string `json:"id"`
	Index       int    `json:"index"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	ContentID   string `json:"contentId,omitempty"`
	Inline      bool   `json:"inline"`
	BodyBase64  string `json:"bodyBase64"`
}

type bodySummary struct {
	Available   bool   `json:"available"`
	Size        int    `json:"size"`
	ContentType string `json:"contentType,omitempty"`
	URL         string `json:"url,omitempty"`
}

type bodyResponse struct {
	ID          string `json:"id"`
	Part        string `json:"part"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Body        string `json:"body"`
}

type unsubscribeSummary struct {
	OneClick bool   `json:"oneClick"`
	URL      string `json:"url,omitempty"`
}
