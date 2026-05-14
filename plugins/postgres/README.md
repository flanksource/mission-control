# Postgres Plugin

Shows connection state metrics from PostgreSQL `pg_stat_activity`.

## Operation

`connection_status` returns:

```json
{
  "Idle": 12,
  "Active": 3,
  "Unknown": 1
}
```

`Unknown` includes every state other than `idle` and `active`, including `idle in transaction`, `disabled`, and null states.

## Build & install

Update `Plugin.yaml` to point `spec.connections.types.postgres` at your Postgres connection, then:

```sh
mkdir -p $MISSION_CONTROL_PLUGIN_PATH
make -C plugins/postgres build
kubectl apply -f plugins/postgres/Plugin.yaml
```

## CLI

```sh
mission-control plugin postgres connection_status
```
