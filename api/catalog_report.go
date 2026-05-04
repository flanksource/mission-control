package api

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/duty/models"
)

func ConfigPermalink(configID string) string {
	if FrontendURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/catalog/%s", FrontendURL, configID)
}

type CatalogReportThresholds struct {
	StaleDays         int `json:"staleDays"`
	ReviewOverdueDays int `json:"reviewOverdueDays"`
}

type CatalogReportCategoryMapping struct {
	Category  string `json:"category,omitempty"`
	Filter    string `json:"filter"`
	Transform string `json:"transform,omitempty"`
}

type CatalogReportOptions struct {
	Title            string                         `json:"title"`
	Since            string                         `json:"since"`
	Sections         CatalogReportSections          `json:"sections"`
	Recursive        bool                           `json:"recursive"`
	GroupBy          string                         `json:"groupBy"`
	ChangeArtifacts  bool                           `json:"changeArtifacts"`
	Filters          []string                       `json:"filters,omitempty"`
	Thresholds       *CatalogReportThresholds       `json:"thresholds,omitempty"`
	CategoryMappings []CatalogReportCategoryMapping `json:"categoryMappings,omitempty"`
}

type CatalogReportAudit struct {
	BuildCommit  string               `json:"buildCommit"`
	BuildVersion string               `json:"buildVersion"`
	GitStatus    string               `json:"gitStatus,omitempty"`
	Options      CatalogReportOptions `json:"options"`
	Scrapers     []ScraperInfo        `json:"scrapers"`
	Queries      []CatalogReportQuery `json:"queries"`
	Groups       []CatalogReportGroup `json:"groups"`
}

type CatalogReportGroup struct {
	ID        string                     `json:"id"`
	Name      string                     `json:"name"`
	GroupType string                     `json:"groupType,omitempty"`
	Members   []CatalogReportGroupMember `json:"members"`
}

type CatalogReportGroupMember struct {
	UserID              string  `json:"userId"`
	Name                string  `json:"name"`
	Email               string  `json:"email,omitempty"`
	UserType            string  `json:"userType,omitempty"`
	LastSignedInAt      *string `json:"lastSignedInAt,omitempty"`
	MembershipAddedAt   string  `json:"membershipAddedAt"`
	MembershipDeletedAt *string `json:"membershipDeletedAt,omitempty"`
}

type CatalogReportQuery struct {
	Name     string `json:"name"`
	Args     string `json:"args,omitempty"`
	Count    int    `json:"count"`
	Duration int64  `json:"duration"`
	Error    string `json:"error,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Pretty   string `json:"pretty"`
}

type CatalogReport struct {
	Title       string                `json:"title"`
	GeneratedAt time.Time             `json:"generatedAt"`
	PublicURL   string                `json:"publicURL,omitempty"`
	From        string                `json:"from,omitempty"`
	To          string                `json:"to,omitempty"`
	Sections    CatalogReportSections `json:"sections"`
	Recursive   bool                  `json:"recursive,omitempty"`
	GroupBy     string                `json:"groupBy,omitempty"`
	Entries     []CatalogReportEntry  `json:"entries"`

	CategoryMappings []CatalogReportCategoryMapping `json:"categoryMappings,omitempty"`
	Thresholds       *CatalogReportThresholds       `json:"thresholds,omitempty"`
	Audit            *CatalogReportAudit            `json:"audit,omitempty"`

	// Deprecated: use Entries[0] for single-config reports
	ConfigItem models.ConfigItem   `json:"configItem"`
	Parents    []models.ConfigItem `json:"parents"`

	Changes          []CatalogReportChange       `json:"changes,omitempty"`
	Analyses         []CatalogReportAnalysis     `json:"analyses,omitempty"`
	Relationships    []CatalogReportRelationship `json:"relationships,omitempty"`
	RelatedConfigs   []CatalogReportConfigItem   `json:"relatedConfigs,omitempty"`
	RelationshipTree *CatalogReportTreeNode      `json:"relationshipTree,omitempty"`
	Access           []CatalogReportAccess       `json:"access,omitempty"`
	AccessLogs       []CatalogReportAccessLog    `json:"accessLogs,omitempty"`
	ConfigJSON       *string                     `json:"configJSON,omitempty"`
	ConfigGroups     []CatalogReportConfigGroup  `json:"configGroups,omitempty"`
}

type CatalogReportEntry struct {
	ConfigItem       CatalogReportConfigItem   `json:"configItem"`
	Parents          []CatalogReportConfigItem `json:"parents,omitempty"`
	RelationshipTree *CatalogReportTreeNode    `json:"relationshipTree,omitempty"`
	ChangeCount      int                       `json:"changeCount"`
	InsightCount     int                       `json:"insightCount"`
	AccessCount      int                       `json:"accessCount"`
	RBACResources    []RBACResource            `json:"rbacResources,omitempty"`
	Changes          []CatalogReportChange     `json:"changes,omitempty"`
	Analyses         []CatalogReportAnalysis   `json:"analyses,omitempty"`
	Access           []CatalogReportAccess     `json:"access,omitempty"`
	AccessLogs       []CatalogReportAccessLog  `json:"accessLogs,omitempty"`

	// IsRoot marks entries whose ancestors are not in the selected set. Used by
	// groupBy=none to render roots first and children under a breadcrumb.
	IsRoot bool `json:"isRoot,omitempty"`
	// Breadcrumb is the chain of selected ancestors from root → parent. Empty
	// for roots. Only populated when groupBy=none.
	Breadcrumb []CatalogReportConfigItem `json:"breadcrumb,omitempty"`
}

type CatalogReportConfigGroup struct {
	ConfigItem CatalogReportConfigItem  `json:"configItem"`
	Changes    []CatalogReportChange    `json:"changes,omitempty"`
	Analyses   []CatalogReportAnalysis  `json:"analyses,omitempty"`
	Access     []CatalogReportAccess    `json:"access,omitempty"`
	AccessLogs []CatalogReportAccessLog `json:"accessLogs,omitempty"`
}

type CatalogReportSections struct {
	Changes       bool `json:"changes"`
	Insights      bool `json:"insights"`
	Relationships bool `json:"relationships"`
	Access        bool `json:"access"`
	AccessLogs    bool `json:"accessLogs"`
	ConfigJSON    bool `json:"configJSON"`
}

// CatalogReportChange wraps models.ConfigChange with camelCase JSON tags
// to match report/config-types.ts ConfigChange interface.
type CatalogReportArtifact struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	Checksum    string `json:"checksum,omitempty"`
	Path        string `json:"path,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	DataURI     string `json:"dataUri,omitempty"`
}

