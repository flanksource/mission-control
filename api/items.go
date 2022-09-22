package api

import (
	"strings"

	"gorm.io/gorm"
)

type Items []string

func (items Items) String() string {
	return strings.Join(items, ",")
}

// contains returns true if any of the items in the list match the item
// negative matches are supported by prefixing the item with a !
// * matches everything
func (items Items) Contains(item string) bool {
	if len(items) == 0 {
		return true
	}

	negations := 0
	for _, i := range items {
		if strings.HasPrefix(i, "!") {
			negations++
			if item == strings.TrimPrefix(i, "!") {
				return false
			}
		}
	}

	if negations == len(items) {
		// none of the negations matched
		return true
	}

	for _, i := range items {
		if strings.HasPrefix(i, "!") {
			continue
		}
		if i == "*" || item == i {
			return true
		}
	}
	return false
}

func (items Items) WithNegation() []string {
	var result []string
	for _, item := range items {
		if strings.HasPrefix(item, "!") {
			result = append(result, item[1:])
		}
	}
	return result
}

func (items Items) WithoutNegation() []string {
	var result []string
	for _, item := range items {
		if !strings.HasPrefix(item, "!") {
			result = append(result, item)
		}
	}
	return result
}

func (items Items) Where(query *gorm.DB, col string) *gorm.DB {
	if items == nil {
		return query
	}

	negated := items.WithNegation()
	if len(negated) > 0 {
		query = query.Where("NOT "+col+" IN ?", negated)
	}

	positive := items.WithoutNegation()
	if len(positive) > 0 {
		query = query.Where(col+" IN ?", positive)
	}

	return query
}
