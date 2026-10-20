package authz_test

import (
	"strings"

	. "github.com/onsi/gomega"

	. "github.com/flanksource/incident-commander/tests/authz/harness"
)

// elsewhere is a namespace no case grants anything in.
const elsewhere = "authz-elsewhere"

// mentions reports whether a response mentions each of the ids, e.g. lists the rows of the configs.
func mentions(ids ...string) func(Response) bool {
	return func(resp Response) bool {
		for _, id := range ids {
			if !strings.Contains(string(resp.Body), id) {
				return false
			}
		}
		return true
	}
}

// specs/authorization/collection-access.md: configs are read whole or by the rows a subject's grants select.
var _ = Describe("Configs",
	Case{
		Name: "read a config through the database API",
		Arrange: func(f *Fixture) Attempt {
			config := Create(f, &Config{})
			return Attempt{
				Requests: []Request{
					GET(config.Path()),
					GET("/db/configs?id=eq.%s", config.ID),
					GET("/db/config_detail?id=eq.%s", config.ID),
				},
				Shows: mentions(config.ID),
			}
		},
		Grant: Rules(Allow(ActionRead, AllOf[*Config]())),
		NearMisses: []NearMiss{
			Miss("read on another namespace's configs", Allow(ActionRead, In[*Config](elsewhere))),
			Miss("delete on every config", Allow(ActionDelete, AllOf[*Config]())),
			Miss("read on every connection", Allow(ActionRead, AllOf[*Connection]())),
		},
	},

	// A snapshot and a relationship tree reach past the config, so they take a read of every config
	Case{
		Name: "read a config's snapshot and relationships",
		Arrange: func(f *Fixture) Attempt {
			config := Create(f, &Config{})
			return Attempt{
				Requests: []Request{
					GET("/snapshot/config/%s", config.ID),
					GET("/catalog/%s/relationships", config.ID),
				},
			}
		},
		Grant: Rules(Allow(ActionRead, AllOf[*Config]())),
		NearMisses: []NearMiss{
			Miss("read on the config's own namespace only", Allow(ActionRead, Here[*Config]())),
			Miss("delete on every config", Allow(ActionDelete, AllOf[*Config]())),
		},
	},

	// Row-level security answers a listing with the rows the grants select, and leaves out the rest
	Case{
		Name: "list only the configs a grant on a namespace selects",
		Arrange: func(f *Fixture) Attempt {
			pair := f.Name("pair")
			in := Create(f, &Config{Tags: map[string]string{"authz-pair": pair}})
			out := Create(f, &Config{Tags: map[string]string{"authz-pair": pair, "namespace": elsewhere}})
			filter := "tags->>authz-pair=eq." + pair
			return Attempt{
				Requests: []Request{
					GET("/db/config_items?%s", filter),
					GET("/db/configs?%s", filter),
					GET("/db/config_detail?%s", filter),
				},
				Shows: mentions(in.ID),
				Verify: func(resp Response) {
					Expect(string(resp.Body)).ToNot(ContainSubstring(out.ID), "%s listed a config outside the granted namespace", resp.Request)
				},
			}
		},
		Grant: Rules(Allow(ActionRead, Here[*Config]())),
		NearMisses: []NearMiss{
			Miss("read on another namespace's configs", Allow(ActionRead, In[*Config]("authz-nowhere"))),
			Miss("connection:use on the namespace's connections", Allow(ActionConnectionUse, Here[*Connection]())),
		},
	},
)
