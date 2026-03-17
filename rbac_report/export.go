package rbac_report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/duty/context"

	"github.com/flanksource/incident-commander/api"
)

func Export(ctx context.Context, opts Options, format string) ([]byte, error) {
	report, err := BuildReport(ctx, opts)
	if err != nil {
		return nil, err
	}

	ctx.Logger.V(3).Infof("Report built: %d resources, %d users, %d changelog entries",
		len(report.Resources), report.Summary.TotalUsers, len(report.Changelog))

	switch format {
	case "csv":
		if opts.ByUser {
			return renderCSVByUser(report)
		}
		return renderCSV(report)
	case "facet-html":
		return RenderFacetHTML(ctx, report, opts.ByUser)
	case "facet-pdf":
		return RenderFacetPDF(ctx, report, opts.ByUser)
	default:
		return json.MarshalIndent(report, "", "  ")
	}
}

func renderCSV(report *api.RBACReport) ([]byte, error) {
	var buf strings.Builder
	w := csv.NewWriter(&buf)

	header := []string{
		"Config Name", "Config Type", "User Name", "Email",
		"Role", "Role Source", "Source System",
		"Created", "Last Sign In", "Last Reviewed",
		"Stale", "Review Overdue",
	}
	if err := w.Write(header); err != nil {
		return nil, fmt.Errorf("failed to write CSV header: %w", err)
	}

	for _, resource := range report.Resources {
		for _, u := range resource.Users {
			row := []string{
				resource.ConfigName,
				resource.ConfigType,
				u.UserName,
				u.Email,
				u.Role,
				u.RoleSource,
				u.SourceSystem,
				u.CreatedAt.Format(time.RFC3339),
				formatOptionalTime(u.LastSignedInAt),
				formatOptionalTime(u.LastReviewedAt),
				fmt.Sprintf("%t", u.IsStale),
				fmt.Sprintf("%t", u.IsReviewOverdue),
			}
			if err := w.Write(row); err != nil {
				return nil, fmt.Errorf("failed to write CSV row: %w", err)
			}
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("failed to flush CSV: %w", err)
	}

	return []byte(buf.String()), nil
}

func renderCSVByUser(report *api.RBACReport) ([]byte, error) {
	var buf strings.Builder
	w := csv.NewWriter(&buf)

	header := []string{
		"User Name", "Email", "Config Name", "Config Type",
		"Role", "Role Source", "Created",
		"Last Sign In", "Last Reviewed",
		"Stale", "Review Overdue",
	}
	if err := w.Write(header); err != nil {
		return nil, fmt.Errorf("failed to write CSV header: %w", err)
	}

	for _, user := range report.Users {
		for _, r := range user.Resources {
			row := []string{
				user.UserName,
				user.Email,
				r.ConfigName,
				r.ConfigType,
				r.Role,
				r.RoleSource,
				r.CreatedAt.Format(time.RFC3339),
				formatOptionalTime(r.LastSignedInAt),
				formatOptionalTime(r.LastReviewedAt),
				fmt.Sprintf("%t", r.IsStale),
				fmt.Sprintf("%t", r.IsReviewOverdue),
			}
			if err := w.Write(row); err != nil {
				return nil, fmt.Errorf("failed to write CSV row: %w", err)
			}
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("failed to flush CSV: %w", err)
	}
	return []byte(buf.String()), nil
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
