# Health notification recovery (opt-in)

`spec.onResolved.enabled: true` adds recovery to **successful original deliveries** of ungrouped config, component and check health notifications. Omitted or disabled policies leave legacy sends unchanged. Supported destinations are native Slack Connections, named SMTP Connections, email/person recipients using system SMTP, and team rules selecting these transports (including successful fallback destinations).

```yaml
spec:
  events: [config.unhealthy, config.warning]
  waitFor: 1m                  # delay the initial alert
  onResolved:
    enabled: true
    waitFor: 30s              # continuous healthy time before recovery
    # template: 'Recovered {{.resource.id}}'
    slack:
      reply: true             # default, even when slack is omitted
      reaction: white_check_mark  # optional emoji NAME, not :name: or a bool
```

`onResolved.waitFor` defaults to zero and accepts nonnegative Go durations. `template` optionally replaces the recovery body; otherwise the body identifies the resource and reports recovery time and outage duration. Normal resource template variables, `resource`, `recoveredAt`, `resolvedAt` and `outageDuration` are available. Slack must enable a reply, a reaction, or both.

## Lifecycle and destinations

* Recovery does not require a healthy event in `spec.events` and does not reapply the original unhealthy filter, silences, inhibition or repeat limits. Its own transport/permission failures retry with backoff.
* Recovery before the initial `waitFor` send produces no alert or orphan recovery. Unknown health, deletion and leaving a filter do **not** mean healthy.
* Warning/unhealthy changes within an outage retain its identity. A genuine healthy-to-unhealthy transition starts a distinct conversation and resets repeat suppression. If health flaps during recovery stabilization, older delivered episodes remain outstanding; all resolve after the **latest** healthy transition stabilizes, in their separate conversations.
* Duty records health episodes in the source transaction. Source events carry the episode ID through queue coalescing/reordering. Pre-upgrade queued source events without this association are ignored for opted-in initial sends, rather than assigned to a newer outage.
* Slack uses the returned channel and timestamp, not a later connection channel setting. Replies are threaded; reply and reaction completion are saved separately, so retrying a failed reaction does not repost a completed reply.
* Email snapshots the actual recipients, From and original subject. Both raw and structured sends use stable original/recovery Message-IDs; recovery preserves the envelope/From, uses the original `Re:` subject and supplies `In-Reply-To`/`References`. Mail clients ultimately control threading. Do not override the reserved Message-ID, In-Reply-To, References, From, To or Subject headers.
* Once an original send intent exists, retries use that intent's transport and connection reference even if a person/team/rule changes. Credentials are resolved again, not copied into the receipt. A missing named connection never becomes system SMTP. Named connections require the notification's current real connection-read permission, including during recovery; grant only the required connections.
* **System SMTP exception:** email/person/system-SMTP delivery uses shared application mail infrastructure with its existing access model, not a new per-notification connection-read grant. This applies to the DB `system` SMTP Connection and the existing env/CLI fallback. Configure system SMTP centrally; it is not a substitute for an inaccessible explicitly named connection.

Grouped notifications, playbooks, webhooks, generic `slack://` URLs and other transports are rejected at validation and guarded again at dispatch. There is no UI/grouped-recovery support in this initial implementation.

## Operations and retained data

`NotificationRecoveries` reconciles persisted receipts on startup and every 15 seconds, with bounded batches, five-minute leases and no database transaction held over network calls. Set the property `notification.recovery.max-retries` to control recovery retries; the default is 7 retries after the initial attempt (8 attempts total). Zero disables retries, negative values are treated as zero, and invalid values use the default. Failures use exponential backoff: 2, 4, 8, 16, 32, 64 and 128 seconds for the default budget, capped at one hour when a larger budget is configured. Polling can add delay. Health deferrals do not consume attempts or reset accumulated failures. Unhealthy/unknown/deleted resources leave receipts in dormant `waiting-for-healthy`, excluded from the ready queue. Healthy receipts use `stabilizing` with `not_before = healthy_since +` the saved policy's parsed `waitFor`, never earlier than an existing failure backoff. `recovery-error` retains its backoff independently of health events. Flapping is revalidated at the deadline and before every external operation; completed operations remain checkpointed.

