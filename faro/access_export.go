package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/clientcmd"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var exportLimit int
var exportPrincipalTypes []string

type accessExportOptions struct {
	Limit           int
	RequireComplete bool
	PrincipalTypes  []string
	UserTypes       []ProjectionUserTypeRule
}

// registerDateLayout is the calendar-date form governance registers record. The
// config-db timestamps are RFC 3339; the register keeps only the date, so a
// re-export on the same day is a no-op rather than a timestamp churn.
const registerDateLayout = "2006-01-02"

// RegisterGrant is one config_access row of an identity-register entry. The field
// names are the config_access_summary columns the register documents, so the
// export round-trips between Mission Control, the register and the access
// matrices without a renaming layer.
type RegisterGrant struct {
	ConfigID        string   `json:"config_id" yaml:"config_id"`
	ConfigName      string   `json:"config_name" yaml:"config_name"`
	ConfigType      string   `json:"config_type" yaml:"config_type"`
	Role            string   `json:"role" yaml:"role"`
	RoleExternalIDs []string `json:"role_external_ids" yaml:"role_external_ids"`
	Grant           string   `json:"grant" yaml:"grant"`
	ExternalUserID  string   `json:"external_user_id,omitempty" yaml:"external_user_id,omitempty"`
	ExternalGroupID string   `json:"external_group_id,omitempty" yaml:"external_group_id,omitempty"`
	CreatedAt       *string  `json:"created_at" yaml:"created_at"`
	LastSignedInAt  *string  `json:"last_signed_in_at" yaml:"last_signed_in_at"`
	LastReviewedAt  *string  `json:"last_reviewed_at" yaml:"last_reviewed_at"`
}

// RegisterGroup is one group membership of an identity-register entry.
type RegisterGroup struct {
	ID        string `json:"id" yaml:"id"`
	Name      string `json:"name" yaml:"name"`
	GroupType string `json:"group_type" yaml:"group_type"`
	Tenant    string `json:"tenant,omitempty" yaml:"tenant,omitempty"`
}

type RegisterMember struct {
	ExternalUserID string  `json:"external_user_id" yaml:"external_user_id"`
	Name           string  `json:"name" yaml:"name"`
	Email          string  `json:"email,omitempty" yaml:"email,omitempty"`
	UserType       string  `json:"user_type" yaml:"user_type"`
	AddedAt        *string `json:"added_at" yaml:"added_at"`
	LastSignedInAt *string `json:"last_signed_in_at,omitempty" yaml:"last_signed_in_at,omitempty"`
}

// RegisterIdentity is the scraped half of an identity-register entry. It carries
// no review state — owner, privilege_level, review_decision and the rest are
// determinations the governance repository owns, and faro cannot evidence them.
type RegisterIdentity struct {
	ID               string           `json:"id" yaml:"id"`
	IdentityType     string           `json:"identity_type" yaml:"identity_type"`
	IdentityProvider string           `json:"identity_provider" yaml:"identity_provider"`
	Name             string           `json:"name,omitempty" yaml:"name,omitempty"`
	Email            string           `json:"email,omitempty" yaml:"email,omitempty"`
	Aliases          []string         `json:"aliases" yaml:"aliases"`
	ExternalUserID   string           `json:"external_user_id,omitempty" yaml:"external_user_id,omitempty"`
	ExternalGroupID  string           `json:"external_group_id,omitempty" yaml:"external_group_id,omitempty"`
	UserType         string           `json:"user_type" yaml:"user_type"`
	GroupType        string           `json:"group_type,omitempty" yaml:"group_type,omitempty"`
	Tenant           string           `json:"tenant,omitempty" yaml:"tenant,omitempty"`
	ProvisionedAt    *string          `json:"provisioned_at" yaml:"provisioned_at"`
	Groups           []RegisterGroup  `json:"groups" yaml:"groups"`
	Members          []RegisterMember `json:"members" yaml:"members"`
	ConfigAccess     []RegisterGrant  `json:"config_access" yaml:"config_access"`
}

