# canarywatch: sh -c 'cd ../canary-checker &&   watchexec -e go -c make fast-build install'
# configwatch: sh -c 'cd ../confighub && watchexec -e go -c make build install'
# apmwatch: sh -c 'cd ../apm-hub && watchexec -e go -c make build install'
# incidentwatch: sh -c 'watchexec -e go -c make build install'
canary: canary-checker serve --httpPort 8081  --db    "postgres://localhost/incident_commander" -v   --disable-postgrest
config: config-db serve --httpPort 8085  --db    "postgres://localhost/incident_commander" -v      --disable-postgrest
ui: sh -c 'cd ../flanksource-ui && NEXT_PUBLIC_WITHOUT_SESSION=true npm run dev'
apm: apm-hub serve --httpPort 8082 ../apm-hub/samples/config.yaml
incident: incident-commander serve --apm-hub http://localhost:8082 --canary-checker http://localhost:8081 --db    "postgres://localhost/incident_commander" --config-db http://localhost:8085