Successful original sends only schedule their receipt; healthy events schedule at most one bounded batch for that resource. Each targeted wake has a five-second timeout. Neither drains unrelated receipts or sends recovery transport synchronously. Duty records a durable `wake_pending` marker on healthy transitions. `NotificationRecoverySweep` runs at startup and every five minutes, consuming indexed healthy pending markers, not polling/re-writing every unhealthy receipt. Batches left incomplete (including leased dormant receipts) retain their marker. Dormant completion and wake scheduling lock the resource's health-state row before mutating receipts. Resource batches inspect remaining receipts in subsequent statements while holding that lock, so overlapping marker clears cannot lose a leased worker's dormant completion; healthy dormancy atomically rearms the marker. The sweep refreshes authoritative health, so a stale healthy check marker with missing unlogged status becomes unknown, not proof of recovery. The ready worker still refreshes and compares generations before external operations. Large backlogs can take multiple sweeps; this is a safety catch-up, not a latency guarantee.

After the final failed attempt, the receipt gets a fresh `exhausted_at` timestamp and becomes `recovery-exhausted` and is excluded from automatic claims, with `resolved_at` left NULL. Existing receipts already beyond the configured budget are also exhausted without another transport call. Completed replies/reactions remain recorded. Increasing the property alone does not restart exhausted receipts; after fixing the cause, an operator must explicitly requeue a verified failed delivery without clearing its completed-operation markers. Check `job_history.details.errors` for the `NotificationRecoveries` job and the receipt status for permission revocation, deleted/changed connections or unavailable notifications. Original pending/fallback retries retain their existing, separate configured retry limit.

Transport delivery is not a distributed transaction. An original receipt left `dispatching` after interruption is ambiguous and is **not automatically resent**. An administrator must inspect the destination before repairing it: record the actual Slack channel/timestamp and sent state if delivered, or reset only a demonstrably unsent intent for retry. Do not blindly clear leases or delete receipts. Transport timeouts and external success followed by persistence failure can still leave uncertain outcomes; exactly-once delivery is not promised. Inspect destination and job logs before manually retrying uncertain operations.

Internal `notification_health_states`, `notification_health_episodes` and `notification_deliveries` are not exposed for PostgREST reads, writes, TRUNCATE or health-helper RPCs, even with source RLS disabled. Authoritative source triggers still allow otherwise-authorized source writes. Receipts retain destination addresses, subjects and conversation IDs (not credential secrets). Send-history retention sets the receipt's history reference to NULL, not deleting its recovery state. Hard deletion of a notification cascades its receipts; a soft-deleted notification defers recovery with an error. Automatic retention is deliberately conservative; it is **not a fully bounded lifecycle**. Include retained routing data in backup/privacy procedures.

### Maintenance properties and retention

All keys below have prefix `notification.recovery.`. Durations accept Go syntax plus the extended `d`/`w`/`y` units, so `720h`, `30d` and `4w2d` are equivalent. Schedule intervals are read when jobs are registered; restart/re-register jobs to change intervals. Other settings are read each invocation.

| Suffix | Default | Meaning |
| --- | --- | --- |
| `wake.batch-size` | `20` | Receipts scheduled per targeted resource wake |
| `sweep.interval` | `5m` | Durable-marker safety sweep interval |
| `sweep.batch-size` | `100` | Maximum marked resources per sweep |
| `sweep.budget` | `5s` | Sweep timeout |
| `retention.interval` | `1h` | Cleanup interval (no startup cleanup) |
| `retention.batch-size` | `500` | Maximum deletes **per category** per invocation |
| `retention.budget` | `5s` | Whole cleanup transaction timeout; statement timeout also bounded |
| `retention.resolved` | `720h` | Resolved receipts older than 30 days after `resolved_at` |
| `retention.exhausted` | `2160h` | Exhausted receipts older than 90 days after `exhausted_at` |
| `retention.episodes` | `720h` | Ended episodes older than 30 days after `healthy_at` |
| `retention.deleted-states` | `720h` | Deleted-state observation grace; positive values below 30 days are clamped to 30 days |

Zero retention duration disables that category. Negative or malformed retention values also disable it and warn; they never mean immediate deletion. Invalid/nonpositive interval, batch-size and budget settings warn and use the defaults above. Positive settings can be tuned; keep batches/budgets modest. Cleanup uses a transaction-scoped PostgreSQL try-advisory lock across processes, row locking with `SKIP LOCKED`, and a short transaction budget. Overlapping cleanup invocations skip; timeouts roll back the invocation. No transaction is held over transport calls.

