package vars

var AuthMode = ""

const (
	// RLS Flag should be set explicitly to avoid unwanted DB Locks
	FlagRLSEnable  = "rls.enable"
	FlagRLSDisable = "rls.disable"
)
