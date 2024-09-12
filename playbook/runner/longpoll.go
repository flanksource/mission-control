package runner

import (
	"encoding/json"
	"sync"
	"time"
)

// Global instance
var ActionMgr = NewActionNotifyManager(make(chan string))

var LongpollTimeout = time.Minute * 2 // time.Second * 5

type actionNotifyManager struct {
	ch            chan string
	mu            *sync.RWMutex
	subscriptions map[string]chan string
}

func NewActionNotifyManager(ch chan string) *actionNotifyManager {
	return &actionNotifyManager{
		ch:            ch,
		mu:            &sync.RWMutex{},
		subscriptions: make(map[string]chan string),
	}
}

type playbookActionNotifyPayload struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
}

func (d *actionNotifyManager) Chan() chan<- string {
	return d.ch
}

func (d *actionNotifyManager) Listen() {
	for payload := range d.ch {
		var action playbookActionNotifyPayload
		if err := json.Unmarshal([]byte(payload), &action); err != nil {
			continue
		}

		d.mu.RLock()
		if e, ok := d.subscriptions[action.AgentID]; ok {
			e <- action.ID
		}
		d.mu.RUnlock()
	}
}

func (d *actionNotifyManager) Register(agentID string) chan string {
	d.mu.Lock()
	defer d.mu.Unlock()

	if e, ok := d.subscriptions[agentID]; ok {
		return e
	}

	ch := make(chan string)
	d.subscriptions[agentID] = ch

	return ch
}
