package harness

import (
	"net/http"
	"reflect"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Resource is a kind of thing cases create, read and delete, only through Mission Control's public API.
// Kinds are pointers to structs that the API's JSON decodes into.
type Resource interface {
	// ScopeType is the key a Scope target selects the kind with, e.g. connection.
	ScopeType() string

	// Prepare fills in what the resource needs before it's created: its id, a name and namespace of the case,
	// credentials of its own.
	Prepare(f *Fixture)

	// CreateRequest creates the resource. It's answered with the resource.
	CreateRequest() Request

	// Path is where the resource is read with GET, and deleted with DELETE.
	Path() string
}

// Secretive is a resource with a credential that no response may reveal.
type Secretive interface {
	Secret() string
}

// Build prepares the resource without creating it, e.g. to send it in a request a case checks.
func Build[R Resource](f *Fixture, r R) R {
	r.Prepare(f)
	return r
}

// Create prepares and creates the resource as the admin, and deletes it after the case.
func Create[R Resource](f *Fixture, r R) R {
	ginkgo.GinkgoHelper()
	Build(f, r)
	resp := f.Admin().Do(r.CreateRequest())
	Expect(resp.Status).To(BeElementOf(http.StatusOK, http.StatusCreated), resp.String())
	f.Defer(func() { f.Admin().Do(DELETE(r.Path())) })
	return r
}

// Read reads the resource as the admin, as it's stored now, or returns false when there's none.
func Read[R Resource](f *Fixture, r R) (R, bool) {
	ginkgo.GinkgoHelper()
	stored := reflect.New(reflect.TypeOf(r).Elem()).Interface().(R)
	resp := f.Admin().Do(GET(r.Path()))
	if resp.Status == http.StatusNotFound {
		return stored, false
	}
	Expect(resp.Status).To(Equal(http.StatusOK), resp.String())
	Expect(resp.Decode(stored)).To(Succeed())
	return stored, true
}

// Exists reports whether the resource exists.
func Exists[R Resource](f *Fixture, r R) func() bool {
	return func() bool {
		_, ok := Read(f, r)
		return ok
	}
}

// Gone reports whether the resource doesn't exist.
func Gone[R Resource](f *Fixture, r R) func() bool {
	return func() bool {
		_, ok := Read(f, r)
		return !ok
	}
}

// Changed reports whether the resource exists, and changed as changed tells from how it's stored now.
func Changed[R Resource](f *Fixture, r R, changed func(stored R) bool) func() bool {
	return func() bool {
		stored, ok := Read(f, r)
		return ok && changed(stored)
	}
}

// Any reports whether any of the checks does.
func Any(checks ...func() bool) func() bool {
	return func() bool {
		for _, check := range checks {
			if check() {
				return true
			}
		}
		return false
	}
}

// NoSecret checks that a response doesn't reveal any of the resources' credentials.
func NoSecret(resources ...Secretive) func(Response) {
	return func(resp Response) {
		for _, r := range resources {
			Expect(strings.Contains(string(resp.Body), r.Secret())).To(BeFalse(), "%s revealed a credential", resp.Request)
		}
	}
}

// AllOf selects every resource of the kind.
func AllOf[R Resource]() Selector {
	return func(string) map[string]any {
		return map[string]any{scopeType[R](): map[string]any{"name": "*"}}
	}
}

// Here selects the resources of the kind in the case's namespace.
func Here[R Resource]() Selector {
	return func(namespace string) map[string]any {
		return map[string]any{scopeType[R](): map[string]any{"namespace": namespace}}
	}
}

// In selects the resources of the kind in another namespace.
func In[R Resource](namespace string) Selector {
	return func(string) map[string]any {
		return map[string]any{scopeType[R](): map[string]any{"namespace": namespace}}
	}
}

func scopeType[R Resource]() string {
	var r R
	return r.ScopeType()
}
