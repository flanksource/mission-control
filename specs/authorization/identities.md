# Who a check is made as

Every check is made as a subject: the caller of the request, or, where there's none, the resource doing the work. What kind of subject it is only decides which RoleBinding subjects can select it (`rolebindings.md`, Section 2), never what it may do (`overview.md`, "Access").

## 1. The caller

A request is checked as whoever made it: a person, a user of an external identity provider (`external-identity-providers.md`), or an agent (Section 5). A request made with an access token is checked as the token's owner (Section 4).

## 2. Work without a caller

Some work isn't done by the person who started it: a playbook running on its schedule, from a webhook or from an event it's triggered by, a notification being sent, a playbook started by a notification or by another playbook. Mission Control has no identity of its own to run it as (`overview.md`, "Access"). It's checked as the resource doing it, which a RoleBinding selects like any subject (`rolebindings.md`, Section 2.5):

| Work                                              | Starting it is checked as             | Checks made during it are made as |
| ------------------------------------------------- | ------------------------------------- | --------------------------------- |
| A person runs playbook P                          | The person                            | P                                 |
| P runs on its schedule, from a webhook, or from an event it's triggered by | P: `playbook:run` on P, then `read` on the target | P |
| Notification N runs playbook P                    | N                                     | P                                 |
| Playbook Q's run starts playbook P                | Q                                     | P                                 |
| N is evaluated against an event                   | Not checked                           | Not checked                       |
| N is sent                                         | Not checked: no one asks for the send | N                                 |
| A person invokes operation O of plugin X on config C | The person: `invoke:X:O`, then `read` on C | The person, except connection lookups: X |

- A run no person started is created only if the playbook passes the checks a person starting it would (`roles.md`, Section 4.3): `playbook:run` on itself, with the run's target if it has one, then `read` on the target. A playbook refused either check gets no run, and the refusal is recorded in its job history. _Why:_ starting a run makes the same checks whoever starts it, with no exception for the playbook. The `read` check also matters on its own: a run's templates are filled with its target's data before any step runs, so without it, a playbook could be triggered on configs it may not read and send their data anywhere.
- A run's actions are checked as its playbook, whoever started it. A playbook with no binding can do nothing that's checked: it can't read a config, use a connection or run another playbook.
- An action a run assigns to an agent runs there without being checked against the agent's own RoleBindings, e.g. for `connection:use` on the agent's connections, and the agent needn't know the playbook. The run was authorized here when it started, and the agent only fetches the actions assigned to it, by its own identity (Section 5). _Why:_ the playbook and its bindings live here, not on the agent, so no binding on the agent could select it, and the agent's token already proves the action is its own.
- So whoever may run P may have P act with P's grants, through the parameters they pass too, e.g. a parameter naming the connection or config a step acts on. `playbook:run` on P is as sensitive as P's own bindings: grant it, and write P's parameters, with that in mind.
- A webhook authenticates the request with the playbook's own webhook settings. The webhook isn't a subject, and grants nothing.
- Evaluating a notification matches an event against the notification's events, filter and silences, and queues what to send. It makes no check, and MUST NOT fill in a connection's credentials. It records the connection to send through by id only.
- **TODO:** a notification isn't checked for `read` on the resource it reports on, although its templates are filled with that resource's data and sent through its connection. A playbook can't do the same: a run checks `read` on its target before its templates are filled (`roles.md`, Section 4.3). Accepted for now, and to be revisited. Recipients that aren't connections, e.g. a person or team emailed through Mission Control's own SMTP settings, aren't covered either: no check is made for them.
- Sending checks `connection:use` on that connection as the notification. If the check fails, the send fails, and the failure is recorded on the notification's send history and status. It's never retried as another subject.
- No one's access is checked when a notification or playbook is created or changed against the connections and resources it names. What it may do comes only from the bindings that select it. A notification created from a custom resource has no author to check, so an author check would treat the same notification differently depending on where it was written.

_Why the resource and not its author:_ a resource's author can change, and resources come from Git, the UI and other tools. Granting the resource itself makes what it may do one question, answered by its bindings, whoever wrote it last. Who can change a resource with a grant is covered by `rolebindings.md`, Section 2.5.

## 3. Plugins

Unlike the work in Section 2, a plugin's work always has a caller, and the plugin acts for them, except with connections.

