package vars

import "time"

var AuthMode = ""

const (
	// RLS Flag should be set explicitly to avoid unwanted DB Locks
	FlagRLSEnable  = "rls.enable"
	FlagRLSDisable = "rls.disable"
)

const PlaybookRunTimeout = 30 * time.Minute
