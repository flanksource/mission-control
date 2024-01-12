package playbook

const (
	// always run the action
	actionFilterAlways = "always"

	// skip the action
	actionFilterSkip = "skip"

	// run the action if any of the previous actions failed
	actionFilterFailure = "failure"

	// run the action if any of the previous actions timed out
	actionFilterTimeout = "timeout"

	// run the action if all of the previous actions succeeded
	actionFilterSuccess = "success"
)

var actionCelFunctions = map[string]func() any{
	"always":  func() any { return actionFilterAlways },
	"failure": func() any { return actionFilterFailure },
	"skip":    func() any { return actionFilterSkip },
	"success": func() any { return actionFilterSuccess },
	"timeout": func() any { return actionFilterTimeout },
}
