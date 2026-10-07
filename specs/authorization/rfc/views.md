# RFC: Views under Roles and RoleBindings

**Status: Open for discussion.** This RFC chooses no design. It sets out the problem, what any design must meet, and the options considered so far, so reviewers can weigh them, argue for one, or propose another. Once one is chosen, it's written into `scopes.md`, `roles.md` and `collection-access.md`, and this RFC is closed.

## 1. Problem

Views are the one resource left outside Scopes, Roles and RoleBindings (`overview.md`, "Not covered yet"). No rule can select a View, and Roles grant no rows of the tables Views generate.

Views have authorization of their own instead, built before Roles existed:

- **Which views a subject may open** is decided by Permissions, through `object.views` or a Scope's `view` targets, and only on some of the ways a view can be read.
- **Which rows of a view a subject may see** is decided when the view is computed. Each row produced by a config query is stamped with the ids of the Scopes whose config targets match it. A subject sees a row when one of those Scopes is granted to them through a Permission's `object.scopes`.

It doesn't fit the rest of the system:

- RoleBindings grant no rows. A guest who may read the monitoring configs through a Role sees no row of a view listing them.
- Rows from anything but a config query (changes, SQL, Prometheus, merged queries) carry no stamp. Guests never see them; everyone else always does.
- Panels are computed over every row, so a subject who sees some rows sees totals over all of them.
- A stamp is taken when the view is computed. A Scope change reaches a view's rows at its next refresh, not when the Scope is saved (`scopes.md`, Section 7.1).
- Scope targets are evaluated a second time, outside the stored membership (`design/materialised-membership.md`).

This RFC asks how Views should fit Roles and RoleBindings instead.

## 2. What makes Views different

Every other resource answers one question: may this subject read _this_ resource? A View raises two:

1. **Which views** may the subject open? Of ten views, which ones are listed and can be opened.
2. **Which rows** of a view may they see? A `pods` view may list every pod, while a subject should only see the pods of one namespace.

The second question is the hard one, for three reasons:

- **A row is arbitrary data.** It's whatever the view's queries and mappings produce. Nothing requires a row to correspond to a config, or to any resource: it can be a Prometheus series, a row of an SQL query, or a join of several queries.
- **Rows are computed once and shared.** A view's results are stored in a table generated for it, and served to every reader who requests the same variables. Its queries don't run as the reader.
- **Panels aggregate rows.** A count or a sum over a view is only right for a reader if it's computed over the rows that reader may see.

The first question is an ordinary resource question. The second is where the options differ.

## 3. Common ground

Every option below shares this, so it's proposed regardless of which is chosen. Reviewers are asked to confirm it separately.

- **A view is a resource.** A Scope's `view` target (`scopes.md`, Section 5.3) selects views by `id`, `name` and `namespace`, and has stored membership like any other type.
- **`read` accepts View.** A Role rule with `action: read` on a Scope of views lets its subjects open those views.
- **Views get a collection-level answer** (`collection-access.md`, Section 2): whether a subject may read views at all, and which pages to show.
- **Every way of reading a view makes the same check**: the UI, the database API, MCP, applications, exports and playbook reports.
- **The stamped Scope ids, and Permissions' view selectors, are removed.** Permissions lose them under `permissions.md`'s rules: they MAY grant less, never more.

The options decide only what a reader of a view sees inside it.

## 4. What a design should meet

Not every option meets all of these. Reviewers are asked which matter most.

| Criterion                    | Meaning                                                                                                      |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------ |
| **Define once**              | Granting "the monitoring configs" also restricts every view that shows them, with nothing more to write.     |
| **Uniform across sources**   | A view behaves the same whether its rows come from configs, changes, SQL or Prometheus.                      |
| **No hidden rules**          | What a reader sees follows from what's written in the Scope, the Role and the view, not from implicit fields. |
| **Fails closed**             | A mistake by a view's author hides rows; it never shows rows a reader shouldn't see.                         |
| **Panels are right**         | A reader never sees a total over rows they can't see.                                                        |
| **Takes effect when saved**  | A Scope or binding change applies like everywhere else (`scopes.md`, Section 7.1), not at the next refresh.  |
| **Identity, not state**      | Rows are granted by what they are, never by their current state (`scopes.md`, Section 4.3).                  |
| **Cost**                     | How often a view is computed, and how much is stored.                                                        |

