package authz_test

import (
	. "github.com/flanksource/incident-commander/tests/authz/harness"
)

// runOnConfigs allows running the case's playbooks on the case's configs.
func runOnConfigs(action string) Rule {
	return Allow(action, Here[*Playbook]()).On(Here[*Config]())
}

// readConfigs allows reading the case's configs, which every action on a run of a playbook on one takes
// (specs/authorization/roles.md, Section 4.3).
var readConfigs = Allow(ActionRead, Here[*Config]())

// awaitingApproval starts a run of a playbook that waits for the case's person to approve it, as the admin.
func awaitingApproval(f *Fixture) *PlaybookRun {
	playbook := Create(f, &Playbook{Approvers: []string{f.Person}})
	config := Create(f, &Config{})
	return Create(f, RunOf(playbook, config))
}

// readPlaybooks allows reading every playbook. Writing playbooks through /db takes it too: their row-level security
// policy admits only the rows the subject may read, for every statement.
var readPlaybooks = Allow(ActionRead, AllOf[*Playbook]())

var _ = Describe("Playbooks",
	Case{
		Name: "run a playbook on a config",
		Arrange: func(f *Fixture) Attempt {
			playbook := Create(f, &Playbook{})
			config := Create(f, &Config{})
			run := Build(f, RunOf(playbook, config))
			return Attempt{
				Requests: []Request{run.CreateRequest()},
				Happened: Listed(f, "/db/playbook_runs?playbook_id=eq.%s&config_id=eq.%s", playbook.ID, config.ID),
			}
		},
		Grant: Rules(runOnConfigs(ActionPlaybookRun), readConfigs),
		NearMisses: []NearMiss{
			Miss("playbook:run on another namespace's configs",
				Allow(ActionPlaybookRun, Here[*Playbook]()).On(In[*Config](elsewhere)), readConfigs),
			Miss("playbook:run on another namespace's playbooks",
				Allow(ActionPlaybookRun, In[*Playbook](elsewhere)).On(Here[*Config]()), readConfigs),
			Miss("playbook:run without read on the config", runOnConfigs(ActionPlaybookRun)),
			Miss("read on the playbook and the config", Allow(ActionRead, Here[*Playbook]()), readConfigs),
			Miss("playbook:approve and playbook:cancel", runOnConfigs(ActionPlaybookApprove), runOnConfigs(ActionPlaybookCancel), readConfigs),
		},
	},

	Case{
		Name: "approve a playbook run",
		Arrange: func(f *Fixture) Attempt {
			run := awaitingApproval(f)
			return Attempt{
				Requests: []Request{POST("/playbook/run/approve/"+run.ID, nil)},
				Happened: Listed(f, "/db/playbook_approvals?run_id=eq.%s", run.ID),
			}
		},
		Grant: Rules(runOnConfigs(ActionPlaybookApprove), readConfigs),
		NearMisses: []NearMiss{
			Miss("playbook:run", runOnConfigs(ActionPlaybookRun), readConfigs),
			Miss("playbook:approve without read on the config", runOnConfigs(ActionPlaybookApprove)),
			Miss("playbook:approve on another namespace's playbooks",
				Allow(ActionPlaybookApprove, In[*Playbook](elsewhere)).On(Here[*Config]()), readConfigs),
		},
	},

	Case{
		Name: "cancel a playbook run",
		Arrange: func(f *Fixture) Attempt {
			run := awaitingApproval(f)
			return Attempt{
				Requests: []Request{POST("/playbook/run/cancel/"+run.ID, nil)},
				Happened: Changed(f, run, func(stored *PlaybookRun) bool { return stored.Status == "cancelled" }),
			}
		},
		Grant: Rules(runOnConfigs(ActionPlaybookCancel), readConfigs),
		NearMisses: []NearMiss{
			Miss("playbook:run", runOnConfigs(ActionPlaybookRun), readConfigs),
			Miss("playbook:cancel without read on the config", runOnConfigs(ActionPlaybookCancel)),
		},
	},

	Case{
		Name: "read a playbook",
		Arrange: func(f *Fixture) Attempt {
			playbook := Create(f, &Playbook{})
			return Attempt{
				Requests: []Request{
					GET(playbook.Path()),
					GET("/playbook/list"),
				},
				Shows: mentions(playbook.ID),
			}
		},
		Grant: Rules(Allow(ActionRead, Here[*Playbook]())),
		NearMisses: []NearMiss{
			Miss("read on another namespace's playbooks", Allow(ActionRead, In[*Playbook](elsewhere))),
			Miss("read on every config", Allow(ActionRead, AllOf[*Config]())),
		},
	},

	Case{
		Name: "create a playbook",
		Arrange: func(f *Fixture) Attempt {
			playbook := Build(f, &Playbook{})
			f.Defer(func() { f.Admin().Do(DELETE(playbook.Path())) })
			return Attempt{
				Requests: []Request{playbook.CreateRequest()},
				Happened: Exists(f, playbook),
			}
		},
		Grant: Rules(Allow(ActionCreate, AllOf[*Playbook]()), readPlaybooks),
		NearMisses: []NearMiss{
			Miss("update and read on every playbook", Allow(ActionUpdate, AllOf[*Playbook]()), readPlaybooks),
			Miss("playbook:run and read on every playbook", Allow(ActionPlaybookRun, AllOf[*Playbook]()), readPlaybooks),
		},
	},

	Case{
		Name: "change a playbook",
		Arrange: func(f *Fixture) Attempt {
			playbook := Create(f, &Playbook{})
			return Attempt{
				Requests: []Request{PATCH(playbook.Path(), map[string]any{"description": "changed"})},
				Happened: Listed(f, "/db/playbooks?id=eq.%s&description=eq.changed", playbook.ID),
			}
		},
		Grant: Rules(Allow(ActionUpdate, AllOf[*Playbook]()), readPlaybooks),
		NearMisses: []NearMiss{
			Miss("create and read on every playbook", Allow(ActionCreate, AllOf[*Playbook]()), readPlaybooks),
			Miss("read on every playbook", readPlaybooks),
		},
	},

	Case{
		Name: "delete a playbook",
		Arrange: func(f *Fixture) Attempt {
			playbook := Create(f, &Playbook{})
			return Attempt{
				Requests: []Request{DELETE(playbook.Path())},
				Happened: Gone(f, playbook),
			}
		},
		Grant: Rules(Allow(ActionDelete, AllOf[*Playbook]()), readPlaybooks),
		NearMisses: []NearMiss{
			Miss("update and read on every playbook", Allow(ActionUpdate, AllOf[*Playbook]()), readPlaybooks),
		},
	},
)
