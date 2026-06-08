package mailbox

import "testing"

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
