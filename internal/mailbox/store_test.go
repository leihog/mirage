package mailbox

import (
	"context"
	"testing"
	"time"
)

func TestStoreDeleteRemovesOnlyMatchingMessage(t *testing.T) {
	store := NewStore()
	first := store.Add(Message{Subject: "first"})
	second := store.Add(Message{Subject: "second"})

	if !store.Delete(first.ID) {
		t.Fatal("expected delete to report a removed message")
	}
	if store.Delete("missing") {
		t.Fatal("missing delete should report false")
	}

	messages := store.List()
	if len(messages) != 1 {
		t.Fatalf("expected one remaining message, got %d", len(messages))
	}
	if messages[0].ID != second.ID {
		t.Fatalf("expected second message to remain, got %#v", messages[0])
	}
}

func TestStoreAddUsesUTCCreatedAt(t *testing.T) {
	store := NewStore()
	msg := store.Add(Message{Subject: "timestamp"})

	if msg.CreatedAt.Location() != time.UTC {
		t.Fatalf("expected UTC location, got %s", msg.CreatedAt.Location())
	}
	if got := store.List()[0].CreatedAt.Location(); got != time.UTC {
		t.Fatalf("expected stored UTC location, got %s", got)
	}
}

func TestStoreMarkViewedUpdatesMessage(t *testing.T) {
	store := NewStore()
	msg := store.Add(Message{Subject: "unread"})

	viewed, ok := store.MarkViewed(msg.ID)
	if !ok {
		t.Fatal("expected mark viewed to find the message")
	}
	if !viewed.Viewed {
		t.Fatal("expected returned message to be viewed")
	}
	if store.List()[0].Viewed != true {
		t.Fatal("expected stored message to be viewed")
	}
	if _, ok := store.MarkViewed("missing"); ok {
		t.Fatal("missing mark viewed should report false")
	}
}

func TestStoreSetViewedUpdatesMessage(t *testing.T) {
	store := NewStore()
	msg := store.Add(Message{Subject: "read state"})

	viewed, ok := store.SetViewed(msg.ID, true)
	if !ok {
		t.Fatal("expected set viewed to find the message")
	}
	if !viewed.Viewed || !store.List()[0].Viewed {
		t.Fatal("expected message to be viewed")
	}

	unread, ok := store.SetViewed(msg.ID, false)
	if !ok {
		t.Fatal("expected set unread to find the message")
	}
	if unread.Viewed || store.List()[0].Viewed {
		t.Fatal("expected message to be unread")
	}
	if _, ok := store.SetViewed("missing", true); ok {
		t.Fatal("missing set viewed should report false")
	}
}

func TestStorePublishesMutationEvents(t *testing.T) {
	store := NewStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := store.Subscribe(ctx)

	msg := store.Add(Message{Subject: "evented"})
	event := nextEvent(t, events)
	if event.Type != "message-added" || event.MessageID != msg.ID || event.Revision != 1 {
		t.Fatalf("unexpected add event: %#v", event)
	}

	if _, ok := store.SetViewed(msg.ID, true); !ok {
		t.Fatal("expected set viewed to find message")
	}
	event = nextEvent(t, events)
	if event.Type != "message-updated" || event.MessageID != msg.ID || event.Revision != 2 {
		t.Fatalf("unexpected update event: %#v", event)
	}

	if !store.Delete(msg.ID) {
		t.Fatal("expected delete to find message")
	}
	event = nextEvent(t, events)
	if event.Type != "message-deleted" || event.MessageID != msg.ID || event.Revision != 3 {
		t.Fatalf("unexpected delete event: %#v", event)
	}
}

func nextEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for store event")
		return Event{}
	}
}
