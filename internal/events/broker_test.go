package events

import (
	"testing"
	"time"
)

func TestBrokerAssignsIDsReplaysEventsAndClosesSlowSubscribers(t *testing.T) {
	broker := NewBroker(2, 1)
	first := broker.Publish("health", []byte(`{"state":"healthy"}`))
	second := broker.Publish("health", []byte(`{"state":"offline"}`))
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("event IDs %d,%d want 1,2", first.ID, second.ID)
	}
	replay, sub := broker.Subscribe(1)
	if len(replay) != 1 || replay[0].ID != 2 {
		t.Fatalf("replay after ID 1 = %#v, want event 2", replay)
	}
	broker.Publish("health", []byte("three"))
	broker.Publish("health", []byte("four"))
	closed := false
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	for !closed {
		select {
		case _, ok := <-sub.Events:
			closed = !ok
		case <-timer.C:
			t.Fatal("overflow did not close slow subscriber")
		}
	}
	if !closed {
		t.Fatal("overflow did not close slow subscriber")
	}
	sub.Close()
}

func TestBrokerKeepsBoundedReplayWindow(t *testing.T) {
	broker := NewBroker(2, 2)
	for i := 0; i < 4; i++ {
		broker.Publish("state", []byte("x"))
	}
	replay, sub := broker.Subscribe(0)
	defer sub.Close()
	if len(replay) != 2 || replay[0].ID != 3 || replay[1].ID != 4 {
		t.Fatalf("bounded replay=%+v", replay)
	}
}

func TestPublishedEventDataIsCopiedFromCaller(t *testing.T) {
	broker := NewBroker(2, 2)
	published := broker.Publish("state", []byte("original"))
	published.Data[0] = 'X'
	replay, sub := broker.Subscribe(0)
	defer sub.Close()
	if string(replay[0].Data) != "original" {
		t.Fatalf("replay was mutated by caller: %q", replay[0].Data)
	}
}
