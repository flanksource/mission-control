# Row-filtered Subjects Specification

This specifies the two kinds of subject Mission Control authorizes, what each has without a grant, and how built-in access reaches them. It resolves the open questions on guests in [roles.md, Section 7.4](roles.md#74-open-questions).

The terms **MUST**, **MUST NOT** and **MAY** describe requirements.

## 1. Two kinds of subject

Every subject is one of:

- **A member.** A Mission Control user with a member built-in role: `viewer`, `editor`, `commander`, `responder` or `admin` ([roles.md, Section 7.2](roles.md#72-members)). Members see every resource of the types their roles let them read. Their listings are never filtered by row.
- **A row-filtered subject.** Everyone else: guests ([roles.md, Section 7.3](roles.md#73-guests)), users of an external identity provider ([external-identity-providers.md, Section 6](external-identity-providers.md#6-access)), and resources acting on their own ([rolebindings.md, Section 2.5](rolebindings.md#25-resources)). A row-filtered subject sees exactly what their grants select, and nothing else.

Which kind a subject is follows from how they signed in and the built-in role they hold, never from a Role or RoleBinding. A RoleBinding can grant a row-filtered subject every resource of a type, through a whole-type Scope, but can't make them a member.

Agents hold the built-in `agent` role, which is neither a member role nor restricted: they have what the `agent` role grants, unfiltered, and nothing of what's granted to `viewer`.

**Why one category.** Guests and users of an external identity provider were specified separately but want the same thing: access that's exactly what was shared. Specifying them once means a Role behaves the same whichever it's bound to, and a change to how filtering works is made once.

## 2. Built-in access

What a subject has before any Role or Permission applies:

| Subject              | Built-in read access                                                                                                                                                     |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Member               | What their member role grants ([roles.md, Section 7](roles.md#7-built-in-roles)): for a viewer, `read` on the catalog, topology, canaries, playbooks, views, people and applications as whole types |
| Row-filtered subject | None of the resource types. Reference data only ([Section 2.1](#21-reference-data))                                                                                                            |

Reads granted to the built-in `viewer` role reach members only. They MUST NOT reach a row-filtered subject, on a whole-type check or any other. This settles the question [roles.md, Section 7.4](roles.md#74-open-questions) used to ask about reads granted to `viewer`: guests aren't kept out by their listings being filtered after the fact, they're outside the grant.

### 2.1 Reference data

To use Mission Control at all, a subject has to read the data that describes kinds of resources rather than resources: resource types and classes, statuses, health values, and the keys of tags and labels, as the UI's filters show them. A row-filtered subject MAY read it without a grant. It MUST NOT include any resource or anything that identifies one, and it MUST NOT include people, views or applications.

What counts as reference data is decided in code. Adding to it adds to what every outsider sees, and is reviewed as such.

## 3. What a row-filtered subject may do

A row-filtered subject holds nothing but what Roles, through RoleBindings, and Permissions grant them. Everything below follows from [collection-access.md](collection-access.md):

- **All, some or none.** Whether they may read a type at all is derived from their grants alone ([collection-access.md, Section 2](collection-access.md#2-all-some-or-none)). With no grant on a type it's `none`: the listing is refused, the page isn't shown, and the type isn't searched.
- **Listings.** Filtered to what their grants select, while row-level security is on. While it's off, only whole-type grants apply ([roles.md, Section 3.1](roles.md#31-the-read-action)), so a listing is either every resource of a type they hold whole, or refused. This settles the question [roles.md, Section 7.4](roles.md#74-open-questions) used to ask about guests while row-level security is off: a row-filtered subject never gets an unfiltered listing of a type they don't hold whole, whatever the setting.
- **Opening one resource.** Checked against their grants, and only their grants. A resource outside them is refused as if it didn't exist. Built-in whole-type `read` never covers it, because they have none.
- **Everything else.** As the rules say. Actions other than `read` already come only from rules.

## 4. Members

Nothing changes for members. Their listings stay unfiltered, their built-in roles grant what they grant, and deny rules on `read` stay unsupported for the reason [roles.md, Section 2](roles.md#2-actions) gives: an unfiltered listing can't honour a deny.

## 5. Future work

- **Deny on `read` for row-filtered subjects.** Their listings are filtered, so a deny could be enforced on them. It stays unsupported until it can be supported for members too, so that a Role means the same thing whoever it's bound to.
- **A person with no built-in role.** Today they're treated as a viewer ([roles.md, Section 7.4](roles.md#74-open-questions)). Whether they should be a row-filtered subject instead is undecided.

## 6. Changing to this

Today a guest gets past whole-type `read` checks through built-in access, and reads people, views, applications and the public database tables unfiltered. Under this specification a guest reads only what their grants select, plus reference data. A guest with no grants sees nothing at all, instead of empty pages, and a guest who could reach something through built-in access that no grant covers loses it.
