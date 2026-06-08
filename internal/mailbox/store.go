package mailbox

import (
	"slices"
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
}

type Store struct {
	mu       sync.RWMutex
	nextID   uint64
	messages []Message
}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) Add(msg Message) Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	msg.ID = time.Now().UTC().Format("20060102T150405") + "-" + itoa(s.nextID)
	msg.CreatedAt = time.Now()
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
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, msg := range s.messages {
		if msg.ID == id {
			s.messages[i].Viewed = true
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
			return true
		}
	}
	return false
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = nil
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
