package events

import (
	"sync"

	"github.com/flanksource/postq"
)

type EventRing struct {
	size   int
	events map[string][]map[string]any
	mu     *sync.RWMutex
}

func NewEventRing(size int) *EventRing {
	return &EventRing{
		size:   size,
		events: make(map[string][]map[string]any),
		mu:     &sync.RWMutex{},
	}
}

func (t *EventRing) Add(event postq.Event, env map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.events[event.Name]; !ok {
		t.events[event.Name] = make([]map[string]any, 0)
	}

	t.events[event.Name] = append(t.events[event.Name], map[string]any{
		"event": event,
		"env":   env,
	})

	if len(t.events[event.Name]) > t.size {
		t.events[event.Name] = t.events[event.Name][1:]
	}
}

func (t *EventRing) Get() map[string][]map[string]any {
	return t.events
}
