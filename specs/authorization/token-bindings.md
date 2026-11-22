# Token Bindings

A token binding is a RoleBinding whose subject is an access token. It isn't a kind of its own: it's stored, compiled and reported like any RoleBinding (`rolebindings.md`). What sets it apart comes from its subject: a token isn't an identity (`access-tokens.md`), so binding a Role to it limits the token instead of granting anything.

## 1. What it does

A token may do what its owner may do, and only what a rule of one of its bindings' Roles also matches (`limiting-permissions.md`, "Access tokens"). Every check made with a token is checked as its owner too, so a binding can only narrow what the owner can already do with that token. It adds nothing to the owner, to anyone else, or to the token beyond its owner.

- **A token is never a subject of its own.** Nothing reaches a token except its bindings: no other binding or team can select it. An access review or access search MUST NOT be run as a token. _Why:_ any grant of its own would let a token's limit reach beyond its bindings, and probing a token's limit would tell anyone allowed to review access what another person's token can do.

## 2. Who may write it

A person MAY write the RoleBindings of their own tokens without any grant on RoleBindings, and a subject with `service-account:manage` those of any service account's tokens (`service-accounts.md`, Section 4). These are the only ways to write a RoleBinding without a grant on RoleBindings (`overview.md`, "Access").

- **Created with the token.** A token's bindings MUST be created with the token, one per Role it's created with, in the same transaction: the token and all of its bindings are created, or none of them is (`access-tokens.md`, Section 2).
- **Added and removed while it exists.** While the token exists and hasn't expired, whoever may write its bindings MAY change the Roles it's bound to, through the API or the token's details. A change sets the token's whole list of Roles at once: bindings are added for Roles new to the list and removed for Roles left out of it, in one transaction. Nothing else MAY create or delete a token binding; the `auth token` CLI command only creates tokens. A token that has expired, or was deleted, can't be changed. _Why:_ a token's limit is chosen for it, by its owner or a service account's managers, who could always change it by creating a token with other Roles and deleting the old one. Changing it in place spares reconfiguring every client that holds the token. Widening in place also widens every copy of the token, e.g. a kubeconfig that was shared, which whoever adds the Role accepts by adding it; narrowing has no such cost.
- **Never edited.** A binding's Role and subject MUST NOT change: to move a token to another Role, a binding is added and the old one removed. _Why:_ a binding's name comes from its token and Role (Section 4), so one binding always names one Role of one token.
- **The whole list, saved at once.** A Role still in the list keeps its binding, and a list the token is already bound to changes nothing. Two changes made at the same moment don't merge: the one saved last is the token's list. _Why the whole list:_ the token's Roles are edited like any other form, reviewed and then saved, and a change either lands entirely or not at all. Two people editing one token at once is rare: a person's token has only its owner, and a service account's token only its managers.
- **A Role that's missing or invalid is accepted.** It's bound like any other, and the binding is `Ready=False` and matches nothing until the Role becomes valid (Section 4). _Why:_ refusing it would tell whoever adds it whether a Role of that name exists and is valid before it's bound, which "Any Role" below already lets them bind without reading.
- **The last one MAY be removed.** A token with no bindings left can do nothing until one is added (`access-tokens.md`, Section 2). _Why:_ that's already the state of a token whose only Role is deleted, and refusing would make its owner bind a placeholder Role just to remove the last real one.
- **In effect at once.** A change MUST apply to every request made with the token that starts after the change returns. Requests already running, including a stream such as an MCP session, finish under the limit they started with. _Why:_ the token's details show what it may do, and a removed Role that kept working until a cache expired would be a limit shown but not enforced. A token in the wrong hands is deleted, not narrowed.
- **Not as a custom resource, nor through the RoleBinding API.** Neither has a field that selects a token, so neither can write a token binding. Token bindings live in a reserved namespace (Section 4) where neither MAY write anything, so neither can change, delete or replace one either.
- **One token, the caller's own or a service account's.** A token binding MUST select exactly one token, and no other subject: one the caller owns and created, or one of a service account, written by a subject with `service-account:manage`. It MUST NOT be written or removed through a token, nor for a token of an agent or of another person. A request to change any other token, or one that doesn't exist, is refused with `404 Not Found`, without saying which. _Why the same answer:_ it mustn't reveal whether another person's token exists. _Why not through a token:_ a token could otherwise bind itself to `admin` and lift its own limit (`access-tokens.md`, Section 2, "Tokens don't manage tokens").
- **Never a deny.** A token binding MUST NOT set `deny`; one stored with it is invalid and limits nothing, so the token may do nothing. _Why:_ a token binding narrows by what it allows, and the owner's own deny bindings already apply to every request made with the token (`rolebindings.md`, Section 5.3).
- **Any Role.** A token MAY be bound to any Role, named by namespace and name, whether or not its owner may read it. _Why:_ a token binding only limits, so binding a Role takes no grant on it, as binding one to a person takes none on the person.
- **Deleted with the token.** A token's bindings MUST be deleted in the same transaction as the token, however it's deleted: by its owner, by a subject with `service-account:manage`, with its service account, or when it expires.