### 4.1 A tension

**Define once** and **uniform across sources** pull against each other. Only resources carry grants. For a view to follow a config grant without the grant being restated, each row has to be traced back to a config, and whether that's possible depends on where the row came from: a config query can be traced, a Prometheus series can't. So a design that inherits resource grants behaves differently by source, and a design that behaves the same for every source has to state the view's restriction in the view's own terms.

No option below escapes this. Each picks a side, or a balance.

## 5. Options

**Options A and D are ruled out.** A needs a separate view for each team's namespace, but we need one shared view that shows each team only its own rows. D cannot apply our permissions to arbitrary Prometheus queries or external API calls. It would also hide useful data, such as a node's AWS cost, from readers who may read the node but not the AWS resources.

The examples restrict a `pods` view to the pods of the `monitoring` namespace, for a subject who may also read the monitoring configs.

### Option A: a view is a report

Reading a view shows everything it shows. Rows aren't filtered. To give someone the pods of one namespace, create a view for that namespace and grant it.

```yaml
kind: Scope
metadata:
  name: monitoring
spec:
  targets:
    - config:
        tagSelector: namespace=monitoring
    - view:
        name: pods-monitoring
```

- **For:** one rule, the same for every source. Panels are right. Nothing is computed per reader.
- **Against:** one view per slice, e.g. per namespace or tenant. Granting a view grants its contents, whatever the reader's other grants: whoever writes the view decides what its readers see.

### Option B: rows selected by their column values

A `view` target can select rows of a view by the values of its columns, e.g. the rows whose `namespace` column is `monitoring`. The generated table filters rows by the reader's Scopes.

```yaml
kind: Scope
metadata:
  name: monitoring
spec:
  targets:
    - config:
        tagSelector: namespace=monitoring
    - view:
        name: pods
        columns:
          namespace: monitoring
```

- **For:** the same for every source, since every row has columns. One view serves every slice. A view becomes a type of its own, and its rows its resources, so the two questions of Section 2 become the one `collection-access.md` already answers: `all` rows, `some`, or `none`.
- **Against:**
  - The restriction is stated twice, once for configs and once for the view, though in one Scope. A view that has no `namespace` column can't be restricted by namespace, and granting it whole shows everything.
  - Whoever writes the view decides what a column means. A query that puts production pods under `namespace: monitoring` shows them to monitoring readers.
  - Columns are arbitrary, so nothing stops a target on a column holding state, e.g. `status: failed` (`scopes.md`, Section 4.3). The view would have to declare which columns may be used.
  - A renamed or removed column must make the Scope select nothing, never everything.
  - Panels are computed over every row, and need either computing per reader or hiding from readers with `some`.

### Option C: rows reference the resources they come from

Each row carries the ids of the resources it's about, e.g. its config. A reader sees a row when they may read every resource it references, by their existing `read` grants, through stored membership. The restriction is written once, on configs.

Who fills in the references:

- The engine, where it knows the source: a row of a config query is that config, a change belongs to its config, and a row copied from another view keeps its references.
- The view's author, where it can't: a merge, an SQL query or a Prometheus series. The author maps a column to the resource it holds, e.g. a config id.

A row with no reference is hidden from every subject whose listings are filtered.

- **For:** define once. A grant change applies on the next read, with no refresh. A row about several resources needs all of them readable, as relationships between configs already do. An author who forgets a reference hides rows; nothing leaks.
- **Against:**
  - Not uniform. A pods view built from a config query works with no effort; one built from Prometheus shows nothing to guests until its author maps a column. The difference comes from a rule the author can't see in the view.
  - Rows that don't concern any resource, e.g. cluster-wide metrics, can never be shown to filtered subjects.
  - Panels are computed over every row, as in Option B.

### Option D: views computed with the reader's grants

A view is computed once per set of grants instead of once for everyone. Readers whose listings aren't filtered share one result, as today. For a filtered reader, the view's queries run under their row-level security, so a config query returns only their configs, and an SQL query only the rows of tables they may read. Merges and panels then run over data already filtered. Readers with the same grants share a result.

