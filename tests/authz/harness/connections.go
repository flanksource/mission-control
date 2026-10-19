package harness

import (
	"net/http"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// AllConnections selects every connection.
func AllConnections() Selector {
	return func(string) map[string]any {
		return map[string]any{"connection": map[string]any{"name": "*"}}
	}
}

// ConnectionsHere selects the connections in the case's namespace.
func ConnectionsHere() Selector {
	return func(namespace string) map[string]any {
		return map[string]any{"connection": map[string]any{"namespace": namespace}}
	}
}

// ConnectionsIn selects the connections in another namespace.
func ConnectionsIn(namespace string) Selector {
	return func(string) map[string]any {
		return map[string]any{"connection": map[string]any{"namespace": namespace}}
	}
}

// Connection is a connection a fixture created.
type Connection struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	URL       string `json:"url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// Body is the connection as it's sent to create or update one.
func (c Connection) Body() map[string]any {
	return map[string]any{
		"name": c.Name, "namespace": c.Namespace, "type": c.Type, "url": c.URL, "username": c.Username, "password": c.Password,
	}
}

// NewConnection returns an HTTP connection in the case's namespace, with credentials of its own, without creating it.
func (f *Fixture) NewConnection(url string) Connection {
	return Connection{
		Name:      f.Name("conn"),
		Namespace: f.Namespace,
		Type:      "http",
		URL:       url,
		Username:  "authz-user",
		Password:  f.Secret(),
	}
}

// Connection creates an HTTP connection to the URL in the case's namespace, through the connection API.
func (f *Fixture) Connection(url string) Connection {
	ginkgo.GinkgoHelper()
	conn := f.NewConnection(url)
	password := conn.Password

	resp := f.mustDo(Request{Method: http.MethodPost, Path: "/connection", Body: conn.Body()}, http.StatusCreated)
	Expect(resp.Decode(&conn)).To(Succeed())
	conn.Password = password
	f.Defer(func() { f.Admin().Do(Request{Method: http.MethodDelete, Path: "/connection/" + conn.ID}) })
	return conn
}

// StoredConnection reads the connection with the id as the admin, with its credentials masked, or returns nil when
// there's none.
func (f *Fixture) StoredConnection(id string) *Connection {
	ginkgo.GinkgoHelper()
	resp := f.mustDo(Request{Method: http.MethodGet, Path: "/connection/" + id}, http.StatusOK, http.StatusNotFound)
	if resp.Status == http.StatusNotFound {
		return nil
	}
	var conn Connection
	Expect(resp.Decode(&conn)).To(Succeed())
	return &conn
}
