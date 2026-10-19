package authz_test

import (
	"net/http"

	. "github.com/onsi/gomega"

	. "github.com/flanksource/incident-commander/tests/authz/harness"
)

// unreachable is a URL for connections that are never called.
const unreachable = "http://authz.invalid"

// shows checks that a response shows the connection, without its credentials.
func shows(conn *Connection) func(Response) {
	return func(resp Response) {
		Expect(string(resp.Body)).To(ContainSubstring(conn.ID), "%s didn't show the connection", resp.Request)
		NoSecret(conn)(resp)
	}
}

// specs/authorization/roles.md, Section 2.2: a connection is read and used separately, and read only as a whole type.
var _ = Describe("Connections",
	Case{
		Name: "read a connection",
		Arrange: func(f *Fixture) Attempt {
			conn := Create(f, &Connection{URL: unreachable})
			return Attempt{
				Requests: []Request{
					GET(conn.Path()),
					GET("/db/connection_details?id=eq.%s", conn.ID),
					GET("/db/connections_list?id=eq.%s", conn.ID),
				},
				Verify: shows(conn),
			}
		},
		Grant: Rules(Allow(ActionRead, AllOf[*Connection]())),
		NearMisses: []NearMiss{
			Miss("connection:use on every connection", Allow(ActionConnectionUse, AllOf[*Connection]())),
			Miss("update on every connection", Allow(ActionUpdate, AllOf[*Connection]())),
		},
	},

	Case{
		Name: "create a connection",
		Arrange: func(f *Fixture) Attempt {
			conn := Build(f, &Connection{URL: unreachable})
			f.Defer(func() { f.Admin().Do(DELETE(conn.Path())) })
			return Attempt{
				Requests: []Request{
					conn.CreateRequest(),
					// Saving a connection that doesn't exist creates it
					PUT(conn.Path(), conn.Body()),
				},
				Happened: Exists(f, conn),
				Verify:   NoSecret(conn),
			}
		},
		Grant: Rules(Allow(ActionCreate, AllOf[*Connection]())),
		NearMisses: []NearMiss{
			Miss("update on every connection", Allow(ActionUpdate, AllOf[*Connection]())),
		},
	},

	Case{
		Name: "update a connection",
		Arrange: func(f *Fixture) Attempt {
			conn := Create(f, &Connection{URL: unreachable})
			changed := conn.With(func(c *Connection) { c.URL = "http://changed.authz.invalid" })
			return Attempt{
				Requests: []Request{PUT(conn.Path(), changed.Body())},
				Happened: Changed(f, conn, func(stored *Connection) bool { return stored.URL == changed.URL }),
				Verify:   NoSecret(conn),
			}
		},
		Grant: Rules(Allow(ActionUpdate, AllOf[*Connection]())),
		NearMisses: []NearMiss{
			Miss("create on every connection", Allow(ActionCreate, AllOf[*Connection]())),
			Miss("read on every connection", Allow(ActionRead, AllOf[*Connection]())),
		},
	},

	Case{
		Name: "delete a connection",
		Arrange: func(f *Fixture) Attempt {
			conn := Create(f, &Connection{URL: unreachable})
			return Attempt{
				Requests: []Request{DELETE(conn.Path())},
				Happened: Gone(f, conn),
			}
		},
		Grant: Rules(Allow(ActionDelete, AllOf[*Connection]())),
		NearMisses: []NearMiss{
			Miss("update on every connection", Allow(ActionUpdate, AllOf[*Connection]())),
		},
	},

	// Testing a connection calls the other system with its credentials, so it's a use (roles.md, Section 2.2)
	Case{
		Name: "test a connection, which calls it with its credentials",
		Arrange: func(f *Fixture) Attempt {
			sink := f.Sink()
			conn := Create(f, &Connection{URL: sink.URL})
			return Attempt{
				Requests: []Request{POST("/connection/test/"+conn.ID, nil)},
				Happened: func() bool { return len(sink.Calls()) > 0 },
				Verify:   NoSecret(conn),
			}
		},
		Grant: Rules(Allow(ActionConnectionUse, Here[*Connection]())),
		NearMisses: []NearMiss{
			Miss("connection:use on another namespace's connections", Allow(ActionConnectionUse, In[*Connection](elsewhere))),
			Miss("read and update on every connection", Allow(ActionRead, AllOf[*Connection]()), Allow(ActionUpdate, AllOf[*Connection]())),
		},
	},

	// The connections table holds credentials, so it's neither read nor written through /db, whatever the grant
	Case{
		Name:  "read or write the connections table through the database API",
		Never: true,
		Arrange: func(f *Fixture) Attempt {
			conn := Create(f, &Connection{URL: unreachable})
			created := Build(f, &Connection{URL: unreachable})
			f.Defer(func() { f.Admin().Do(DELETE(created.Path())) })
			return Attempt{
				Requests: []Request{
					GET("/db/connections?id=eq.%s", conn.ID),
					POST("/db/connections", created.Body()),
					PATCH("/db/connections?id=eq."+conn.ID, map[string]any{"url": "http://changed.authz.invalid"}),
					DELETE("/db/connections?id=eq.%s", conn.ID),
				},
				Happened: Any(
					Gone(f, conn),
					Changed(f, conn, func(stored *Connection) bool { return stored.URL != conn.URL }),
					Exists(f, created),
				),
				Denied: []int{http.StatusMethodNotAllowed},
			}
		},
		Grant: Rules(
			Allow(ActionRead, AllOf[*Connection]()), Allow(ActionCreate, AllOf[*Connection]()),
			Allow(ActionUpdate, AllOf[*Connection]()), Allow(ActionDelete, AllOf[*Connection]()),
			Allow(ActionConnectionUse, AllOf[*Connection]()),
		),
	},
)
