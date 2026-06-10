package mailbox

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"time"
)

type Message struct {
	ID          string
	Provider    string
	Domain      string
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	Text        string
	HTML        string
	Headers     map[string]string
	Variables   map[string]string
	Options     map[string]string
	Attachments []Attachment
	CreatedAt   time.Time
	Viewed      bool
	Raw         []byte
}

type Attachment struct {
	Name        string
	ContentType string
	Size        int64
	ContentID   string
	Inline      bool
	Data        []byte
}

type Event struct {
	Revision  uint64 `json:"revision"`
	Type      string `json:"type"`
	MessageID string `json:"messageId,omitempty"`
}

type Store struct {
	mu          sync.RWMutex
	nextID      uint64
	revision    uint64
	messages    []Message
	subscribers map[chan Event]struct{}
}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) Add(msg Message) Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	now := time.Now().UTC()
	msg.ID = now.Format("20060102T150405") + "-" + strconv.FormatUint(s.nextID, 10)
	msg.CreatedAt = now
	if msg.Headers == nil {
		msg.Headers = map[string]string{}
	}
	if msg.Variables == nil {
		msg.Variables = map[string]string{}
	}
	if msg.Options == nil {
		msg.Options = map[string]string{}
	}

	s.messages = append(s.messages, msg)
	s.publishLocked("message-added", msg.ID)
	return msg
}

func (s *Store) List() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := slices.Clone(s.messages)
	slices.Reverse(out)
	return out
}

func (s *Store) Get(id string) (Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, msg := range s.messages {
		if msg.ID == id {
			return msg, true
		}
	}
	return Message{}, false
}

func (s *Store) MarkViewed(id string) (Message, bool) {
	return s.SetViewed(id, true)
}

func (s *Store) SetViewed(id string, viewed bool) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, msg := range s.messages {
		if msg.ID == id {
			if s.messages[i].Viewed == viewed {
				return s.messages[i], true
			}
			s.messages[i].Viewed = viewed
			s.publishLocked("message-updated", id)
			return s.messages[i], true
		}
	}
	return Message{}, false
}

func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, msg := range s.messages {
		if msg.ID == id {
			s.messages = slices.Delete(s.messages, i, i+1)
			s.publishLocked("message-deleted", id)
			return true
		}
	}
	return false
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.messages) == 0 {
		return
	}
	s.messages = nil
	s.publishLocked("inbox-cleared", "")
}

func (s *Store) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, 8)

	s.mu.Lock()
	if s.subscribers == nil {
		s.subscribers = map[chan Event]struct{}{}
	}
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
		s.mu.Unlock()
	}()

	return ch
}

func (s *Store) publishLocked(eventType, messageID string) {
	s.revision++
	event := Event{
		Revision:  s.revision,
		Type:      eventType,
		MessageID: messageID,
	}
	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}