- **For:** define once, with nothing to declare: no references, columns or targets on rows. Panels are right with no special case.
- **Against:**
  - Not uniform. Sources outside Mission Control's database, e.g. Prometheus, HTTP, or SQL through a connection to another database, can't be filtered by its grants, so they must return nothing to filtered readers.
  - Each distinct set of grants is computed and stored on its own. That's cheap with a few tenants, and expensive with many and with views whose queries are costly.
  - A grant change reaches a cached result at its next refresh, unless changing a Scope or binding discards the results it affects.

### Option E: grants fix a view's variables

A `view` target can fix the values of a view's variables. A reader may only request a view with values their grants allow; with several targets, they choose among them. A view's results are already computed and stored per set of variable values, so enforcing it is a check on the request, before anything is computed or read.

```yaml
kind: Scope
metadata:
  name: monitoring
spec:
  targets:
    - config:
        tagSelector: namespace=monitoring
    - view:
        name: pods
        variables:
          namespace: monitoring
```

- **For:**
  - The same for every source: a Prometheus query uses `$namespace` exactly as a config query does.
  - Variables are declared on purpose, and shown in the UI. Nothing is implicit.
  - Panels are right, since they're computed per set of values. Results are shared by everyone requesting the same values, as today.
  - The generated tables need no row filter at all.
- **Against:**
  - The restriction is stated twice, though in one Scope, as in Option B.
  - Whoever writes the view decides what a variable means. A query that ignores `$namespace` grants every pod when it's fixed.
  - A variable can hold state, as a column can; the view would declare which variables a grant may fix.
  - A view can only be sliced by the variables it has. One without a suitable variable is granted whole or not at all.

## 6. Comparison

|                             | A: report | B: columns | C: references | D: reader's grants | E: variables |
| --------------------------- | --------- | ---------- | ------------- | ------------------ | ------------ |
| Define once                 | No        | No         | Yes           | Yes                | No           |
| Uniform across sources      | Yes       | Yes        | No            | No                 | Yes          |
| No hidden rules             | Yes       | Yes        | No            | Partly             | Yes          |
| Fails closed                | n/a       | Needs care | Yes           | Yes                | Needs care   |
| Panels are right            | Yes       | No         | No            | Yes                | Yes          |
| Takes effect when saved     | Yes       | Yes        | Yes           | At refresh         | Yes          |
| One view serves many slices | No        | Yes        | Yes           | Yes                | Yes          |
| Extra computing             | None      | None       | None          | Per set of grants  | None         |

"Needs care" means a mistake by the author shows more, not less: a column or variable the query doesn't honour.

The options aren't exclusive. For example, E for views whose data comes from outside Mission Control and C for the rest, or B as a later addition to C for rows that concern no resource. Each combination brings back some of what uniformity was meant to remove, and is worth proposing only with that in view.

## 7. Questions for reviewers

1. **Section 3.** Is the common ground right, whatever option is chosen?
2. **Section 4.1.** Which matters more: a restriction written once and followed by every view, or one rule that holds for every source?
3. **Readers who see part of a view.** Who are they in practice: guests, users of an external identity provider, tenants? How many distinct sets of grants does a large installation have? That decides whether Option D's cost matters.
4. **Data from outside.** Do readers who see part of a view need data from outside Mission Control's database, e.g. Prometheus panels per tenant? Options C and D can't show it to them.
5. **Authors.** Is it acceptable that editing a view is permission administration, as it is in Options A, B and E? Who may edit views today, and is that the right set of people for it?
6. **The view and the catalog disagreeing.** In A, B and E, a reader can see a pod in a view that they can't open in the catalog, or the reverse. Is that acceptable, if the Scope says so?
7. **Another option.** Is there a design that is both define-once and uniform across sources, which Section 4.1 says is impossible?

## 8. Not covered

- Writing views (`create`, `update`, `delete`) and `mcp:run` on views. They're whole-view actions and don't depend on the choice here; they can be added to the action contracts (`roles.md`, Section 2) afterwards.
- Applications, whose sections are views. They follow whatever is chosen.