type CatalogReportChange struct {
	ID                string                  `json:"id,omitempty"`
	ConfigID          string                  `json:"configID,omitempty"`
	ConfigName        string                  `json:"configName,omitempty"`
	ConfigType        string                  `json:"configType,omitempty"`
	Permalink         string                  `json:"permalink,omitempty"`
	ChangeType        string                  `json:"changeType"`
	Category          string                  `json:"category,omitempty"`
	Severity          string                  `json:"severity,omitempty"`
	Source            string                  `json:"source,omitempty"`
	Summary           string                  `json:"summary,omitempty"`
	Details           map[string]any          `json:"details,omitempty"`
	TypedChange       map[string]any          `json:"typedChange,omitempty"`
	CreatedBy         string                  `json:"createdBy,omitempty"`
	ExternalCreatedBy string                  `json:"externalCreatedBy,omitempty"`
	CreatedAt         string                  `json:"createdAt,omitempty"`
	Count             int                     `json:"count,omitempty"`
	Artifacts         []CatalogReportArtifact `json:"artifacts,omitempty"`
}

func NewCatalogReportChange(c models.ConfigChange, configName, configType string) CatalogReportChange {
	r := CatalogReportChange{
		ID:         c.ID,
		ConfigID:   c.ConfigID,
		ConfigName: configName,
		ConfigType: configType,
		Permalink:  ConfigPermalink(c.ConfigID),
		ChangeType: c.ChangeType,
		Severity:   string(c.Severity),
		Source:     c.Source,
		Summary:    c.Summary,
		CreatedAt:  c.CreatedAt.Format(time.RFC3339),
		Count:      c.Count,
	}
	if c.CreatedBy != nil {
		r.CreatedBy = c.CreatedBy.String()
	}
	if c.ExternalCreatedBy != nil {
		r.ExternalCreatedBy = *c.ExternalCreatedBy
	}
	if len(c.Details) > 0 {
		var details map[string]any
		if err := json.Unmarshal(c.Details, &details); err == nil {
			r.Details = details
		}
	}
	return r
}

