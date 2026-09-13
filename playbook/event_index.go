package playbook

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"gorm.io/gorm"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

type eventPlaybookIndex struct {
	mu      sync.RWMutex
	events  map[string][]string
	byEvent map[string][]models.Playbook
}

var eventIndex = newEventPlaybookIndex()

func newEventPlaybookIndex() *eventPlaybookIndex {
	return &eventPlaybookIndex{
		events:  make(map[string][]string),
		byEvent: make(map[string][]models.Playbook),
	}
}

func eventPlaybookCacheKey(eventClass, event string) string {
	return eventClass + "::" + event
}

func eventsOf(p models.Playbook) []string {
	var spec v1.PlaybookSpec
	if err := json.Unmarshal(p.Spec, &spec); err != nil || spec.On == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var keys []string
	add := func(class string, events []v1.PlaybookTriggerEvent) {
		for _, e := range events {
			if e.Event == "" {
				continue
			}
			k := eventPlaybookCacheKey(class, e.Event)
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	add("canary", spec.On.Canary)
	add("config", spec.On.Config)
	add("component", spec.On.Component)
	return keys
}

func (idx *eventPlaybookIndex) removeLocked(id string) {
	for _, key := range idx.events[id] {
		bucket := idx.byEvent[key]
		filtered := bucket[:0]
		for _, existing := range bucket {
			if existing.ID.String() != id {
				filtered = append(filtered, existing)
			}
		}
		if len(filtered) == 0 {
			delete(idx.byEvent, key)
		} else {
			idx.byEvent[key] = filtered
		}
	}
	delete(idx.events, id)
}

func (idx *eventPlaybookIndex) upsertLocked(p models.Playbook) {
	id := p.ID.String()
	idx.removeLocked(id)
	keys := eventsOf(p)
	if len(keys) == 0 {
		return
	}
	idx.events[id] = keys
	for _, key := range keys {
		idx.byEvent[key] = append(idx.byEvent[key], p)
	}
}

func (idx *eventPlaybookIndex) replace(playbooks []models.Playbook) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.events = make(map[string][]string)
	idx.byEvent = make(map[string][]models.Playbook)
	for _, p := range playbooks {
		idx.upsertLocked(p)
	}
}

// LoadEventIndex builds the event subscription index from the database.
func LoadEventIndex(ctx context.Context) error {
	var playbooks []models.Playbook
	if err := ctx.DB().Where("deleted_at IS NULL").Find(&playbooks).Error; err != nil {
		return err
	}
	eventIndex.replace(playbooks)
	return nil
}

// RefreshEventPlaybook updates the event index for a single playbook after table_activity.
func RefreshEventPlaybook(ctx context.Context, id string) error {
	var p models.Playbook
	err := ctx.DB().Where("id = ?", id).First(&p).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	eventIndex.mu.Lock()
	defer eventIndex.mu.Unlock()
	if err != nil || p.DeletedAt != nil {
		eventIndex.removeLocked(id)
		return nil
	}
	eventIndex.upsertLocked(p)
	return nil
}

func FindPlaybooksForEvent(eventClass, event string) []models.Playbook {
	key := eventPlaybookCacheKey(eventClass, event)
	eventIndex.mu.RLock()
	defer eventIndex.mu.RUnlock()
	src := eventIndex.byEvent[key]
	out := make([]models.Playbook, len(src))
	copy(out, src)
	return out
}
