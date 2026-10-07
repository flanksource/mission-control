# RFC: Views under Roles and RoleBindings

**Status: Decided.** Option B is chosen, and Section 6 is the design. Sections 1 to 4 are kept as the record of the problem and the criteria. Section 5 keeps every option with why it was ruled out, so the discussion isn't repeated when one is proposed again. What Section 6 decides is to be written into `scopes.md`, `roles.md`, `collection-access.md` and `design/materialised-membership.md` (Section 6.7); this RFC stays as the record of why.

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

Every option below shares this. It is confirmed as part of the decision (Section 6.1).

- **A view is a resource.** A Scope's `view` target (`scopes.md`, Section 5.3) selects views by `id`, `name` and `namespace`, and has stored membership like any other type.
- **`read` accepts View.** A Role rule with `action: read` on a Scope of views lets its subjects open those views.
- **Views get a collection-level answer** (`collection-access.md`, Section 2): whether a subject may read views at all, and which pages to show.
- **Every way of reading a view makes the same check**: the UI, the database API, MCP, applications, exports and playbook reports.
- **The stamped Scope ids, and Permissions' view selectors, are removed.** Permissions lose them under `permissions.md`'s rules: they MAY grant less, never more.

The options decide only what a reader of a view sees inside it.

## 4. What a design should meet

Not every option meets all of these. Section 6 says which of them decided it.

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

The examples restrict a `pods` view to the pods of the `monitoring` namespace, for a subject who may also read the monitoring configs.
Every option is kept, with why it was ruled out or chosen, so that the next person who proposes one finds the answer here.

### Option A: a view is a report (ruled out)

Reading a view shows everything it shows. Rows aren't filtered. To give someone the pods of one namespace, create a view for that namespace and grant it.

**Why ruled out:** Suppose Team A owns the `monitoring` namespace and Team B owns `payments`. Both teams should open the same `pods` view, but each should see only its own pods. Option A can only grant the whole view or nothing: granting the shared view exposes both namespaces. Keeping them separate would require `pods-monitoring` and `pods-payments`; ten teams with different namespaces would need ten views. A shared template could avoid copying the queries by hand, but it would still create ten separate views and grants. A single View MUST support different readers seeing different rows, so separate reports are not an acceptable substitute.

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

### Option B: rows selected by their column values (chosen)

**View rows are independently granted data.** A `view` target selects a View and can narrow access to rows by their column values. Access to the source resources neither grants nor restricts access to these rows, and reading a View row grants no access to its sources.

**Why:** A View can contain arbitrary data from several sources. An administrator may share a cost report containing S3 bucket names and costs with someone who cannot browse those buckets directly. That is a grant to the report, not a change to the bucket permissions. Views use the existing Scopes, Roles and RoleBindings, but their rows are granted on their own rather than inheriting from the sources.

**Example: pods from Prometheus.** A `pods` View in namespace `mc` gets its data entirely from Prometheus, with no config query. Its creator includes `cluster`, `namespace` and `pod` columns:

| cluster    | namespace  | pod               |
| ---------- | ---------- | ----------------- |
| production | monitoring | prometheus-0      |
| production | payments   | checkout-0        |
| staging    | monitoring | prometheus-test-0 |

This Scope selects only the first row:

```yaml
kind: Scope
metadata:
  name: team-a-pods
  namespace: mc
spec:
  targets:
    - view:
        name: pods
        namespace: mc
        columns:
          cluster: production
          namespace: monitoring
```

A Role grants `read` on this Scope, and a RoleBinding gives it to Team A's guests, who have no other View grants. They can open the shared View and see only the `production/monitoring` row. Team B's guests can use another Scope on the same View for `production/payments`. Neither team needs a config grant. The selector's `namespace: mc` identifies the View; `columns.namespace` selects its rows. Mission Control checks the published columns, not the Prometheus query.

