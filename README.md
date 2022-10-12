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

## DB

The database is exposed via [postgREST](https://postgrest.org/en/stable/) on `/db/`

## Bumping UI Version

To update the `@flanksource/flanksource-ui` dependency run:

```shell
npm install --prefix ui ./ui @flanksource/flanksource-ui@<VERSION>
```