- Every check a plugin's work makes is made as the caller, with the caller's grants: reading a config, listing configs, and starting another plugin's operation (`invoke:<plugin>:<operation>`, then `read` on the config). If the caller used an access token, its Roles limit these checks (Section 4).
- Looking up a connection is the one exception. It's checked as the plugin, selected by `plugins` in a RoleBinding (`rolebindings.md`, Section 2.5), by whatever path the plugin resolves it: `read` lets it see a connection's name, type and settings, and `connection:use` lets it act with the credentials (`roles.md`, Section 2.2). The caller's access to the connection is never checked: a caller needs no grant on the plugin's connections, and their grants give the plugin none. A plugin without a grant that matches is refused, and the call fails. It's never retried as the caller or as any other subject.
- A plugin can only be granted two actions, `read` and `connection:use`, on Scopes that select connections and nothing else. A RoleBinding that selects a plugin and binds a Role with any other rule, or with a rule whose Scope selects another type, is `Ready=False` and grants nothing, to any of its subjects, until the Role or the binding is changed. _Why:_ connection lookups are the only checks made as the plugin, so no other grant could take effect.
- So whoever may invoke operation O of plugin X may have X act with X's connections, on any config they may read that X's selector matches. `invoke:X:O` is as sensitive as what O does with them: grant it with that in mind.
- Plugins are upgraded by Mission Control itself, on a schedule. No person or Role can trigger an upgrade.

  ```yaml
  kind: Role
  metadata:
    name: logs-plugin-connections
    namespace: mission-control
  spec:
    rules:
      - name: use-loki
        action: connection:use
        resource:
          scopeRef: loki-connections
  ---
  kind: RoleBinding
  metadata:
    name: kubernetes-logs-plugin
    namespace: mission-control
  spec:
    role: logs-plugin-connections
    subjects:
      plugins:
        - namespace: mission-control
          name: kubernetes-logs
  ```

  The `kubernetes-logs` plugin can use the connections in `loki-connections`, whoever invokes it, and no others. A person with `invoke:kubernetes-logs:get-pod-logs` and `read` on a pod gets that pod's logs through them, without any grant on the connections.

_Why the caller:_ a plugin does work someone asked for, so it reads with their access, and no one reaches more data through a plugin than they could directly. _Why connections are the plugin's:_ a plugin often needs credentials its callers shouldn't hold, e.g. a cluster's credentials to fetch one pod's logs, and its code, not the caller, decides what it does with them. Borrowing the caller's access would let anyone allowed to invoke an operation have the plugin act with any credentials they can use. Granting connections to the plugin keeps them readable from its own bindings, and the same whoever invokes it.

## 4. Access tokens

An access token is a credential, not a subject. A request made with a person's token is checked as that person, and RoleBindings select the person, never the token. Actions taken with it are recorded as the person's. What a token may do is limited by its Roles (`limiting-permissions.md`, "Access tokens").

- **Roles are required.** A person's token MUST list at least one Role, however it's created: through the UI, the API or the `auth token` CLI command. A token meant to do everything its owner may lists the shipped `admin` and `plugin-admin` Roles. An agent's token MUST NOT list any (Section 5). _Why:_ what a token may do is chosen when it's created, never everything by default.
- **Tokens without Roles.** A person's token that lists no Role, e.g. one created before Roles were required, MUST be refused with `401 Unauthorized`. Mission Control deletes such tokens when it starts, and logs each one with its name and owner. Agents' tokens are kept. _Why break them:_ they can't be limited, and the scopes some of them had before Roles can't be told apart from no limit. Agents, the one use that can't afford the break, aren't affected.
- **Tokens don't manage tokens.** A request made with a token can't create, change or delete a token, whatever its Roles. _Why:_ otherwise a limited token could create one without its limit.

A person may create, change and delete their own tokens, without any grant, but not through a token. No person can create, change or delete a token for another person, and no action grants it.

An agent's token is different: it's created with the agent, by a subject with `create` on `agent`, and handed to them (Section 5). Rotating or revoking an agent's token takes `update` on `agent`. An agent's token lists no Roles.

_Why no grant:_ a token adds no access its owner doesn't already have, so there's nothing for a Role to decide.

## 5. Agents

An agent is a Mission Control instance in another cluster that sends what it scrapes and checks to this one. Every request it makes carries an access token issued to that agent. The agent a request acts as MUST be the one its token belongs to: a request that names another agent, or a token that belongs to no agent, is refused. A request never creates an agent.

An agent may, without any Role or RoleBinding:

- push its own data, e.g. configs, changes, components, checks and their statuses, job history and artifacts, and delete data it pushed before;
- fetch what's assigned to it: its canaries, scrape configs and playbook actions, and report the results of those actions;
- keep its connection: ping, and open the tunnel this instance uses to reach it;
- read the column definitions of any view, never its rows;
- register the plugins it runs. It MUST NOT replace a plugin registered by anyone else.

An agent may do nothing else, e.g. read configs other agents pushed or list playbooks. No RoleBinding can select an agent (`rolebindings.md`, Section 2.3), so nothing grants it more.

_Why no Role:_ everything above acts on the agent's own data, and the token already proves which agent is asking. A rule would be the same for every agent, so it would grant nothing a reader of a Role couldn't assume. Creating the agent is the grant: only a subject with `create` on `agent` can, and only they receive its token. Kubernetes authorizes a kubelet the same way, by the node its credential names rather than by RBAC.

An agent sets the tags and labels of what it pushes. A Scope that selects by them, e.g. `tagSelector: env=prod`, includes an agent's resources when the agent tags them so, and grants on that Scope reach them. To keep an agent's resources out of a Scope, select by `agent` too (`scopes.md`, Section 5.4).