type CatalogReportAnalysis struct {
	ID            string `json:"id,omitempty"`
	ConfigID      string `json:"configID,omitempty"`
	ConfigName    string `json:"configName,omitempty"`
	ConfigType    string `json:"configType,omitempty"`
	Permalink     string `json:"permalink,omitempty"`
	Analyzer      string `json:"analyzer"`
	Message       string `json:"message,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Status        string `json:"status,omitempty"`
	Severity      string `json:"severity,omitempty"`
	AnalysisType  string `json:"analysisType,omitempty"`
	Source        string `json:"source,omitempty"`
	FirstObserved string `json:"firstObserved,omitempty"`
	LastObserved  string `json:"lastObserved,omitempty"`
}

func NewCatalogReportAnalysis(a models.ConfigAnalysis, configName, configType string) CatalogReportAnalysis {
	r := CatalogReportAnalysis{
		ID:           a.ID.String(),
		ConfigID:     a.ConfigID.String(),
		ConfigName:   configName,
		ConfigType:   configType,
		Permalink:    ConfigPermalink(a.ConfigID.String()),
		Analyzer:     a.Analyzer,
		Message:      a.Message,
		Summary:      a.Summary,
		Status:       a.Status,
		Severity:     string(a.Severity),
		AnalysisType: string(a.AnalysisType),
		Source:       a.Source,
	}
	if a.FirstObserved != nil {
		r.FirstObserved = a.FirstObserved.Format(time.RFC3339)
	}
	if a.LastObserved != nil {
		r.LastObserved = a.LastObserved.Format(time.RFC3339)
	}
	return r
}

type CatalogReportRelationship struct {
	ConfigID  string `json:"configID"`
	RelatedID string `json:"relatedID"`
	Relation  string `json:"relation"`
	Direction string `json:"direction,omitempty"`
}

type CatalogReportTreeNode struct {
	CatalogReportConfigItem `json:",inline"`
	EdgeType                string                  `json:"edgeType,omitempty"` // "parent", "child", "related", "target"
	Relation                string                  `json:"relation,omitempty"`
	Children                []CatalogReportTreeNode `json:"children,omitempty"`
}

type CatalogReportConfigItem struct {
	ID          string            `json:"id"`
	Permalink   string            `json:"permalink,omitempty"`
	Name        string            `json:"name"`
	Type        string            `json:"type,omitempty"`
	ConfigClass string            `json:"configClass,omitempty"`
	Status      string            `json:"status,omitempty"`
	Health      string            `json:"health,omitempty"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	CreatedAt   string            `json:"createdAt,omitempty"`
	UpdatedAt   string            `json:"updatedAt,omitempty"`
}

func NewCatalogReportConfigItem(ci models.ConfigItem) CatalogReportConfigItem {
	r := CatalogReportConfigItem{
		ID:          ci.ID.String(),
		Permalink:   ConfigPermalink(ci.ID.String()),
		Name:        ci.GetName(),
		ConfigClass: ci.ConfigClass,
		Tags:        ci.Tags,
	}
	if ci.Type != nil {
		r.Type = *ci.Type
	}
	if ci.Status != nil {
		r.Status = *ci.Status
	}
	if ci.Health != nil {
		r.Health = string(*ci.Health)
	}
	if ci.Description != nil {
		r.Description = *ci.Description
	}
	if ci.Labels != nil {
		r.Labels = *ci.Labels
	}
	if !ci.CreatedAt.IsZero() {
		r.CreatedAt = ci.CreatedAt.Format(time.RFC3339)
	}
	if ci.UpdatedAt != nil {
		r.UpdatedAt = ci.UpdatedAt.Format(time.RFC3339)
	}
	return r
}

type CatalogReportAccess struct {
	ConfigID        string   `json:"configId,omitempty"`
	ConfigName      string   `json:"configName,omitempty"`
	ConfigType      string   `json:"configType,omitempty"`
	Permalink       string   `json:"permalink,omitempty"`
	UserID          string   `json:"userId"`
	UserName        string   `json:"userName"`
	Email           string   `json:"email"`
	Role            string   `json:"role"`
	RoleExternalIDs []string `json:"roleExternalIds,omitempty"`
	UserType        string   `json:"userType"`
	CreatedAt       string   `json:"createdAt"`
	LastSignedInAt  *string  `json:"lastSignedInAt,omitempty"`
	LastReviewedAt  *string  `json:"lastReviewedAt,omitempty"`
}

func NewCatalogReportAccess(a models.ConfigAccessSummary) CatalogReportAccess {
	r := CatalogReportAccess{
		ConfigID:   a.ConfigID.String(),
		ConfigName: a.ConfigName,
		ConfigType: a.ConfigType,
		Permalink:  ConfigPermalink(a.ConfigID.String()),
		UserID:     a.ExternalUserID.String(),
		UserName:   a.User,
		Email:      a.Email,
		Role:       a.Role,
		UserType:   a.UserType,
		CreatedAt:  a.CreatedAt.Format(time.RFC3339),
	}
	if a.LastSignedInAt != nil {
		s := a.LastSignedInAt.Format(time.RFC3339)
		r.LastSignedInAt = &s
	}
	if a.LastReviewedAt != nil {
		s := a.LastReviewedAt.Format(time.RFC3339)
		r.LastReviewedAt = &s
	}
	return r
}

type CatalogReportAccessLog struct {
	ConfigID   string            `json:"configId,omitempty"`
	Permalink  string            `json:"permalink,omitempty"`
	UserID     string            `json:"userId"`
	UserName   string            `json:"userName"`
	ConfigName string            `json:"configName"`
	ConfigType string            `json:"configType"`
	CreatedAt  string            `json:"createdAt"`
	MFA        bool              `json:"mfa"`
	Count      int               `json:"count"`
	Properties map[string]string `json:"properties,omitempty"`
}

func NewCatalogReportRelationship(configID string, rc models.ConfigRelationship) CatalogReportRelationship {
	r := CatalogReportRelationship{
		ConfigID:  rc.ConfigID,
		RelatedID: rc.RelatedID,
		Relation:  rc.Relation,
	}
	if rc.ConfigID == configID {
		r.Direction = "outgoing"
	} else {
		r.Direction = "incoming"
	}
	return r
}