Only explicitly `resolved` or `recovery-exhausted` **sent** receipts are aged; active leases, waiting/active work, unsent/prepared and ambiguous `dispatching` receipts are preserved. `created_at` is never a terminal age. Legacy exhausted receipts with NULL `exhausted_at` are preserved indefinitely, not backfilled with invented dates. Episodes require an actual `healthy_at`, no **any-status** delivery references and no current state references. The latest episode of a live resource remains protected even after its receipts expire.

Deleted-state pruning requires `health = deleted`, an explicit `deletion_observed_at` older than the grace, physical source absence, and no outstanding episode/delivery dependencies. Soft-deleted but physically present sources are preserved. Deletion observations are stamped only on real transitions to deleted, cleared on resurrection, and not guessed for legacy NULL-age tombstones. Never-ended orphan episodes and their dependent deleted state remain indefinitely; deletion is not fabricated recovery. History retention continues to SET NULL its receipt reference, never cascade receipts.

### Operator inspection and verified retry

Cleanup logs per-category delete counts. These privileged read-only queries expose intentional indefinite-retention exceptions (run against the intended environment with appropriate access; not through PostgREST):

```sql
SELECT status, count(*), count(*) FILTER (WHERE exhausted_at IS NULL) AS missing_exhaustion_age
FROM notification_deliveries GROUP BY status;
SELECT count(*) AS never_ended_episodes FROM notification_health_episodes WHERE healthy_at IS NULL;
SELECT health, count(*), count(*) FILTER (WHERE deletion_observed_at IS NULL) AS missing_deletion_age
FROM notification_health_states GROUP BY health;
SELECT count(*) AS pending_healthy_wakes FROM notification_health_states WHERE health = 'healthy' AND wake_pending;
```

After fixing the cause and inspecting the destination, explicitly retry **only** a verified exhausted receipt. Bind `:delivery_id` to that receipt's UUID; do not clear reply/reaction markers, routing snapshots or active leases:

```sql
UPDATE notification_deliveries
SET status = 'recovery-error', attempts = 0, exhausted_at = NULL,
    error = NULL, not_before = now(), lease_token = NULL, lease_until = NULL
WHERE id = :delivery_id AND status = 'recovery-exhausted'
  AND resolved_at IS NULL AND sent_at IS NOT NULL
  AND (lease_until IS NULL OR lease_until < now());
```

Expect exactly one affected row. Re-exhaustion records a fresh timestamp and starts a new retention window. Do not use this query for ambiguous original dispatches or uncertain external success. Increasing max retries alone does not restart exhausted receipts.

## Examples and development

Adapt the connection endpoints, secret references, resource filter and recipients before applying:

* [Native Slack](../fixtures/notifications/recovery-slack.yaml)
* [Shared system SMTP](../fixtures/notifications/recovery-system-smtp.yaml)
* [Named SMTP](../fixtures/notifications/recovery-named-smtp.yaml)

Release the matching duty schema/model changes before shipping Mission Control. Local validation uses an untracked scratch `-modfile` with an absolute duty replacement, never edits tracked dependency files.

**Existing integration blocker:** the baseline pins casbin/v2 v2.135.0 with gorm-adapter/v3 v3.41.0, whose adapter implements casbin/v3 persistence. Production RBAC bootstrap is incompatible; this work does not change those dependency pins. Recovery transport tests use an explicitly selected `recoverytests` build configuration with a v2-compatible test adapter and real least-privilege Casbin policies. These tests validate allow/deny/revocation behavior, **not production RBAC bootstrap**. The tagged configuration excludes legacy specs and live consumers to avoid global policy leakage; run legacy specs separately.

```sh
# Embedded databases only (setup may recreate its test database); all transports mocked.
# For MC local-duty testing, first create a scratch module file:
scratch=$(mktemp -d)
cp go.mod go.sum "$scratch/"
go mod edit -modfile="$scratch/go.mod" -replace=github.com/flanksource/duty="$(cd ../duty && pwd)"
export GOFLAGS="-modfile=$scratch/go.mod"
env -u DUTY_DB_URL -u DB_URL go run github.com/onsi/ginkgo/v2/ginkgo --tags=recoverytests --focus='Notification recovery' ./notification
env -u DUTY_DB_URL -u DB_URL go run github.com/onsi/ginkgo/v2/ginkgo --label-filter='!ignore_local' ./notification ./db ./api/v1 ./mail
unset GOFLAGS
cd ../duty
env -u DUTY_DB_URL -u DB_URL go run github.com/onsi/ginkgo/v2/ginkgo --focus='Notification recovery migration' ./tests
```
