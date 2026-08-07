package main

import (
	"context"
	"fmt"
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

// RegisterIdentity is the scraped half of an identity-register entry. It carries
// no review state — owner, privilege_level, review_decision and the rest are
// determinations the governance repository owns, and faro cannot evidence them.
type RegisterIdentity struct {
	ID               string          `json:"id" yaml:"id"`
	IdentityType     string          `json:"identity_type" yaml:"identity_type"`
	IdentityProvider string          `json:"identity_provider" yaml:"identity_provider"`
	Name             string          `json:"name,omitempty" yaml:"name,omitempty"`
	Email            string          `json:"email,omitempty" yaml:"email,omitempty"`
	Aliases          []string        `json:"aliases" yaml:"aliases"`
	ExternalUserID   string          `json:"external_user_id" yaml:"external_user_id"`
	UserType         string          `json:"user_type" yaml:"user_type"`
	Tenant           string          `json:"tenant,omitempty" yaml:"tenant,omitempty"`
	ProvisionedAt    *string         `json:"provisioned_at" yaml:"provisioned_at"`
	Groups           []RegisterGroup `json:"groups" yaml:"groups"`
	ConfigAccess     []RegisterGrant `json:"config_access" yaml:"config_access"`
}

// AccessExportResult is the document `access users export` prints.
type AccessExportResult struct {
	Context    string             `json:"context,omitempty" yaml:"context,omitempty"`
	ExportedAt string             `json:"exported_at" yaml:"exported_at"`
	Entries    []RegisterIdentity `json:"entries" yaml:"entries"`
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

// identityTypeSkip marks a principal that is deliberately not an identity-register
// entry, as distinct from one this code does not recognise.
const identityTypeSkip = ""

// identityTypeFor maps the scraped user type onto the register's vocabulary, which
// is person and workload_identity only. An unrecognised type is an error rather
// than a guess — misclassifying a principal as a person would put it in front of a
// human reviewer under false pretences.
//
// "Group" resolves to no entry at all: a group is neither a person nor a workload,
// and the access it holds already reaches the register through each member's own
// grant, recorded as `group:<name>` by AccessGrant.RoleSource. Emitting the group
// as its own entry would double-count that same access.
func identityTypeFor(userType string) (string, error) {
	switch userType {
	case "Human", "User", "GitHub::User", "local":
		return "person", nil
	case "ServiceAccount", "AWSService":
		return "workload_identity", nil
	case "Group":
		return identityTypeSkip, nil
	default:
		return "", fmt.Errorf("external user has unmapped user_type %q: extend identityTypeFor before exporting it", userType)
	}
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
		rows = append(rows, RegisterGrant{
			ConfigID:        grant.ConfigID.String(),
			ConfigName:      grant.ConfigName,
			ConfigType:      grant.ConfigType,
			Role:            grant.Role,
			RoleExternalIDs: grant.RoleExternalIDs,
			Grant:           grant.RoleSource(),
			CreatedAt:       registerDate(&grant.CreatedAt),
			LastSignedInAt:  registerDate(grant.LastSignedInAt),
			LastReviewedAt:  registerDate(grant.LastReviewedAt),
		})
	}
	return rows
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
	grantsByUser := map[uuid.UUID][]sdk.AccessGrant{}
	for _, grant := range grants {
		grantsByUser[grant.ExternalUserID] = append(grantsByUser[grant.ExternalUserID], grant)
	}

	entries := make([]RegisterIdentity, 0, len(users))
	for _, user := range users {
		held := grantsByUser[user.ID]
		if len(held) == 0 {
			continue
		}
		identityType, err := identityTypeFor(user.UserType)
		if err != nil {
			return nil, fmt.Errorf("external user %s: %w", user.ID, err)
		}
		if identityType == identityTypeSkip {
			// Loud, not silent: a principal holding access that produces no entry is
			// exactly the kind of omission an access review must not discover late.
			logger.Warnf("skipping %s (%s): group principals are represented by their members' group:<name> grants, not as their own entry", user.Name, user.UserType)
			continue
		}

		entry := RegisterIdentity{
			ID:               "external-user-" + user.ID.String(),
			IdentityType:     identityType,
			IdentityProvider: identityProvider(held),
			Aliases:          user.Aliases,
			ExternalUserID:   user.ID.String(),
			UserType:         user.UserType,
			Tenant:           user.Tenant,
			ProvisionedAt:    registerDate(&user.CreatedAt),
			Groups:           groups[user.ID],
			ConfigAccess:     registerGrants(held),
		}
		if identityType == "person" {
			entry.Name = user.Name
			if user.Email != nil {
				entry.Email = *user.Email
			}
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
	return entries, nil
}

// unattributedGrants counts the grants no exported entry accounts for, keyed by the
// principal holding them. Kubernetes-style group bindings (system:authenticated and
// friends) are recorded against a nil external user, so a per-user register cannot
// represent them — but they are often the broadest access in the estate, and
// dropping them without a word is exactly the omission an access review must not
// discover late.
func unattributedGrants(entries []RegisterIdentity, grants []sdk.AccessGrant) map[string]int {
	exported := make(map[string]bool, len(entries))
	for _, entry := range entries {
		exported[entry.ExternalUserID] = true
	}

	orphaned := map[string]int{}
	for _, grant := range grants {
		if exported[grant.ExternalUserID.String()] {
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

// groupsByUser resolves every group membership in one pass, so the export costs a
// fixed number of requests rather than one per user.
func groupsByUser(ctx context.Context, client *sdk.Client) (map[uuid.UUID][]RegisterGroup, error) {
	groups, _, err := client.ListExternalGroups(ctx, sdk.IdentityOptions{})
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return map[uuid.UUID][]RegisterGroup{}, nil
	}

	byID := make(map[uuid.UUID]sdk.ExternalGroupSummary, len(groups))
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		byID[group.ID] = group
		ids = append(ids, group.ID.String())
	}

	members, err := client.GetGroupMembers(ctx, ids)
	if err != nil {
		return nil, err
	}

	membership := map[uuid.UUID][]RegisterGroup{}
	for _, member := range members {
		if member.MembershipDeletedAt != nil {
			continue
		}
		group, ok := byID[member.GroupID]
		if !ok {
			continue
		}
		membership[member.UserID] = append(membership[member.UserID], RegisterGroup{
			ID:        group.ID.String(),
			Name:      group.Name,
			GroupType: group.GroupType,
			Tenant:    group.Tenant,
		})
	}
	for user := range membership {
		sort.Slice(membership[user], func(i, j int) bool { return membership[user][i].ID < membership[user][j].ID })
	}
	return membership, nil
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
		client, ctx, err := accessClient()
		if err != nil {
			return err
		}

		users, total, err := client.ListExternalUsers(ctx, sdk.IdentityOptions{Limit: exportLimit})
		if err != nil {
			return err
		}
		warnTruncated("users", len(users), total)

		grants, grantTotal, err := client.ListAccessGrants(ctx, sdk.AccessGrantOptions{})
		if err != nil {
			return err
		}
		warnTruncated("access entries", len(grants), grantTotal)

		membership, err := groupsByUser(ctx, client)
		if err != nil {
			return err
		}

		entries, err := projectRegisterIdentities(users, grants, membership)
		if err != nil {
			return err
		}

		for holder, count := range unattributedGrants(entries, grants) {
			logger.Warnf("%d grant(s) held by %q are not attributable to an external user and are absent from this export; review them via `faro access permissions`", count, holder)
		}

		contextName, err := accessContextName()
		if err != nil {
			return err
		}

		clicky.MustPrint(AccessExportResult{
			Context:    contextName,
			ExportedAt: time.Now().UTC().Format(registerDateLayout),
			Entries:    entries,
		}, clicky.Flags.FormatOptions)
		return nil
	},
}

func init() {
	AccessExport.Flags().IntVar(&exportLimit, "limit", 0, "Maximum number of users (0 for no limit)")
	clicky.BindAllFlags(AccessExport.PersistentFlags(), "format")
	clicky.RegisterSubCommand("access/users", AccessExport)
}
