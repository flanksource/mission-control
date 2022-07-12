## DB

The database is exposed via [postgREST](https://postgrest.org/en/stable/) on `/db/`


## Local Setup

```
# Use docker-compose to setup the server
docker compose up

# To connect to the postgres database
docker compose exec -it db psql postgres://ic:ic@localhost:5432/ic
```
