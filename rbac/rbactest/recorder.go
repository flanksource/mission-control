// Package rbactest helps tests assert which subjects authorization checks are made as.
package rbactest

import (
	"fmt"
	"sync"

	casbinlog "github.com/casbin/casbin/v2/log"
	dutyRBAC "github.com/flanksource/duty/rbac"
)

// SubjectRecorder records the subject of every request Casbin enforces.
type SubjectRecorder struct {
	casbinlog.DefaultLogger

	mu       sync.Mutex
	subjects []string
}

// RecordSubjects starts recording the subject of every request the enforcer evaluates, with its cache off so that
// none is answered unseen. The returned function stops recording.
func RecordSubjects() (*SubjectRecorder, func()) {
	recorder := &SubjectRecorder{}
	enforcer := dutyRBAC.Enforcer()
	enforcer.EnableCache(false)
	enforcer.SetLogger(recorder)

	return recorder, func() {
		enforcer.SetLogger(&casbinlog.DefaultLogger{})
		enforcer.EnableCache(true)
	}
}

// LogEnforce records the request's subject.
func (r *SubjectRecorder) LogEnforce(_ string, request []any, _ bool, _ [][]string) {
	if len(request) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.subjects = append(r.subjects, fmt.Sprint(request[0]))
}

// Subjects returns the subjects recorded so far, in order.
func (r *SubjectRecorder) Subjects() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.subjects...)
}