**Who is trusted.** The creator is responsible for what the rows contain and what their columns mean. Administrators decide which rows to share through Scopes, Roles and RoleBindings. Mission Control MUST enforce those grants on every read path. If a creator labels another namespace's data as `monitoring`, or an administrator grants the whole View by mistake, the engine cannot infer their intent. Editing a shared View is therefore a trusted publishing operation: it can change what existing readers learn. Permission to execute queries with particular source credentials must also be controlled; making a View private does not make unrestricted query execution safe.

**Why chosen:** B is the only option whose rule covers every row a view can produce and whose likely mistake fails closed. A, C and D fall to requirements: one shared view for several teams, rows about several or no catalog resources, and inputs the reader can't read. Between B and E, which both grant rows on their own terms rather than through their sources, the difference is what the creator is trusted with. B trusts the creator to publish a true fact on each row, `namespace: monitoring`, which Mission Control then checks. E trusts the creator to write filtering logic that works in every query, which Mission Control can't check. A fact sits on the row where anyone can read it, and a row without it matches nothing. Logic is spread across queries and a merge, and a variable that is referenced but restricts nothing shows everything, with no way for the engine to tell. B also keeps variables out of authorization altogether (Section 6.4), reads like the rest of the Scope language, gives a team with two targets the union of its rows in one table, and shows a member everything with no value to pick. The one thing E had over B, panels computed per slice, B gets by publishing panel rows with the same columns (Section 6.3). The rules are in Section 6.

- **For:** one shared View serves different groups with different rows, using the same rules for configs, Prometheus, SQL and API results. The source queries do not need to understand the reader's permissions. A missing column hides. Nothing is computed or stored per reader.
- **Tradeoffs:**
  - Source and View grants are separate. If a team needs both catalog access and a View of the same pods, both must be granted; changing one does not automatically change the other.
  - The creator must publish suitable identity or ownership columns on rows and on panel rows, and declare which columns a Scope may select. State such as `status: failed` is never selectable (`scopes.md`, Section 4.3). A View without a suitable column can't be split along that boundary.
  - A wrong column value shows a row to the wrong reader. Mission Control can't detect it, any more than it can detect a scraper tagging a config with the wrong namespace.
  - A total over exactly the rows a reader may see, when their grant spans several groups, isn't stored anywhere. It is the sum of the panel rows they see.

### Option C: rows reference the resources they come from (ruled out)

Each row carries the ids of the resources it's about, e.g. its config. A reader sees a row when they may read every resource it references, by their existing `read` grants, through stored membership. The restriction is written once, on configs.

Who fills in the references:

- The engine, where it knows the source: a row of a config query is that config, a change belongs to its config, and a row copied from another view keeps its references.
- The view's author, where it can't: a merge, an SQL query or a Prometheus series. The author maps a column to the resource it holds, e.g. a config id.

A row with no reference is hidden from every subject whose listings are filtered.

**Why ruled out:** C can only decide a row that is about exactly one config, and most rows a view shows are not. Suppose a view lists Prometheus series, and each row names a pod, the node it runs on and the deployment it belongs to. Which config does the row reference? If it references all three, a reader needs `read` on all three, so a team that owns the pod and the deployment but not the node loses the row. If it references one of them, the author picks which, and that pick decides who sees the row with no Scope behind it. Now suppose a view shows total CPU use as a single row. There is no config to reference, so no filtered reader ever sees it. The same is true of every count, sum and cluster-wide panel, and of any row from an SQL or HTTP query that didn't come from the catalog. Views exist to show data the catalog doesn't hold, and C makes the catalog the limit of what a filtered reader may see. "A row with no reference is hidden" reads as a safe default, but for these rows it isn't a decision; it is the lack of one. The reason is not that C trusts the author to map references, since B and E trust the author just as much. The reason is that C has no answer for a row about several resources or about none, and those are most rows. A View MUST be able to show totals, and rows about several or no catalog resources, to a reader who sees only part of it. C cannot, so it is not the model.

What doesn't rescue it:

- **Reference every resource the row names.** Then every reader needs every one of them. The node example shows why that is the wrong answer, and a single-row total still has nothing to reference.
- **Let the author mark a row as "about nothing, show it".** That is a grant with no Scope behind it, which is Option A for that row. It would also be the first thing an author reaches for whenever a reference is awkward.
- **Let the author pick one of the resources.** The same thing in a smaller form: the author decides who sees the row, and no rule says whether the pick was right.
- **Use a column or a variable for the rows C can't place.** That is Option B or E for those rows and C for the rest. Two rules, and a reader can't tell from the view which one applies to a row.

