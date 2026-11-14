package notifications

import (
	"encoding/json"
	"sync"

	"ispilolite/internal/models"
)

type Hub struct {
	mu               sync.RWMutex
	subscribers      map[chan []byte]string
	topicSubscribers map[chan []byte]string
}

var Default = NewHub()

func NewHub() *Hub {
	return &Hub{
		subscribers:      make(map[chan []byte]string),
		topicSubscribers: make(map[chan []byte]string),
	}
}

func (h *Hub) Subscribe(userID string) (<-chan []byte, func()) {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.subscribers[ch] = userID
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subscribers[ch]; ok {
			delete(h.subscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// SubscribeTopic subscribes to an already-authorized domain topic, such as
// "job:<uuid>". Authorization belongs in the HTTP/service layer before this
// method is called; the hub only handles fan-out.
func (h *Hub) SubscribeTopic(topic string) (<-chan []byte, func()) {
	ch := make(chan []byte, 32)
	h.mu.Lock()
	h.topicSubscribers[ch] = topic
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.topicSubscribers[ch]; ok {
			delete(h.topicSubscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(notification *models.Notification) {
	payload, err := json.Marshal(notification)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch, userID := range h.subscribers {
		if userID != notification.UserID {
			continue
		}
		select {
		case ch <- payload:
		default:
		}
	}
}

// PublishTopic emits a typed SSE-compatible envelope to every subscriber on a
// topic. Slow subscribers drop an event rather than blocking business writes;
// clients reconnect and use the persisted event-history endpoint to catch up.
func (h *Hub) PublishTopic(topic, event string, data interface{}) {
	payload, err := json.Marshal(map[string]interface{}{
		"event": event,
		"data":  data,
	})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch, subscribedTopic := range h.topicSubscribers {
		if subscribedTopic != topic {
			continue
		}
		select {
		case ch <- payload:
		default:
		}
	}
}
