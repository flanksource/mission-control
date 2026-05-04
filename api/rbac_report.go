package api

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
)

type RBACReport struct {
	Title       string              `json:"title"`
	Query       string              `json:"query,omitempty"`
	GeneratedAt time.Time           `json:"generatedAt"`
	Subject     *models.ConfigItem  `json:"subject,omitempty"`
	Parents     []models.ConfigItem `json:"parents,omitempty"`
	Resources   []RBACResource      `json:"resources"`
	Changelog   []RBACChangeEntry   `json:"changelog"`
	Summary     RBACSummary         `json:"summary"`
	Users       []RBACUserReport    `json:"users,omitempty"`
}

type RBACResource struct {
	ConfigID        string                `json:"configId"`
	ConfigName      string                `json:"configName"`
	ConfigType      string                `json:"configType"`
	ConfigClass     string                `json:"configClass,omitempty"`
	ParentID        string                `json:"parentId,omitempty"`
	Path            string                `json:"path,omitempty"`
	Status          string                `json:"status,omitempty"`
	Health          string                `json:"health,omitempty"`
	Description     string                `json:"description,omitempty"`
	Tags            types.JSONStringMap   `json:"tags,omitempty"`
	Labels          types.JSONStringMap   `json:"labels,omitempty"`
	CreatedAt       *time.Time            `json:"createdAt,omitempty"`
	UpdatedAt       *time.Time            `json:"updatedAt,omitempty"`
	Users           []RBACUserRole        `json:"users"`
	Changelog       []RBACChangeEntry     `json:"changelog"`
	TemporaryAccess []RBACTemporaryAccess `json:"temporaryAccess,omitempty"`
}

type RBACTemporaryAccess struct {
	ConfigID  string    `json:"configId"`
	User      string    `json:"user"`
	Role      string    `json:"role"`
	Source    string    `json:"source"`
	GrantedAt time.Time `json:"grantedAt"`
	RevokedAt time.Time `json:"revokedAt"`
	Duration  string    `json:"duration"`
}

type RBACUserRole struct {
	UserID          string     `json:"userId"`
	UserName        string     `json:"userName"`
	Email           string     `json:"email"`
	Role            string     `json:"role"`
	RoleExternalIDs []string   `json:"roleExternalIds,omitempty"`
	RoleSource      string     `json:"roleSource"`
	SourceSystem    string     `json:"sourceSystem"`
	CreatedAt       time.Time  `json:"createdAt"`
	LastSignedInAt  *time.Time `json:"lastSignedInAt,omitempty"`
	LastReviewedAt  *time.Time `json:"lastReviewedAt,omitempty"`
	IsStale         bool       `json:"isStale"`
	IsReviewOverdue bool       `json:"isReviewOverdue"`
}

type RBACChangeEntry struct {
	ConfigID    string    `json:"configId"`
	Date        time.Time `json:"date"`
	ChangeType  string    `json:"changeType"`
	User        string    `json:"user"`
	Role        string    `json:"role"`
	ConfigName  string    `json:"configName"`
	Source      string    `json:"source"`
	Description string    `json:"description"`
}

type RBACUserResource struct {
	ConfigID        string              `json:"configId"`
	ConfigName      string              `json:"configName"`
	ConfigType      string              `json:"configType"`
	ConfigClass     string              `json:"configClass,omitempty"`
	Path            string              `json:"path,omitempty"`
	Role            string              `json:"role"`
	RoleExternalIDs []string            `json:"roleExternalIds,omitempty"`
	RoleSource      string              `json:"roleSource"`
	CreatedAt       time.Time           `json:"createdAt"`
	LastSignedInAt  *time.Time          `json:"lastSignedInAt,omitempty"`
	LastReviewedAt  *time.Time          `json:"lastReviewedAt,omitempty"`
	IsStale         bool                `json:"isStale"`
	IsReviewOverdue bool                `json:"isReviewOverdue"`
	Status          string              `json:"status,omitempty"`
	Health          string              `json:"health,omitempty"`
	Tags            types.JSONStringMap `json:"tags,omitempty"`
	Labels          types.JSONStringMap `json:"labels,omitempty"`
}

type RBACUserReport struct {
	UserID         string             `json:"userId"`
	UserName       string             `json:"userName"`
	Email          string             `json:"email"`
	SourceSystem   string             `json:"sourceSystem"`
	LastSignedInAt *time.Time         `json:"lastSignedInAt,omitempty"`
	Resources      []RBACUserResource `json:"resources"`
}

type RBACSummary struct {
	TotalUsers        int `json:"totalUsers"`
	TotalResources    int `json:"totalResources"`
	StaleAccessCount  int `json:"staleAccessCount"`
	OverdueReviews    int `json:"overdueReviews"`
	DirectAssignments int `json:"directAssignments"`
	GroupAssignments  int `json:"groupAssignments"`
}