- **For:** define once. A grant change applies on the next read, with no refresh. A row about several resources needs all of them readable, as relationships between configs already do. An author who forgets a reference hides rows; nothing leaks.
- **Against:**
  - Not uniform. A pods view built from a config query works with no effort; one built from Prometheus shows nothing to guests until its author maps a column. The difference comes from a rule the author can't see in the view.
  - Rows that don't concern any resource, e.g. cluster-wide metrics, can never be shown to filtered subjects.
  - Panels are computed over every row, as in Option B.

### Option D: views computed with the reader's grants (ruled out)

A view is computed once per set of grants instead of once for everyone. Readers whose listings aren't filtered share one result, as today. For a filtered reader, the view's queries run under their row-level security, so a config query returns only their configs, and an SQL query only the rows of tables they may read. Merges and panels then run over data already filtered. Readers with the same grants share a result.

**Why ruled out:** Suppose Team A may read `monitoring` pods in Mission Control, but the `pods` view gets its rows from Prometheus rather than a config query. Prometheus runs the configured query using the connection's credentials; it does not know the reader's Mission Control grants. The query may leave out namespace labels or return only a count of `42` across all namespaces, so Mission Control cannot recover Team A's rows or count from the result. Prometheus can filter by namespace when a query is written to do so, but our grants cannot automatically enforce that restriction on every arbitrary query. The same problem applies to rows returned by an external API. There is also a separate problem with requiring access to all inputs: a node view may join Kubernetes nodes with AWS costs, and its readers should be able to see their nodes' costs without permission to browse the AWS resources. Running that view with the reader's source permissions would hide the costs or require broader access than intended. A View MUST support sharing such results without requiring every source to enforce the reader's grants or the reader to have direct access to every input. This rules D out as the general View model, regardless of its computing cost.

- **For:** define once, with nothing to declare: no references, columns or targets on rows. Panels are right with no special case.
- **Against:**
  - Not uniform. Sources outside Mission Control's database, e.g. Prometheus, HTTP, or SQL through a connection to another database, can't be filtered by its grants, so they must return nothing to filtered readers.
  - Each distinct set of grants is computed and stored on its own. That's cheap with a few tenants, and expensive with many and with views whose queries are costly.
  - A grant change reaches a cached result at its next refresh, unless changing a Scope or binding discards the results it affects.

### Option E: grants fix a view's variables (ruled out)

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

**Why ruled out:** E and B both grant rows on their own terms rather than through their sources, and both trust the view's creator. The difference is what they trust the creator with, and that decided it. B trusts the creator to publish a true fact on each row, which Mission Control then checks. E trusts the creator to write filtering logic that restricts every query correctly, which Mission Control can't check. The two fail in opposite directions. Under B a row without the grant's column matches nothing and is hidden. Under E a query that forgets `$namespace`, or uses it where it restricts nothing, returns everything, and nothing in the engine can tell. The creator's likely mistake is survivable under B and a leak under E. E also makes every variable part of authorization: the same value is the filter and the grant, so a variable added for convenience becomes a way to widen access, and E needs three extra rules to stay tight: a pinned variable the view no longer declares selects nothing, a pin overrides the view's default, and a query that doesn't reference a pinned variable returns nothing for that reader. Under B a variable only chooses which variant is computed, and the grant is checked inside every variant, so any reader may ask for any variant (Section 6.4). Three smaller reasons point the same way. A `columns` selector reads like `namespace` on a config target, selecting by the values of declared fields, while `variables` would be a new kind of thing in the Scope language. Two B targets give a team both namespaces in one table and one set of panel rows, where E gives two variants to switch between unless every query handles list values. And under E the view is parameterised for everyone, so even an admin has to pick a value unless the creator builds an "all". The earlier objection that a variable can hold state doesn't hold: a pinned value is part of the grant and changes with nothing. The one advantage E had, panels computed per slice, B gets by publishing panel rows with the same columns (Section 6.3). What E was really offering, pushing the slice into the source queries so they return less, B keeps as an optimization: variables narrow computation, and never replace the check.

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

