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
* [Canary Checker](https://github.com/canary-checker) 
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
