package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

var (
	reportFrom     []string
	reportRegister string
	reportDetail   bool
	reportPeople   bool
)

// reportPrincipal is one principal in the by-user matrix: every grant it holds,
// across every context the report was given.
type reportPrincipal struct {
	Key          string
	Label        string
	Owner        string
	IdentityType string
	Providers    []string
	Contexts     []string
	Grants       []reportGrant
}

type reportGrant struct {
	Context    string
	ConfigName string
	ConfigType string
	Role       string
	Grant      string
	LastSignIn string
}

// AccessReportResult is the printable by-user matrix.
type AccessReportResult struct {
	Provenance []reportSource    `json:"provenance" yaml:"provenance"`
	Users      []reportPrincipal `json:"users" yaml:"users"`
	Detail     bool              `json:"-" yaml:"-"`
}

type reportSource struct {
	Context    string `json:"context" yaml:"context"`
	ExportedAt string `json:"exported_at" yaml:"exported_at"`
	Identities int    `json:"identities" yaml:"identities"`
}

// ownersByIdentifier maps every identifier a register entry answers to onto the human
// the entry names as owner. The report folds on it so one person is one row: an
// account's grants stay recorded against the account that holds them, while the matrix
// a reviewer reads is grouped by who is answerable for them.
func ownersByIdentifier(path string) (map[string]string, error) {
	if path == "" {
		return map[string]string{}, nil
	}
	document, _, err := readRegister(path)
	if err != nil {
		return nil, err
	}
	entries, err := registerEntries(document)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	owners := map[string]string{}
	for _, entry := range entries {
		owner := mapString(entry, "owner")
		if owner == "" || owner == "not_evidenced" {
			continue
		}
		for _, identifier := range append(mapStrings(entry, "aliases"), mapString(entry, "external_user_id"), mapString(entry, "email")) {
			if identifier != "" {
				owners[strings.ToLower(identifier)] = owner
			}
		}
	}
	return owners, nil
}

// principalKey decides what counts as one row. An owner folds every account it is
// answerable for into one; without one, an identity stands alone rather than being
// guessed into a person.
func principalKey(identity RegisterIdentity, owners map[string]string) (key string, owner string) {
	for _, identifier := range append(append([]string{}, identity.Aliases...), identity.ExternalUserID, identity.Email) {
		if found, ok := owners[strings.ToLower(identifier)]; ok && identifier != "" {
			return "owner:" + found, found
		}
	}
	if identity.Email != "" {
		return "email:" + strings.ToLower(identity.Email), ""
	}
	return "user:" + identity.ExternalUserID, ""
}

func buildAccessReport(exports []AccessExportResult, owners map[string]string, peopleOnly bool) AccessReportResult {
	result := AccessReportResult{Provenance: []reportSource{}, Users: []reportPrincipal{}}
	index := map[string]int{}

	for _, export := range exports {
		result.Provenance = append(result.Provenance, reportSource{
			Context:    export.Context,
			ExportedAt: export.ExportedAt,
			Identities: len(export.Entries),
		})

		for _, identity := range export.Entries {
			if peopleOnly && identity.IdentityType != "person" {
				continue
			}
			key, owner := principalKey(identity, owners)
			position, seen := index[key]
			if !seen {
				label := identity.Email
				if owner != "" {
					label = owner
				} else if label == "" {
					label = identity.Name
				}
				if label == "" {
					label = identity.ExternalUserID
				}
				result.Users = append(result.Users, reportPrincipal{
					Key:          key,
					Label:        label,
					Owner:        owner,
					IdentityType: identity.IdentityType,
				})
				position = len(result.Users) - 1
				index[key] = position
			}

			user := &result.Users[position]
			user.Providers = appendUnique(user.Providers, identity.IdentityProvider)
			user.Contexts = appendUnique(user.Contexts, export.Context)
			for _, grant := range identity.ConfigAccess {
				user.Grants = append(user.Grants, reportGrant{
					Context:    export.Context,
					ConfigName: grant.ConfigName,
					ConfigType: grant.ConfigType,
					Role:       grant.Role,
					Grant:      grant.Grant,
					LastSignIn: lo.FromPtr(grant.LastSignedInAt),
				})
			}
		}
	}

	// People first, then by how much each holds: a review reads down from the broadest
	// access, and a workload identity is not what a reviewer starts with.
	sort.SliceStable(result.Users, func(i, j int) bool {
		left, right := result.Users[i], result.Users[j]
		if (left.IdentityType == "person") != (right.IdentityType == "person") {
			return left.IdentityType == "person"
		}
		if len(left.Grants) != len(right.Grants) {
			return len(left.Grants) > len(right.Grants)
		}
		return left.Label < right.Label
	})
	return result
}

