package catalog

import (
	"github.com/flanksource/incident-commander/db"
)

// hasGroupRow reports whether any row was granted via a group.
func hasGroupRow(rows []db.RBACAccessRow) bool {
	for _, r := range rows {
		if r.GroupName != nil && *r.GroupName != "" {
			return true
		}
	}
	return false
}

// ExpandGroupAccess emits every input row followed by one synthetic row per
// currently-active member of the row's granting group. Direct rows pass
// through unchanged. Soft-deleted memberships are filtered out so the
// expansion reflects current access, not audit history. Group rows with no
// active members emit only themselves.
//
// Synthetic rows preserve the parent row's GroupName so RoleSource() returns
// "group:<name>", which the TSX layer already renders as indirect access.
func ExpandGroupAccess(rows []db.RBACAccessRow, members []db.GroupMemberRow) []db.RBACAccessRow {
	if len(rows) == 0 {
		return rows
	}

	byGroup := make(map[string][]db.GroupMemberRow)
	for _, m := range members {
		if m.MembershipDeletedAt != nil {
			continue
		}
		byGroup[m.GroupName] = append(byGroup[m.GroupName], m)
	}

	out := make([]db.RBACAccessRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
		if row.GroupName == nil || *row.GroupName == "" {
			continue
		}
		for _, m := range byGroup[*row.GroupName] {
			synthetic := row
			synthetic.UserID = m.UserID
			synthetic.UserName = m.UserName
			synthetic.Email = m.Email
			synthetic.UserType = m.UserType
			synthetic.LastSignedInAt = m.LastSignedInAt
			synthetic.LastReviewedAt = nil
			synthetic.CreatedAt = m.MembershipAddedAt
			out = append(out, synthetic)
		}
	}
	return out
}