// AccessExportResult is the document `access users export` prints.
type AccessExportResult struct {
	Context    string              `json:"context,omitempty" yaml:"context,omitempty"`
	ExportedAt string              `json:"exported_at" yaml:"exported_at"`
	Entries    []RegisterIdentity  `json:"entries" yaml:"entries"`
	Warnings   []ProjectionWarning `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// registerDate truncates a config-db timestamp to the calendar date the register
// records. A nil timestamp stays nil: the register distinguishes "never" from
// "unknown", so an absent value must not become today.
func registerDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := t.UTC().Format(registerDateLayout)
	return &formatted
}

// identityProvider names the systems a principal reaches, derived from the
// config types it holds grants on.
func identityProvider(grants []sdk.AccessGrant) string {
	seen := map[string]bool{}
	providers := make([]string, 0, len(grants))
	for _, grant := range grants {
		provider, _, _ := strings.Cut(grant.ConfigType, "::")
		if provider != "" && !seen[provider] {
			seen[provider] = true
			providers = append(providers, provider)
		}
	}
	sort.Strings(providers)
	return strings.Join(providers, ", ")
}

func registerGrants(grants []sdk.AccessGrant) []RegisterGrant {
	rows := make([]RegisterGrant, 0, len(grants))
	for _, grant := range grants {
		row := RegisterGrant{
			ConfigID:        grant.ConfigID.String(),
			ConfigName:      grant.ConfigName,
			ConfigType:      grant.ConfigType,
			Role:            grant.Role,
			RoleExternalIDs: grant.RoleExternalIDs,
			Grant:           grant.RoleSource(),
			CreatedAt:       registerDate(&grant.CreatedAt),
			LastSignedInAt:  registerDate(grant.LastSignedInAt),
			LastReviewedAt:  registerDate(grant.LastReviewedAt),
		}
		if grant.ExternalGroupID != nil {
			row.ExternalGroupID = grant.ExternalGroupID.String()
			row.Grant = "direct"
		} else if grant.ExternalUserID != uuid.Nil {
			row.ExternalUserID = grant.ExternalUserID.String()
		}
		rows = append(rows, row)
	}
	return rows
}

// registerEmailPattern mirrors register-metadata.schema.json #/$defs/email, the shape
// a register accepts as a contact address.
var registerEmailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// splitContactEmail separates a contact address from a login. A local account carries
// something like admin@local in the email column: it is an identifier, not an address,
// so exporting it as email would produce a register the schema rejects, while dropping
// it would lose the login the merge matches on. It becomes an alias instead, leaving
// the address absent for a human to establish.
func splitContactEmail(email string, aliases []string) (string, []string) {
	if email == "" || registerEmailPattern.MatchString(email) {
		return email, aliases
	}
	for _, alias := range aliases {
		if alias == email {
			return "", aliases
		}
	}
	return "", append(aliases, email)
}

// projectRegisterIdentities joins users, their grants and their group memberships
// into register entries. Users holding no grants are omitted: the register records
// what a principal can reach, so a principal that reaches nothing is not evidence
// of access and must not be proposed as a review subject.
func projectRegisterIdentities(
	users []models.ExternalUser,
	grants []sdk.AccessGrant,
	groups map[uuid.UUID][]RegisterGroup,
) ([]RegisterIdentity, error) {
	entries, _, err := projectRegisterIdentitiesWithWarnings(users, grants, groups)
	return entries, err
}

func projectRegisterIdentitiesWithWarnings(
	users []models.ExternalUser,
	grants []sdk.AccessGrant,
	groups map[uuid.UUID][]RegisterGroup,
) ([]RegisterIdentity, []ProjectionWarning, error) {
	rules, err := compileIdentityTypeRules(nil)
	if err != nil {
		return nil, nil, err
	}
	return projectRegisterIdentitiesUsingRules(users, grants, groups, rules)
}

func projectRegisterIdentitiesUsingRules(
	users []models.ExternalUser,
	grants []sdk.AccessGrant,
	groups map[uuid.UUID][]RegisterGroup,
	rules []compiledIdentityTypeRule,
) ([]RegisterIdentity, []ProjectionWarning, error) {
	grantsByUser := map[uuid.UUID][]sdk.AccessGrant{}
	for _, grant := range grants {
		if grant.ExternalGroupID != nil {
			continue
		}
		grantsByUser[grant.ExternalUserID] = append(grantsByUser[grant.ExternalUserID], grant)
	}

	entries := make([]RegisterIdentity, 0, len(users))
	warnings := []ProjectionWarning{}
	for _, user := range users {
		held := grantsByUser[user.ID]
		if len(held) == 0 {
			continue
		}
		provider := identityProvider(held)
		identityType, err := classifyIdentityType(rules, user, provider)
		if err != nil {
			return nil, nil, fmt.Errorf("external user %s: %w", user.ID, err)
		}
		if identityType == identityTypeSkip {
			// Loud, not silent: a principal holding access that produces no entry is
			// exactly the kind of omission an access review must not discover late.
			logger.Warnf("skipping external user %s (%s): its configured identity type is skip", user.Name, user.UserType)
			continue
		}

		scraped := ""
		if user.Email != nil {
			scraped = *user.Email
		}
		contact, aliases := splitContactEmail(scraped, user.Aliases)
		name := user.Name
		if identityType == "workload_identity" {
			name, err = canonicalWorkloadPrincipal(user, provider)
			// WORKAROUND(missing-serviceaccount-namespace): omit invalid Kubernetes ServiceAccounts and expose them as projection warnings.
			// Correct fix: config-db must reject these subjects before persisting identities or grants.
			// Ref: gavel todo 7a6f89fd-9ec0-4af4-8fce-ef33966c34b5
			if isMissingServiceAccountNamespace(err) {
				warning := ProjectionWarning{Source: "external-user-" + user.ID.String(), Message: err.Error(), Count: len(held)}
				warnings = append(warnings, warning)
				logger.Warnf("%s; omitting %d grant(s) from the access export", warning.Message, warning.Count)
				continue
			}
			if err != nil {
				return nil, nil, fmt.Errorf("external user %s: %w", user.ID, err)
			}
		}

		entry := RegisterIdentity{
			ID:               "external-user-" + user.ID.String(),
			IdentityType:     identityType,
			IdentityProvider: provider,
			Name:             name,
			Aliases:          aliases,
			ExternalUserID:   user.ID.String(),
			UserType:         user.UserType,
			Tenant:           user.Tenant,
			ProvisionedAt:    registerDate(&user.CreatedAt),
			Groups:           groups[user.ID],
			ConfigAccess:     registerGrants(held),
		}
		if identityType == "person" {
			entry.Email = contact
		}
		if entry.Aliases == nil {
			entry.Aliases = []string{}
		}
		if entry.Groups == nil {
			entry.Groups = []RegisterGroup{}
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, warnings, nil
}

// unattributedGrants counts the grants no exported user or group entry accounts for,
// keyed by the principal holding them.
func unattributedGrants(entries []RegisterIdentity, grants []sdk.AccessGrant) map[string]int {
	exportedUsers := make(map[string]bool, len(entries))
	exportedGroups := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.ExternalUserID != "" {
			exportedUsers[entry.ExternalUserID] = true
		}
		if entry.ExternalGroupID != "" {
			exportedGroups[entry.ExternalGroupID] = true
		}
	}

	orphaned := map[string]int{}
	for _, grant := range grants {
		if grant.ExternalGroupID != nil && exportedGroups[grant.ExternalGroupID.String()] {
			continue
		}
		if grant.ExternalGroupID == nil && exportedUsers[grant.ExternalUserID.String()] {
			continue
		}
		holder := grant.GroupName
		if holder == "" {
			holder = grant.User
		}
		if holder == "" {
			holder = grant.ExternalUserID.String()
		}
		orphaned[holder]++
	}
	return orphaned
}

// accessContextName names the Mission Control context this export came from, so
// the consuming register can cite its provenance without being told separately.
func accessContextName() (string, error) {
	cfg, err := clientcmd.LoadConfig()
	if err != nil {
		return "", err
	}
	mcCtx := cfg.CurrentMCContext()
	if mcCtx == nil {
		return "", fmt.Errorf("no Mission Control context is selected")
	}
	return mcCtx.Name, nil
}

var AccessExport = &cobra.Command{
	Use:   "export",
	Short: "Export external users as identity-register entries",
	Long: `Exports one entry per external user in the shape a governance identity
register records, joining users, their config access and their group memberships.

Only scraped facts are exported. Ownership, privilege level, lifecycle status and
review decisions are determinations the governance repository owns, and are
deliberately absent — merge this export into a register rather than treating it as
a replacement for one.

Users holding no config access are omitted.

Examples:
  faro access users export --json
  faro access users export --context beta.example.com --yaml
  faro access users export --format json=identities.json`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		export, err := buildAccessExport(accessExportOptions{Limit: exportLimit, PrincipalTypes: exportPrincipalTypes})
		if err != nil {
			return err
		}
		clicky.MustPrint(export, clicky.Flags.FormatOptions)
		return nil
	},
}

// buildAccessExport projects the live config_access_summary view into register
// entries. Both the export command and identityAccess projections consume it so
// their identity classification and grant attribution cannot drift.
func buildAccessExport(options accessExportOptions) (AccessExportResult, error) {
	client, ctx, err := accessClient()
	if err != nil {
		return AccessExportResult{}, err
	}

	principalTypes := options.PrincipalTypes
	if len(principalTypes) == 0 {
		principalTypes = []string{"users"}
	}
	if err := validatePrincipalTypes(principalTypes); err != nil {
		return AccessExportResult{}, err
	}

	var users []models.ExternalUser
	var total int
	if containsPrincipalType(principalTypes, "users") {
		users, total, err = client.ListExternalUsers(ctx, sdk.IdentityOptions{Limit: options.Limit})
		if err != nil {
			return AccessExportResult{}, err
		}
		if options.RequireComplete {
			if err := requireCompleteProjection("users", len(users), total, options.Limit); err != nil {
				return AccessExportResult{}, err
			}
		}
		warnTruncated("users", len(users), total)
	}

	grants, grantTotal, err := client.ListAccessGrants(ctx, sdk.AccessGrantOptions{Limit: options.Limit})
	if err != nil {
		return AccessExportResult{}, err
	}
	if options.RequireComplete {
		if err := requireCompleteProjection("access", len(grants), grantTotal, options.Limit); err != nil {
			return AccessExportResult{}, err
		}
	}
	warnTruncated("access entries", len(grants), grantTotal)

	groupData, err := loadAccessGroups(ctx, client, options)
	if err != nil {
		return AccessExportResult{}, err
	}

	var entries []RegisterIdentity
	var warnings []ProjectionWarning
	if containsPrincipalType(principalTypes, "users") {
		rules, err := compileIdentityTypeRules(options.UserTypes)
		if err != nil {
			return AccessExportResult{}, err
		}
		entries, warnings, err = projectRegisterIdentitiesUsingRules(users, grants, groupData.byUser, rules)
		if err != nil {
			return AccessExportResult{}, err
		}
	}
	if containsPrincipalType(principalTypes, "groups") {
		entries = append(entries, projectRegisterGroups(groupData.groups, grants, groupData.members)...)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	relevantGrants := make([]sdk.AccessGrant, 0, len(grants))
	for _, grant := range grants {
		if grant.ExternalGroupID != nil && containsPrincipalType(principalTypes, "groups") {
			relevantGrants = append(relevantGrants, grant)
		}
		if grant.ExternalGroupID == nil && containsPrincipalType(principalTypes, "users") {
			relevantGrants = append(relevantGrants, grant)
		}
	}
	for holder, count := range unattributedGrants(entries, relevantGrants) {
		logger.Warnf("%d grant(s) held by %q are not attributable to an exported user or group and are absent from this export; review them via `faro access permissions`", count, holder)
	}

	contextName, err := accessContextName()
	if err != nil {
		return AccessExportResult{}, err
	}

	return AccessExportResult{
		Context:    contextName,
		ExportedAt: time.Now().UTC().Format(registerDateLayout),
		Entries:    entries,
		Warnings:   warnings,
	}, nil
}

func loadAccessExport(path string) (AccessExportResult, error) {
	if path == "" {
		return buildAccessExport(accessExportOptions{Limit: exportLimit, PrincipalTypes: []string{"users"}})
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return AccessExportResult{}, err
	}
	var export AccessExportResult
	if err := json.Unmarshal(body, &export); err != nil {
		return AccessExportResult{}, fmt.Errorf("%s: %w", path, err)
	}
	if export.Context == "" {
		return AccessExportResult{}, fmt.Errorf("%s: export has no context", path)
	}
	if export.ExportedAt == "" {
		return AccessExportResult{}, fmt.Errorf("%s: export has no exported_at", path)
	}
	return export, nil
}

func init() {
	AccessExport.Flags().IntVar(&exportLimit, "limit", 0, "Maximum number of users (0 for no limit)")
	AccessExport.Flags().StringSliceVar(&exportPrincipalTypes, "principal-types", []string{"users"}, "Principal types to export: users,groups")
	clicky.BindAllFlags(AccessExport.PersistentFlags(), "format")
	clicky.RegisterSubCommand("access/users", AccessExport)
}
