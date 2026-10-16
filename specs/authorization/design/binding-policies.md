# Binding Policies

## Background

A RoleBinding's rules are its Role's rules, narrowed by the binding's constraint if it has one (`rolebindings.md`, Section 3). They can be compiled into casbin policies once per binding, or once per Role and shared. We compile them once per Role, and share them between every binding without a constraint; only a binding with a constraint gets policies of its own.

## Design

- **Each valid Role** has its rules compiled once, under the principal `role:<namespace>/<name>`.
- **A binding without a constraint** is one grouping of its principal, `binding:<namespace>/<name>`, into its Role's principal. Its subjects are grouped into the binding, as for every binding, so a check follows subject → binding → Role.
- **A binding with a constraint** has the Role's rules compiled again under its own principal, each narrowed by the constraint, since its rules differ from the Role's.
- **An access token's binding** has no constraint, so it's one grouping of `token:<id>` into its Role's principal (`design/tokens.md`).
- **A missing or invalid Role** has no compiled rules, so a binding to it matches nothing, as its `Ready=False` status says.
- **The `role:` principal** is never the subject of a request, gets no implicit grants, and is refused as the subject of an access review. A stored casbin row for it is ignored when the policy loads.

Whatever reports which binding allowed a check MUST follow the groupings from the matched principal back to the subject's bindings, rather than read a binding's own policies: a binding without a constraint has none.

## Alternatives

- **Compile every binding on its own.** Policies grow with the number of bindings rather than Roles, and casbin walks every policy on each check, so each binding of a large Role, e.g. `admin`, makes every check slower. Access tokens, one binding per Role each, would make it grow with every token.
- **Group every binding into its Role, constraints included.** A constraint narrows the Role's rules, so its policies differ from the Role's and can't be shared.