## 6. Decision: Option B

Option B is chosen. Its rule, in one sentence:

> **A view's rows and panel rows are published data. A Scope selects them by the values of the columns the view declares selectable, and nothing else decides who sees them.**

The terms **MUST**, **MUST NOT** and **MAY** describe requirements. Where this section and Section 5 differ, this section applies.

### 6.1 Which views

Section 3 is confirmed:

- A `view` target selects views by `id`, `name` and `namespace`, with stored membership like any other type (`design/materialised-membership.md`). `membership.Supported` and the `scopes` trigger include views, and `scope-membership-writes.md`, step 2, no longer skips view targets.
- `read` accepts View (`roles.md`, Section 2). Opening a view is a resource-level check on the view (`roles.md`, Section 4.3). The collection-level answer for views decides whether the view pages are shown (`collection-access.md`).
- Every way of reading a view MUST make the same checks: the UI, `/db`, MCP, applications, exports and playbook reports. Today `/view/list`, `/view/metadata/:id`, `/dashboard`, application sections and the playbook `report` action check only the whole-type object or nothing; they get the per-view check and the row check. The report action reads the view as the playbook, which is a subject (`rolebindings.md`, Section 2.5).
- The stamped `__grants` column, `check_view_grants`, the `scopes` claim and Permissions' per-view selectors are removed. `object.views` with `name: "*"` keeps mapping to the whole-type object, since the built-in `viewer` policy is written that way.

### 6.2 Rows

A `view` target MAY carry `columns`, a map from column name to one exact value:

```yaml
targets:
  - view:
      name: pods
      namespace: mc
      columns:
        cluster: production
        namespace: monitoring
```

- A row matches the target when the target's view selector matches the row's view and, for every entry in `columns`, the row has that column with exactly that value. Conditions in one target combine with AND, targets combine with OR, and a RoleBinding constraint narrows as `rolebindings.md`, Section 3.2 says, with both Scopes' column values applying to the same row.
- `columns` MAY be combined with any view selector, `name: "*"` included. `name: "*"` with `columns` isn't a whole-type target (`scopes.md`, Section 5.2): it grants some rows of every view, and the collection-level answer for views is `some`.
- A target without `columns` grants every row of the views it selects. That is the whole-view grant, written on purpose. A failed column match never turns into it.
- A column value is one exact value, matched case-sensitively, with no wildcard, list or exclusion. To select every value of a column, omit the column. One target per value, as for names.
- A view declares which of its columns a Scope may select, with `selectable: true` on the column definition. A target naming a column the view doesn't declare selectable matches nothing, and so does a row that lacks the column or holds a null in it. Renaming or removing a column shrinks grants and never widens them.
- A selectable column MUST hold identity or ownership, never state (`scopes.md`, Section 4.3): a namespace, a cluster, a tenant, a name, an owner. A column such as `status` or `health` MUST NOT be declared selectable. Mission Control can't check this. The creator is responsible for it, as a scraper is for a config's tags.
- The rows are the view's published rows: the columns the view defines, after its merge and mapping. Raw query results are never granted.

**Why exact values.** A column's meaning is set by the creator, not by Mission Control, so the only safe comparison is equality against a value the Scope author wrote. Prefixes and tag maps exist for names and tags because Mission Control knows what those fields hold.

### 6.3 Panels

A panel's result is rows. Panel rows are published like table rows, and the same column grant decides which of them a reader sees.

