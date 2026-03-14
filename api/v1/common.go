package v1

const (
	ConditionReady = "Ready"
)

const (
	ReadyReasonSynced           = "Synced"
	ReadyReasonValidationFailed = "ValidationFailed"
	ReadyReasonPersistFailed    = "PersistFailed"
	ReadyReasonDeleteFailed     = "DeleteFailed"
)