func appendUnique(values []string, value string) []string {
	if value == "" || lo.Contains(values, value) {
		return values
	}
	return append(values, value)
}

// reportRow is one (principal × config type) cell of the matrix: what a principal can
// reach of one kind, and by which roles.
type reportRow struct {
	User     string `json:"user"`
	Type     string `json:"type"`
	Contexts string `json:"contexts"`
	Items    int    `json:"items"`
	Roles    string `json:"roles"`
}

func (r reportRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("User").Build(),
		api.Column("Type").Build(),
		api.Column("Contexts").Build(),
		api.Column("Items").Build(),
		api.Column("Roles").Build(),
	}
}

func (r reportRow) Row() map[string]any {
	return map[string]any{
		"User":     clicky.Text(r.User, "font-bold"),
		"Type":     clicky.Text(r.Type, "text-gray-600"),
		"Contexts": clicky.Text(r.Contexts, "text-gray-500"),
		"Items":    api.HumanNumber(int64(r.Items), "font-bold"),
		"Roles":    clicky.Text(r.Roles, "text-gray-600"),
	}
}

// reportGrantRow is one grant, for --detail.
type reportGrantRow struct {
	User       string `json:"user"`
	Item       string `json:"item"`
	Type       string `json:"type"`
	Role       string `json:"role"`
	Grant      string `json:"grant"`
	Context    string `json:"context"`
	LastSignIn string `json:"last_sign_in"`
}

func (r reportGrantRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("User").Build(),
		api.Column("Item").Build(),
		api.Column("Type").Build(),
		api.Column("Role").Build(),
		api.Column("Grant").Build(),
		api.Column("Context").Build(),
		api.Column("LastSignIn").Label("Last Sign In").Build(),
	}
}

func (r reportGrantRow) Row() map[string]any {
	return map[string]any{
		"User":       clicky.Text(r.User, "font-bold"),
		"Item":       clicky.Text(r.Item, "text-gray-700"),
		"Type":       clicky.Text(r.Type, "text-gray-600"),
		"Role":       clicky.Text(r.Role, "text-gray-600"),
		"Grant":      clicky.Text(r.Grant, "text-gray-500"),
		"Context":    clicky.Text(r.Context, "text-gray-500"),
		"LastSignIn": clicky.Text(r.LastSignIn, "text-gray-500"),
	}
}

func (r AccessReportResult) matrixRows() []reportRow {
	rows := []reportRow{}
	for _, user := range r.Users {
		byType := map[string][]reportGrant{}
		types := []string{}
		for _, grant := range user.Grants {
			if _, seen := byType[grant.ConfigType]; !seen {
				types = append(types, grant.ConfigType)
			}
			byType[grant.ConfigType] = append(byType[grant.ConfigType], grant)
		}
		sort.Strings(types)
		for _, configType := range types {
			grants := byType[configType]
			roles, contexts, items := []string{}, []string{}, []string{}
			for _, grant := range grants {
				roles = appendUnique(roles, grant.Role)
				contexts = appendUnique(contexts, shortContext(grant.Context))
				items = appendUnique(items, grant.ConfigName)
			}
			sort.Strings(roles)
			rows = append(rows, reportRow{
				User:     user.Label,
				Type:     configType,
				Contexts: strings.Join(contexts, ", "),
				Items:    len(items),
				Roles:    strings.Join(roles, ", "),
			})
		}
	}
	return rows
}

