package connection

import (
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/rbac"
)

// Summary identifies a connection without any of its settings, so it never carries a credential.
type Summary struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Type      string    `json:"type"`
	Source    string    `json:"source"`
}

const summaryColumns = "id, name, namespace, type, source"

// ListSummaries lists every connection's summary. It requires read on connections.
func ListSummaries(ctx context.Context) ([]Summary, error) {
	if err := authorizeRead(ctx); err != nil {
		return nil, err
	}

	var summaries []Summary
	if err := ctx.DB().Table("connection_details").Select(summaryColumns).Scan(&summaries).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to list connections")
	}
	return summaries, nil
}

// GetSummary returns the summary of the connection with the namespace and name. It requires read on connections.
func GetSummary(ctx context.Context, namespace, name string) (*Summary, error) {
	if err := authorizeRead(ctx); err != nil {
		return nil, err
	}

	var summaries []Summary
	if err := ctx.DB().Table("connection_details").Select(summaryColumns).
		Where("namespace = ? AND name = ?", namespace, name).Limit(1).Scan(&summaries).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to find connection %s/%s", namespace, name)
	}
	if len(summaries) == 0 {
		return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "connection %s/%s not found", namespace, name)
	}
	return &summaries[0], nil
}

// authorizeRead returns an EFORBIDDEN error unless the person making the request may read connections, as reading
// connection_details through /db requires.
func authorizeRead(ctx context.Context) error {
	if rbac.CheckContext(ctx, policy.ObjectConnection, policy.ActionRead) {
		return nil
	}
	return ctx.Oops().Code(dutyAPI.EFORBIDDEN).
		Errorf("access denied to %s: %s required on %s", ctx.Subject(), policy.ActionRead, policy.ObjectConnection)
}
