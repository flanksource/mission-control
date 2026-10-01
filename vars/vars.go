package vars

import (
	"time"

	"github.com/flanksource/duty/context"
)

var AuthMode = ""

const (
	// RLS Flag should be set explicitly to avoid unwanted DB Locks
	FlagRLSEnable  = "rls.enable"
	FlagRLSDisable = "rls.disable"
)

// rlsEnabledAtStart is rls.enable when the server started, since that's when Postgres row-level security
// is turned on or off. It's nil outside the server, e.g. in tests, where the property is read as it is.
var rlsEnabledAtStart *bool

// SetRLSEnabledAtStart records whether the server turned on Postgres row-level security.
func SetRLSEnabledAtStart(enabled bool) {
	rlsEnabledAtStart = &enabled
}

// RLSEnabled reports whether listings are filtered by row.
// Changing rls.enable takes effect when the server restarts, together with Postgres row-level security.
func RLSEnabled(ctx context.Context) bool {
	if rlsEnabledAtStart != nil {
		return *rlsEnabledAtStart
	}
	return ctx.Properties().On(false, FlagRLSEnable)
}

const PlaybookRunTimeout = 30 * time.Minute
