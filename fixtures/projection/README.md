# Projection example

`trust-center.yaml` is a multi-document list of independent projections. Each document owns one source query and, when applicable, one target mapping.

The Mission Control connection and context are inferred from the normal Faro CLI context. Replace the example IDs in `target.yaml` with config and external-user IDs from that context before applying mappings.

`identityAccess.userTypes` is a list of independent CEL rules. Order does not provide precedence because every external user must match exactly one rule. Add provider vocabulary there rather than changing Faro; zero matches and overlapping matches are errors.

Validate the manifest, CEL expressions, target selectors, and JSON Schema without querying Mission Control:

```sh
faro projection verify --file examples/projection/trust-center.yaml
```

Preview source data without writing the target:

```sh
faro projection run --file examples/projection/trust-center.yaml --yaml
```

Preview mapped target changes:

```sh
faro projection apply --file examples/projection/trust-center.yaml --dry-run --yaml
```

Apply every document that has a target. The targetless permission-change document remains available through `projection run` and is skipped by `projection apply`.

```sh
faro projection apply --file examples/projection/trust-center.yaml --yaml
```
