package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// Broker is a simple SSE event fan-out. Subscribers receive a signal on their
// channel whenever Publish is called. Slow consumers are skipped (non-blocking
// send) so one stuck client cannot block updates to others.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]chan struct{}
}

// NewBroker creates a ready-to-use Broker.
func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[string]chan struct{}),
	}
}

// Subscribe registers a new subscriber and returns a channel that receives
// notifications and a unique subscriber ID for later unsubscription.
func (b *Broker) Subscribe() (string, chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := randomID()
	ch := make(chan struct{}, 1)
	b.subscribers[id] = ch
	return id, ch
}

// Unsubscribe removes a subscriber by ID and closes its channel.
func (b *Broker) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.subscribers[id]; ok {
		close(ch)
		delete(b.subscribers, id)
	}
}

// Publish sends a non-blocking signal to every subscriber. If a subscriber's
// channel buffer is full (slow consumer), the send is skipped for that
// subscriber rather than blocking.
func (b *Broker) Publish() {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// randomID generates a short random hex string for subscriber identification.
func randomID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(buf)
}
