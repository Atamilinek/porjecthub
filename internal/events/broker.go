package events

import (
	"sync"
	"time"
)

type Event struct {
	ID        uint64    `json:"id"`
	Type      string    `json:"type"`
	Data      []byte    `json:"data"`
	CreatedAt time.Time `json:"createdAt"`
}
type subscriber struct {
	events chan Event
	closed bool
}
type Broker struct {
	mu             sync.Mutex
	historyLimit   int
	queueLimit     int
	nextID         uint64
	history        []Event
	subscribers    map[uint64]*subscriber
	nextSubscriber uint64
}
type Subscription struct {
	Events <-chan Event
	broker *Broker
	id     uint64
	once   sync.Once
}

func NewBroker(historyLimit, queueLimit int) *Broker {
	if historyLimit < 1 {
		historyLimit = 128
	}
	if queueLimit < 1 {
		queueLimit = 32
	}
	return &Broker{historyLimit: historyLimit, queueLimit: queueLimit, subscribers: make(map[uint64]*subscriber)}
}
func (b *Broker) Publish(eventType string, data []byte) Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	event := Event{ID: b.nextID, Type: eventType, Data: append([]byte(nil), data...), CreatedAt: time.Now().UTC()}
	b.history = append(b.history, event)
	if len(b.history) > b.historyLimit {
		b.history = append([]Event(nil), b.history[len(b.history)-b.historyLimit:]...)
	}
	for id, sub := range b.subscribers {
		copyEvent := event
		copyEvent.Data = append([]byte(nil), event.Data...)
		select {
		case sub.events <- copyEvent:
		default:
			b.closeLocked(id, sub)
		}
	}
	returned := event
	returned.Data = append([]byte(nil), event.Data...)
	return returned
}
func (b *Broker) Subscribe(lastEventID uint64) ([]Event, *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	replay := make([]Event, 0)
	for _, event := range b.history {
		if event.ID > lastEventID {
			copyEvent := event
			copyEvent.Data = append([]byte(nil), event.Data...)
			replay = append(replay, copyEvent)
		}
	}
	b.nextSubscriber++
	id := b.nextSubscriber
	sub := &subscriber{events: make(chan Event, b.queueLimit)}
	b.subscribers[id] = sub
	return replay, &Subscription{Events: sub.events, broker: b, id: id}
}
func (s *Subscription) Close() {
	if s == nil || s.broker == nil {
		return
	}
	s.once.Do(func() {
		s.broker.mu.Lock()
		defer s.broker.mu.Unlock()
		if sub := s.broker.subscribers[s.id]; sub != nil {
			s.broker.closeLocked(s.id, sub)
		}
	})
}
func (b *Broker) closeLocked(id uint64, sub *subscriber) {
	if sub.closed {
		return
	}
	sub.closed = true
	delete(b.subscribers, id)
	close(sub.events)
}
