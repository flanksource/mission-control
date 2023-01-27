Incident Commander is a mission control center for enterprises, managed service providers and SaaS providers, it provides:

* Single pane of glass across infrastructure, applications and the cloud by aggregating data from multiple data sources:
  * Monitoring & APM 
  * Logs
  * Configuration (Both declared via Git and runtime)
  * Change
  
 * Incident lifecycle automation that automatically opens and closes tickets based on the system health across multiple dimensions:
  * Availibility
  * Cost
  * Integration
  * Compliance
  * Performance 


## Components

* [Incident Commander](https://github.com/flanksource/incident-commander) (this repo) 
  - Primary entrypoint for all services
  - Bi-Directional communication with other help desk systems
  - Incident lifeycle automation 
* [Canary Checker](https://github.com/flanksource/canary-checker) 
  - Synethetic health checks 
  - Topology discovery and scanning
* [Config DB](https://github.com/flanksource/config-db)
  - Scanning configuration from AWS, Kubernetes, Git, SQL etc..
* [APM Hub](https://github.com/flanksource/apm-hub)
  - Proxies requests for logs, metrics and traces
* [Flanksource UI](https://github.com/flanksource/flanksource-ui)
  - Frontend
* [postgREST](https://postgrest.org/en/stable/) - REST API for Postgres
* [ORY Kratos](https://github.com/ory/kratos) - Authentication sub-system

## Quick Start Guide

The recommended method for installing Incident Commander is using [helm](https://helm.sh/)

### Install Helm

The following steps will install the latest version of helm

```bash
curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
chmod 700 get_helm.sh
./get_helm.sh
```

### Add the Flanksource helm repository

```bash
helm repo add flanksource https://flanksource.github.io/charts
helm repo update
```

### Configurable fields

See the [values file](https://github.com/flanksource/incident-commander-chart/blob/main/chart/values.yaml) for the full list of configurable fields.  Mandatory configuration values are for the configuration of the database, configuration of dependent charts, and it is recommended to also configure the UI ingress.

#### DB

Incident Commander requires a Postgres server to function.  A basic postgres server can be installed by the helm chart.

##### Chart-managed Server

|                     |        |
|---------------------|--------|
| db.create  | `true` |
| db.storageClass | Set to name of a storageclass available in the cluster |
| db.storage | Set to volume of storage to request |

The helm chart will create a postgres server statefulset, with a random password and default port, along with an incidentcommander database hosted on the server.

To specify a username and password for the chart-managed Postgres server, create a secret in the namespace that the chart will install to, named `postgres-connection`, which contains `POSTGRES_USER` and `POSTGRES_PASSWORD` keys.  If no pre-existing secret is created, a user called 'postgres' will be given a random password.

##### Prexisting Server

In order to connect to an existing Postgres server, a database must be created on the server, along with a user that has admin permissions

|                     |         |
|---------------------|---------|
| db.create  | `false` |
| db.secretKeyRef.name | Set to name of name of secret that contains a key containging the postgres connection URI |
| db.secretKeyRef.key | Set to the name of the key in the secret that contains the postgres connection URI |

The connection URI must be specified in the format `postgresql://"$user":"$password"@"$host"/"$database"`


#### Canary Checker Subchart

Incident Commander requires [Canary Checker](https://github.com/flanksource/canary-checker), and will automatically install it as a subchart.  The following values must be set correctly in the Canary Checker subchart stanza, as Helm does not currently allow subchart values propogration.  Note that these are the default values in the chart, and only the SecretKeyRef value should need to be changed in the case of an external database being used.

|                     |                   |
|---------------------|-------------------|
| canary-checker.db.external.enabled | must be set to `true` |
| canary-checker.db.external.create | must be set to `false` |
| canary-checker.db.external.secretKeyRef.name | must have the same value as db.secretKeyRef.name |
| canary-checker.db.external.secretKeyRef.key | must have the same value as db.secretKeyRef.key |
| canary-checker.flanksource-ui.enabled | must be set to `false` |

#### Canary Checker Subchart

Incident Commander requires [Config DB](https://github.com/flanksource/config-db), and will automatically install it as a subchart.  The following values must be set correctly in the Canary Checker subchart stanza, as Helm does not currently allow subchart values propogration.  Note that these are the default values in the chart, and only the SecretKeyRef value should need to be changed in the case of an external database being used.

|                     |                   |
|---------------------|-------------------|
| config-db.disablePostgrest | must be set to `true` |
| config-db.db.enabled | must be set to `true` |
| config-db.db.create | must be set to `false` |
| config-db.db.secretKeyRef.name | must have the same value as db.secretKeyRef.name |
| config-db.db.secretKeyRef.key | must have the same value as db.secretKeyRef.key |


#### Flanksource UI

Incident Commander itself only presents an API.  To view the data graphically, the Flanksource UI is required, and is installed as a subchart by default. The UI should be configured to allow external access to the UI via ingress

|                     |                   |
|---------------------|-------------------|
| flanksource-ui.ingress.host | URL at which the UI will be accessed |
| flanksource-ui.ingress.annotations | Map of annotations required by the ingress controller or certificate issuer |
| flanksource-ui.ingress.tls | Map of configuration options for TLS |

More details regarding ingress configuration can be found in the [kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/ingress/)

|                     |                   |
|---------------------|-------------------|
| flanksource-ui.backendURL | Required to be set to the name of the Incident Commander service.  The name will default to 'incident-commander' unless `nameOverride` is specified.  If `nameOverride is set, `backendURL` must be set to the same value |

### Deploy using Helm

To install into a new `incident-commander` namespace, run

```bash
helm install incident-commander-demo --wait -n incident-commander --create-namespace flanksource/incident-commander -f values.yaml
```

where `values.yaml` contains the configuration options detailed above.  eg

```yaml
db:
  external: true
  create: true
  storageClass: default
  storage: 30Gi
flanksource-ui:
  ingress:
    host: incident-commander.flanksource.com
    annotations:
      kubernetes.io/ingress.class: nginx
      kubernetes.io/tls-acme: "true"
    tls:
      - secretName: incident-commander-tls
        hosts:
        - incident-commander.flanksource.com
```