_Why no grant:_ writing RoleBindings is a high privilege because a binding grants: it gives a Role to people, teams and resources. A token binding grants nothing, since it can only narrow a token already capped at its owner. Requiring a grant on RoleBindings would keep anyone without that high privilege from creating a usable token, and protect nothing.

_Why not a custom resource:_ a binding from Git is applied by whoever deploys it, and one through the RoleBinding API by whoever may write RoleBindings, neither of whom owns the token. Letting them select tokens would let one person change another's credential, and widen a token its owner chose to narrow.

## 3. Where it's shown

- **With the token.** A person listing their tokens sees each token's bindings, without needing to read RoleBindings: the Role each names, who added it, and whether it's `Ready`, with the reason. The binding's validation message is shown only when the person may read its Role. _Why:_ a Role's validation message names its rules, actions and Scopes, e.g. "rule run-ops: scope platform/prod-payments not found", which a person who can't read the Role mustn't learn. Whether a binding is `Ready`, and the reason, do reveal whether a Role of that name exists and is valid; a Role's name is accepted as discoverable, its rules aren't. A subject listing a service account's tokens sees their bindings the same way (`service-accounts.md`, Section 4).
- **Not with other RoleBindings.** The Role Bindings page MUST NOT show token bindings, and the RoleBinding API MUST NOT list them unless asked to include them, e.g. `include=access-tokens`. Including them takes the same `read` on RoleBindings as listing any other binding, and shows each token's id and owner. _Why:_ a token binding grants nothing, so among the bindings that decide who has access it's noise. Including them on request keeps them auditable.
- **Changed with the token.** A token's details let whoever may write its bindings add a Role or remove one. The Role Bindings page isn't where they change, for the same reason it doesn't show them.
- **Picking Roles.** When creating a token or adding a Role to one, a person picks from the Roles they may read, and MAY type any other as `namespace/name`.

## 4. How it's stored

| Field        | Value |
| ------------ | ----- |
| `namespace`  | `mission-control-access-tokens` |
| `name`       | `token-<token id>-<hash of role namespace/name>` |
| `role`       | `<role namespace>/<role name>` |
| `subjects`   | `accessToken: <token id>`, and nothing else |
| `source`     | Where it was created, as for any binding: `UI` through the API, the UI or the `auth token` CLI command |
| `created_by` | Whoever wrote the binding: the token's owner, or for a service account's token, the subject with `service-account:manage` who issued the token or added the Role |

- **By id.** The token is selected by its id, so a token deleted and created again with the same name isn't selected. _Why:_ the binding belongs to that one credential.
- **Found by its subject.** A token's bindings are those whose `subjects.accessToken` is its id. The name only has to be unique in the namespace and short enough for a Kubernetes name: the token id and a hash of the Role's reference are both fixed length.
- **In a reserved namespace.** Token bindings are in `mission-control-access-tokens`, which belongs to Mission Control (`overview.md`, "Reserved namespace"). Nothing else can be stored there: the RoleBinding API MUST refuse writes there with `409 Conflict`, and a RoleBinding applied there through Kubernetes is marked `Ready=False` and nothing of it is stored. _Why:_ a RoleBinding needs a namespace, and a token has none of its own. A Role's namespace is someone else's to fill, where a binding of the same name, e.g. one from Git, would replace a token binding. The shipped namespace is cleared of anything Mission Control didn't ship. A reserved namespace of its own means the namespace alone says a binding is a token's, which is what the listings rely on.
- **Removed, not erased.** Removing a Role, or deleting the token, soft-deletes its bindings, so a token's past Roles can still be found. A Role removed and added again gets a new binding.
- **Like any binding otherwise.** It's validated and reported as `rolebindings.md`, Section 4 says. A binding whose Role is missing or invalid is `Ready=False` and matches nothing, so the token can do nothing through it. A binding whose token no longer exists selects nothing.

_Why a RoleBinding:_ a token's limit is a Role bound to the token, which is what a RoleBinding is. Storing it as one keeps a single way to bind a Role, compiled, reported and narrowed the same way, instead of a second list of Roles on the token.

_Why no link table or column:_ a token's bindings are only ever written while its row is locked, and deleted with it in one transaction, so a lookup by subject is enough. A foreign key would only add a guarantee for tokens deleted outside Mission Control, whose leftover bindings select nothing.

## 5. Not covered yet

- **Who removed a Role. TODO.** A removed binding records when it was removed, not by whom, so a token's history shows who widened it but not who narrowed it. Recording it needs a place for who deleted a RoleBinding, which no binding has today.
- **Bindings beyond the owner's access. TODO.** A token can be bound to a Role with rules its owner doesn't hold. Those rules do nothing today, but take effect for the token as soon as its owner gains matching grants. Nothing warns about this, when the Role is bound or later. A warning needs to tell whether a Role's rules are within the owner's grants for any resources, not only those that exist today, and that is left for a later design.