func (r AccessReportResult) detailRows() []reportGrantRow {
	rows := []reportGrantRow{}
	for _, user := range r.Users {
		grants := append([]reportGrant{}, user.Grants...)
		sort.SliceStable(grants, func(i, j int) bool {
			if grants[i].ConfigType != grants[j].ConfigType {
				return grants[i].ConfigType < grants[j].ConfigType
			}
			return grants[i].ConfigName < grants[j].ConfigName
		})
		for _, grant := range grants {
			rows = append(rows, reportGrantRow{
				User:       user.Label,
				Item:       grant.ConfigName,
				Type:       grant.ConfigType,
				Role:       grant.Role,
				Grant:      grant.Grant,
				Context:    shortContext(grant.Context),
				LastSignIn: grant.LastSignIn,
			})
		}
	}
	return rows
}

// shortContext keeps a matrix readable: a Mission Control context name is a hostname,
// and the first label distinguishes the instances without filling the column.
func shortContext(context string) string {
	host, _, _ := strings.Cut(context, ".")
	return host
}

func (r AccessReportResult) Pretty() api.Text {
	if len(r.Users) == 0 {
		return clicky.Text("No access found.", "text-gray-500")
	}
	people := lo.CountBy(r.Users, func(user reportPrincipal) bool { return user.IdentityType == "person" })
	grants := lo.SumBy(r.Users, func(user reportPrincipal) int { return len(user.Grants) })

	text := clicky.Text(fmt.Sprintf("Access by user: %d principals (%d people), %d grants", len(r.Users), people, grants), "font-bold text-gray-700")
	for _, source := range r.Provenance {
		text = text.NewLine().AddText(fmt.Sprintf("  %s: %d identities exported %s", source.Context, source.Identities, source.ExportedAt), "text-xs text-gray-500")
	}
	if r.Detail {
		return text.NewLine().Append(api.NewTableFrom(r.detailRows()))
	}
	return text.NewLine().Append(api.NewTableFrom(r.matrixRows()))
}

// AccessReport backs `access report`.
var AccessReport = &cobra.Command{
	Use:   "report",
	Short: "Report access as a matrix grouped by user",
	Long: `Groups every grant by the principal holding it and reports what each can
reach, one row per principal and kind of item.

Several --from exports are combined into one matrix, so a principal reachable from
more than one Mission Control context appears once with each grant naming its own.

With --register, rows fold onto the identity register's owner: an account whose
owner a reviewer has recorded is reported under that person, so a human who holds
access through several accounts reads as one row. Grants stay recorded against the
account that holds them.

Examples:
  faro access report --from .tmp/beta.json --from .tmp/prod-eu.json
  faro access report --from .tmp/beta.json --register registers/identity-register.yaml
  faro access report --from .tmp/beta.json --detail --markdown`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		owners, err := ownersByIdentifier(reportRegister)
		if err != nil {
			return err
		}

		exports := []AccessExportResult{}
		if len(reportFrom) == 0 {
			export, err := loadAccessExport("")
			if err != nil {
				return err
			}
			exports = append(exports, export)
		}
		for _, path := range reportFrom {
			export, err := loadAccessExport(path)
			if err != nil {
				return err
			}
			exports = append(exports, export)
		}

		result := buildAccessReport(exports, owners, reportPeople)
		result.Detail = reportDetail
		clicky.MustPrint(result, clicky.Flags.FormatOptions)
		return nil
	},
}

func init() {
	AccessReport.Flags().StringArrayVar(&reportFrom, "from", nil, "Read an export from this file (repeatable, one per context)")
	AccessReport.Flags().StringVar(&reportRegister, "register", "", "Fold rows onto this identity register's recorded owners")
	AccessReport.Flags().BoolVar(&reportDetail, "detail", false, "Report one row per grant instead of per kind of item")
	AccessReport.Flags().BoolVar(&reportPeople, "people", false, "Report only person identities")
	clicky.BindAllFlags(AccessReport.PersistentFlags(), "format")
	clicky.RegisterSubCommand("access", AccessReport)
}
