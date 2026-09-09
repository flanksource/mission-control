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

`NotificationRecoveries` reconciles persisted receipts on startup and every 15 seconds, with bounded batches, five-minute leases and no database transaction held over network calls. Healthy deferrals are checked again; other recovery failures back off up to one hour. Check job errors and receipt status for permission revocation, deleted/changed connections or unavailable notifications. Original pending/fallback retries retain their existing configured retry limit.

Transport delivery is not a distributed transaction. An original receipt left `dispatching` after interruption is ambiguous and is **not automatically resent**. An administrator must inspect the destination before repairing it: record the actual Slack channel/timestamp and sent state if delivered, or reset only a demonstrably unsent intent for retry. Do not blindly clear leases or delete receipts. Transport timeouts and external success followed by persistence failure can still leave uncertain outcomes; exactly-once delivery is not promised. Inspect destination and job logs before manually retrying uncertain operations.

Internal `notification_health_states`, `notification_health_episodes` and `notification_deliveries` are not exposed for PostgREST reads, writes, TRUNCATE or health-helper RPCs, even with source RLS disabled. Authoritative source triggers still allow otherwise-authorized source writes. Receipts retain destination addresses, subjects and conversation IDs (not credential secrets). Send-history retention sets the receipt's history reference to NULL, not deleting its recovery state. Hard deletion of a notification cascades its receipts; a soft-deleted notification defers recovery with an error. There is no automatic TTL for the new episode/receipt data: include it in backup/privacy and operator retention procedures, and never purge outstanding receipts or referenced episodes.

## Examples and development

Adapt the connection endpoints, secret references, resource filter and recipients before applying:

* [Native Slack](../fixtures/notifications/recovery-slack.yaml)
* [Shared system SMTP](../fixtures/notifications/recovery-system-smtp.yaml)
* [Named SMTP](../fixtures/notifications/recovery-named-smtp.yaml)

This change currently uses the temporary `go.mod` replacement `github.com/flanksource/duty => ../duty`; release the matching duty changes and replace it with a release dependency before shipping.

**Existing integration blocker:** the baseline pins casbin/v2 v2.135.0 with gorm-adapter/v3 v3.41.0, whose adapter implements casbin/v3 persistence. Production RBAC bootstrap is incompatible; this work does not change those dependency pins. Recovery transport tests use an explicitly selected `recoverytests` build configuration with a v2-compatible test adapter and real least-privilege Casbin policies. These tests validate allow/deny/revocation behavior, **not production RBAC bootstrap**. The tagged configuration excludes legacy specs and live consumers to avoid global policy leakage; run legacy specs separately.

```sh
# Embedded databases only (setup may recreate its test database); all transports mocked.
env -u DUTY_DB_URL go run github.com/onsi/ginkgo/v2/ginkgo --tags=recoverytests --focus='Notification recovery' ./notification
env -u DUTY_DB_URL go run github.com/onsi/ginkgo/v2/ginkgo --label-filter='!ignore_local' ./notification ./db ./api/v1 ./mail
cd ../duty
env -u DUTY_DB_URL go run github.com/onsi/ginkgo/v2/ginkgo --focus='Notification recovery migration' ./tests
```