- A panel row matches a target by the rule of Section 6.2: every entry in `columns` is on the panel row with the same value. Whatever a panel's query reads, its output rows carry the selectable columns they should be granted by, or readers with a column grant don't see them.
- So a panel grouped by the selectable columns, e.g. `SELECT namespace, sum(cpu) AS value FROM pods GROUP BY namespace`, shows each reader their groups and nobody else's. A panel that isn't grouped, e.g. `SELECT sum(cpu) AS value`, has no `namespace` on its row, so a reader with a column grant doesn't see it and a reader with a whole-view grant does. To show a total to readers who see part of the view, the creator groups it.
- A panel with no data rows, such as static text, is part of the view's definition and is shown to anyone who may open the view.
- A total over exactly the rows a reader may see, when their grant spans several groups, is the sum of the panel rows they see. The UI computes it for number and gauge panels. Nothing stored holds a per-reader total, because nothing stored knows the reader.
- Panels are never withheld as a whole from a reader. They are filtered row by row, like the table.

**Why.** A total computed over all rows can't be split afterwards, and recomputing panels per reader would make panels the one thing in the design computed per set of grants. Publishing panel rows with their columns makes rows and panels one case.

### 6.4 Variables

Variables are filtering and computation. They are never permission.

- A variable chooses which variant of the view is computed. `namespace=monitoring` and `namespace=payments` are different computations with their own rows and panel rows, keyed by the variables' fingerprint, as today.
- Any reader who may open a view MAY request any variant. Nothing checks the values. The column grant is checked inside every variant the same way, so a reader asking for a variant whose rows their grants don't select gets an empty table and no panel rows. That is a result, not an error (`collection-access.md`, Section 3).
- A view MUST NOT rely on a variable to restrict what a reader sees. Variables narrow computation: a per-namespace variant is cheaper to compute, and every row of it passes a grant for that namespace. They never replace the check.
- A variable's options stay a listing filtered by the reader's grants, as today. That is convenience in the UI, not authorization.

**Why.** Keeping variables out of authorization means a creator may add variables freely and an admin never has to read a query to know what a grant allows. It is the separation that ruled out E.

### 6.5 Enforcement, storage and caching

- Rows and panel rows are computed once per variant and stored once, shared by every reader. Nothing is computed or stored per reader or per set of grants.
- The view's own membership, which views a Scope selects, is stored in `scope_members` like every other type. Rows and panel rows have no stored membership. Their check is made when they are read: a row is readable when some `view` target in the reader's `read` grants, narrowed by constraints, selects the view and has every one of its `columns` on the row with the same value. The reader's claim carries, per view, the column maps of those targets, with an empty map for a whole-view grant. The policy on each generated table, and on the panel rows, is one containment check of the row's selectable columns against those maps. The same check serves `/db`, the view API, MCP, applications, exports and reports.
- **Why rows aren't materialised.** `materialised-membership.md` stores membership so that one evaluator serves both the single read and the listing, and so that a listing's cost doesn't grow with the table. Neither reason applies here. The check on a row is equality of a few values with no grammar, so there is nothing for two evaluators to disagree about, and view tables are small and read in pages by variant. Storing membership per generated row would rewrite it on every refresh of every variant, for no gain.
- A Scope, Role or RoleBinding change takes effect on the next read, since the check reads current targets (`scopes.md`, Section 7.1). A change to a row reaches readers at the view's next refresh, as it does for every reader.
- Panel rows are stored as rows, each with its panel's name, its selectable column values and its data, not as one blob per variant, so the same policy applies to them.
- Each generated table's primary key MUST include the variant, so that two variants producing the same key don't overwrite each other. Variants nobody has read for a period are dropped.
- The stamped `__grants` column and `check_view_grants` are removed, and the code that computed the stamp with them.

### 6.6 What this asks of a view's creator

- Publish identity or ownership columns on the rows, and declare them selectable.
- Carry the same columns on the rows of every panel a reader with a column grant should see, by grouping.
- Use variables for what readers choose, never for what readers may see.

Editing a shared view is a trusted publishing operation (Section 5, "Who is trusted"). Who may edit views, and whether that set should narrow, is a follow-up (Section 9).

### 6.7 What changes in the other specs

