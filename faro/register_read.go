package main

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

func readRegister(path string) (yaml.MapSlice, string, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var document yaml.MapSlice
	if err := yaml.UnmarshalWithOptions(source, &document, yaml.UseOrderedMap()); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	return document, "", nil
}

func mapGet(mapping yaml.MapSlice, key string) (any, bool) {
	for _, item := range mapping {
		if item.Key == key {
			return item.Value, true
		}
	}
	return nil, false
}

func mapString(mapping yaml.MapSlice, key string) string {
	value, ok := mapGet(mapping, key)
	if !ok || value == nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

func mapSlice(mapping yaml.MapSlice, key string) []any {
	value, ok := mapGet(mapping, key)
	if !ok || value == nil {
		return nil
	}
	switch items := value.(type) {
	case []any:
		return items
	case []string:
		values := make([]any, 0, len(items))
		for _, item := range items {
			values = append(values, item)
		}
		return values
	default:
		return nil
	}
}

func mapStrings(mapping yaml.MapSlice, key string) []string {
	values := make([]string, 0)
	for _, item := range mapSlice(mapping, key) {
		if value, ok := item.(string); ok {
			values = append(values, value)
		}
	}
	return values
}

func registerEntries(document yaml.MapSlice) ([]yaml.MapSlice, error) {
	items := mapSlice(document, "entries")
	entries := make([]yaml.MapSlice, 0, len(items))
	for index, item := range items {
		entry, ok := item.(yaml.MapSlice)
		if !ok {
			return nil, fmt.Errorf("entries[%d] is not a mapping", index)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

type registerContextSummary struct {
	id           string
	grants       int
	configs      int
	roles        int
	handAuthored int
}

func summariseRegister(entries []yaml.MapSlice, contextName string) (map[string]*registerContextSummary, []*registerContextSummary) {
	byIdentifier := map[string]*registerContextSummary{}
	all := make([]*registerContextSummary, 0)
	for _, entry := range entries {
		configs := map[string]struct{}{}
		roles := map[string]struct{}{}
		summary := &registerContextSummary{id: mapString(entry, "id")}
		for _, value := range mapSlice(entry, "config_access") {
			grant, ok := value.(yaml.MapSlice)
			if !ok {
				continue
			}
			configID, _ := mapGet(grant, "config_id")
			if configID == nil {
				summary.handAuthored++
				continue
			}
			if mapString(grant, "context") != contextName {
				continue
			}
			summary.grants++
			configs[mapString(grant, "config_id")] = struct{}{}
			roles[mapString(grant, "role")] = struct{}{}
		}
		if summary.grants == 0 {
			continue
		}
		summary.configs = len(configs)
		summary.roles = len(roles)
		all = append(all, summary)
		identifiers := append(mapStrings(entry, "aliases"), mapString(entry, "external_user_id"))
		for _, identifier := range identifiers {
			if identifier != "" {
				byIdentifier[identifier] = summary
			}
		}
	}
	return byIdentifier, all
}
