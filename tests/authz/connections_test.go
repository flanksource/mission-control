package authz_test

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"

	. "github.com/flanksource/incident-commander/tests/authz/harness"
)

const (
	read   = "read"
	create = "create"
	update = "update"
	del    = "delete"
	use    = "connection:use"
)

// unreachable is a URL for connections that are never called.
const unreachable = "http://authz.invalid"

// withoutSecret checks that a response doesn't reveal the connection's password.
func withoutSecret(conn Connection) func(Response) {
	return func(resp Response) {
		Expect(string(resp.Body)).ToNot(ContainSubstring(conn.Password), "%s revealed the connection's password", resp.Request)
	}
}

// listsConnection checks that a /db response lists the connection, without its password.
func listsConnection(conn Connection) func(Response) {
	return func(resp Response) {
		var rows []map[string]any
		Expect(resp.Decode(&rows)).To(Succeed())
		Expect(rows).To(ContainElement(HaveKeyWithValue("id", conn.ID)), resp.String())
		withoutSecret(conn)(resp)
	}
}

// specs/authorization/roles.md, Section 2.2: a connection is read and used separately, and read only as a whole type.
var _ = Describe("Connections",
	Case{
		Name: "read a connection through the connection API",
		Arrange: func(f *Fixture) Attempt {
			conn := f.Connection(unreachable)
			return Attempt{
				Request: Request{Method: http.MethodGet, Path: "/connection/" + conn.ID},
				Verify:  withoutSecret(conn),
			}
		},
		Grant: []Rule{Allow(read, AllConnections())},
		NearMisses: []NearMiss{
			{Name: "connection:use on every connection", Rules: []Rule{Allow(use, AllConnections())}},
			{Name: "update on every connection", Rules: []Rule{Allow(update, AllConnections())}},
		},
	},

	Case{
		Name: "read connections through connection_details",
		Arrange: func(f *Fixture) Attempt {
			conn := f.Connection(unreachable)
			return Attempt{
				Request: Request{Method: http.MethodGet, Path: "/db/connection_details?id=eq." + conn.ID},
				Verify:  listsConnection(conn),
			}
		},
		Grant: []Rule{Allow(read, AllConnections())},
		NearMisses: []NearMiss{
			{Name: "connection:use on every connection", Rules: []Rule{Allow(use, AllConnections())}},
		},
	},

	Case{
		Name: "read connections through connections_list",
		Arrange: func(f *Fixture) Attempt {
			conn := f.Connection(unreachable)
			return Attempt{
				Request: Request{Method: http.MethodGet, Path: "/db/connections_list?id=eq." + conn.ID},
				Verify:  listsConnection(conn),
			}
		},
		Grant: []Rule{Allow(read, AllConnections())},
		NearMisses: []NearMiss{
			{Name: "connection:use on every connection", Rules: []Rule{Allow(use, AllConnections())}},
		},
	},

	Case{
		Name: "create a connection through the connection API",
		Arrange: func(f *Fixture) Attempt {
			conn := f.NewConnection(unreachable)
			conn.ID = uuid.NewString()
			body := conn.Body()
			body["id"] = conn.ID
			f.Defer(func() { f.Admin().Do(Request{Method: http.MethodDelete, Path: "/connection/" + conn.ID}) })
			return Attempt{
				Request:  Request{Method: http.MethodPost, Path: "/connection", Body: body},
				Happened: func() bool { return f.StoredConnection(conn.ID) != nil },
				Verify:   withoutSecret(conn),
			}
		},
		Grant: []Rule{Allow(create, AllConnections())},
		NearMisses: []NearMiss{
			{Name: "update on every connection", Rules: []Rule{Allow(update, AllConnections())}},
		},
	},

	Case{
		Name: "create a connection by saving one that doesn't exist",
		Arrange: func(f *Fixture) Attempt {
			conn := f.NewConnection(unreachable)
			conn.ID = uuid.NewString()
			f.Defer(func() { f.Admin().Do(Request{Method: http.MethodDelete, Path: "/connection/" + conn.ID}) })
			return Attempt{
				Request:  Request{Method: http.MethodPut, Path: "/connection/" + conn.ID, Body: conn.Body()},
				Happened: func() bool { return f.StoredConnection(conn.ID) != nil },
			}
		},
		Grant: []Rule{Allow(create, AllConnections())},
		NearMisses: []NearMiss{
			{Name: "update on every connection", Rules: []Rule{Allow(update, AllConnections())}},
		},
	},

	Case{
		Name: "update a connection",
		Arrange: func(f *Fixture) Attempt {
			conn := f.Connection(unreachable)
			changed := conn
			changed.URL = "http://changed.authz.invalid"
			return Attempt{
				Request:  Request{Method: http.MethodPut, Path: "/connection/" + conn.ID, Body: changed.Body()},
				Happened: func() bool { return f.StoredConnection(conn.ID).URL == changed.URL },
				Verify:   withoutSecret(conn),
			}
		},
		Grant: []Rule{Allow(update, AllConnections())},
		NearMisses: []NearMiss{
			{Name: "create on every connection", Rules: []Rule{Allow(create, AllConnections())}},
			{Name: "read on every connection", Rules: []Rule{Allow(read, AllConnections())}},
		},
	},

	Case{
		Name: "delete a connection",
		Arrange: func(f *Fixture) Attempt {
			conn := f.Connection(unreachable)
			return Attempt{
				Request:  Request{Method: http.MethodDelete, Path: "/connection/" + conn.ID},
				Happened: func() bool { return f.StoredConnection(conn.ID) == nil },
			}
		},
		Grant: []Rule{Allow(del, AllConnections())},
		NearMisses: []NearMiss{
			{Name: "update on every connection", Rules: []Rule{Allow(update, AllConnections())}},
		},
	},

	// Testing a connection calls the other system with its credentials, so it's a use (roles.md, Section 2.2)
	Case{
		Name: "test a connection, which calls it with its credentials",
		Arrange: func(f *Fixture) Attempt {
			sink := f.Sink()
			conn := f.Connection(sink.URL)
			return Attempt{
				Request:  Request{Method: http.MethodPost, Path: "/connection/test/" + conn.ID},
				Happened: func() bool { return len(sink.Calls()) > 0 },
				Verify:   withoutSecret(conn),
			}
		},
		Grant: []Rule{Allow(use, ConnectionsHere())},
		NearMisses: []NearMiss{
			{Name: "connection:use on another namespace's connections", Rules: []Rule{Allow(use, ConnectionsIn("authz-elsewhere"))}},
			{Name: "read and update on every connection", Rules: []Rule{Allow(read, AllConnections()), Allow(update, AllConnections())}},
		},
	},

	// The connections table holds credentials, so it's neither read nor written through /db, whatever the grant
	neverThroughDB(http.MethodGet, func(conn Connection) string { return "/db/connections?id=eq." + conn.ID }, nil),
	neverThroughDB(http.MethodPost, func(Connection) string { return "/db/connections" }, func(f *Fixture, conn Connection) map[string]any {
		conn.ID = uuid.NewString()
		body := conn.Body()
		body["id"] = conn.ID
		return body
	}),
	neverThroughDB(http.MethodPatch, func(conn Connection) string { return "/db/connections?id=eq." + conn.ID }, func(*Fixture, Connection) map[string]any {
		return map[string]any{"url": "http://changed.authz.invalid"}
	}),
	neverThroughDB(http.MethodDelete, func(conn Connection) string { return "/db/connections?id=eq." + conn.ID }, nil),
)

// neverThroughDB is a case of a request to the connections table through /db, which is refused with every grant on
// connections. What it would do is checked as the connection being read, changed or deleted.
func neverThroughDB(method string, path func(Connection) string, body func(*Fixture, Connection) map[string]any) Case {
	return Case{
		Name:  fmt.Sprintf("%s the connections table through the database API", strings.ToLower(method)),
		Never: true,
		Arrange: func(f *Fixture) Attempt {
			conn := f.Connection(unreachable)
			attempt := Attempt{
				Request: Request{Method: method, Path: path(conn)},
				Denied:  []int{http.StatusMethodNotAllowed},
			}
			if body != nil {
				attempt.Request.Body = body(f, conn)
			}
			switch method {
			case http.MethodPatch:
				attempt.Happened = func() bool { return f.StoredConnection(conn.ID).URL != conn.URL }
			case http.MethodDelete:
				attempt.Happened = func() bool { return f.StoredConnection(conn.ID) == nil }
			}
			return attempt
		},
		Grant: []Rule{
			Allow(read, AllConnections()), Allow(create, AllConnections()), Allow(update, AllConnections()),
			Allow(del, AllConnections()), Allow(use, AllConnections()),
		},
	}
}