- `scopes.md`: Section 3.1 and 5.3, the `view` type takes `id`, `name`, `namespace` and `columns`; Section 5.1, `columns` and its exact-value rule; Section 5.2, `name: "*"` with `columns` isn't whole-type; Section 7.1, view membership is stored, and rows are checked at read as Section 6.5 says.
- `roles.md`: Section 2, `read` accepts View; Section 4.3, "Open view V" is `read` on V, and reading its rows and panel rows is the column check of Section 6.5. `mcp:run` on views gets a contract row later (Section 9).
- `collection-access.md`: Section 5, the view pages take the View type's answer; `name: "*"` with `columns` gives `some`.
- `design/materialised-membership.md`: "Not covered" loses Views; the FAQ on child tables gains the generated tables and panel rows with the containment policy; the view's own membership is stored like every type.
- `design/scope-membership-writes.md`: step 2 converts view targets instead of skipping them, with `columns` kept on the target row for the read check.
- `overview.md`: "Not covered yet" loses Views.
- `permissions.md`: Permissions' per-view selectors and `object.scopes` grants on view rows lose effect; the whole-type `object.views` stays.
- The View spec and `views/AGENTS.md`: `selectable` on columns, panel rows carrying selectable columns, and the rule that variables never restrict.

## 7. Comparison

Kept as the record of the choice. B's row is as decided in Section 6; the others are as they were weighed.

|                             | A: report | B: columns                         | C: references | D: reader's grants | E: variables |
| --------------------------- | --------- | ---------------------------------- | ------------- | ------------------ | ------------ |
| Define once                 | No        | No                                 | Yes           | Yes                | No           |
| Uniform across sources      | Yes       | Yes                                | No            | No                 | Yes          |
| No hidden rules             | Yes       | Yes                                | No            | Partly             | Yes          |
| Fails closed                | n/a       | A missing column hides; a false value shows | Yes  | Yes                | Needs care   |
| Panels are right            | Yes       | Yes, as rows (Section 6.3)         | No            | Yes                | Yes          |
| Takes effect when saved     | Yes       | Yes                                | Yes           | At refresh         | Yes          |
| One view serves many slices | No        | Yes                                | Yes           | Yes                | Yes          |
| Extra computing             | None      | None                               | None          | Per set of grants  | None         |

"Needs care" means a mistake by the author shows more, not less: a variable the query doesn't honour.

The decision takes B alone. E's mechanism survives only as an optimization: variables narrow computation and never replace the check (Section 6.4). No option is combined with another.

## 8. Questions for reviewers, answered

1. **Section 3.** Confirmed, as Section 6.1.
2. **Section 4.1.** Neither decided it. What decided it was two other things: whether the creator's likely mistake fails closed, and whether filtering and authorization stay separate (Section 5, "Why chosen"; Section 6.4). B gives up "define once" on purpose: a view's rows are granted on their own terms, not through their sources.
3. **Readers who see part of a view.** Guests, users of an external identity provider, and anyone behind `X-Flanksource-Scope`. How many distinct sets of grants there are no longer matters: nothing is computed or stored per reader (Section 6.5).
4. **Data from outside.** Yes, and B shows it to them. A row from Prometheus, SQL or HTTP is granted by its columns like any other, and so are the panel rows over it (Sections 6.2, 6.3).
5. **Authors.** Yes. Editing a shared view is a trusted publishing operation (Section 5, "Who is trusted"). The creator publishes facts, and the only mistake that shows more is a false fact, which is the same trust the system places in a scraper's tags. Who may edit views today, and whether that set should narrow, is a follow-up (Section 9).
6. **The view and the catalog disagreeing.** Accepted. A reader may see a pod in a view they can't open in the catalog, or the reverse, because view rows are independently granted data (Section 5, Option B).
7. **Another option.** No. The tension in Section 4.1 stands, and B picks "uniform across sources" and gives up "define once".

## 9. Not covered

- Writing views (`create`, `update`, `delete`) and `mcp:run` on views. They're whole-view actions and don't depend on the choice here; they get contract rows in `roles.md`, Section 2, afterwards.
- Applications, whose sections are views. They follow Section 6.
- Who may edit views, given that editing a shared view changes what its readers see (Section 6.6).
- A per-reader total across several groups, if summing visible panel rows in the UI turns out not to be enough (Section 6.3).
- Whether `columns` should ever take anything but one exact value. Not now (Section 6.2).
