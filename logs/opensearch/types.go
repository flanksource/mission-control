package opensearch

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/types"
)

// +kubebuilder:object:generate=true
type Backend struct {
	Address  string        `json:"address"`
	Username *types.EnvVar `json:"username,omitempty"`
	Password *types.EnvVar `json:"password,omitempty"`
}

// +kubebuilder:object:generate=true
type Request struct {
	Index string `json:"index" template:"true"`
	Query string `json:"query" template:"true"`
	Limit string `json:"limit,omitempty" template:"true"`
}

type TotalHitsInfo struct {
	Value    int64  `json:"value"`
	Relation string `json:"relation"`
}

type HitsInfo struct {
	Total    TotalHitsInfo `json:"total"`
	MaxScore float64       `json:"max_score"`
	Hits     []SearchHit   `json:"hits"`
}

// NextPage returns the next page token.
func (t *HitsInfo) NextPage(requestedRowsCount int) string {
	if len(t.Hits) == 0 {
		return ""
	}

	// If we got less than the requested rows count, we are at the end of the results.
	// Note: We always request one more than the requested rows count, so we can
	// determine if there are more results to fetch.
	if requestedRowsCount >= len(t.Hits) {
		return ""
	}

	lastItem := t.Hits[len(t.Hits)-2]
	val, err := utils.Stringify(lastItem.Sort)
	if err != nil {
		logger.Errorf("error stringifying sort: %v", err)
		return ""
	}

	return val
}

type SearchResponse struct {
	Took     float64  `json:"took"`
	TimedOut bool     `json:"timed_out"`
	Hits     HitsInfo `json:"hits"`
}

type SearchHit struct {
	Index  string         `json:"_index"`
	Type   string         `json:"_type"`
	ID     string         `json:"_id"`
	Score  float64        `json:"_score"`
	Sort   []any          `json:"sort"`
	Source map[string]any `json:"_source"`
}

type SearchResults struct {
	Total    int64            `json:"total,omitempty"`
	Results  []map[string]any `json:"results,omitempty"`
	NextPage string           `json:"nextPage,omitempty"`
}

type Result struct {
	// Id is the unique identifier provided by the underlying system, use to link to a point in time of a log stream
	Id string `json:"id,omitempty"`
	// RFC3339 timestamp
	Time    string            `json:"timestamp,omitempty"`
	Message string            `json:"message,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}
