# Production Asterisk config overlay

This directory is mounted read-only over `/etc/asterisk` in
`docker-compose.prod.yml`, replacing the dev defaults baked into the image
(`core/conf/`).

Populate it on the deploy server (never commit real secrets) with at least:

- `ari.conf` — strong, unique ARI password
- `manager.conf` — strong, unique AMI password, `permit`/`deny` restricted
  to the app container's network only
- `pjsip.conf` — your real SIP trunk/endpoint configuration
- `extensions.conf` — your production dialplan
- `cdr_pgsql.conf` / `cel_pgsql.conf` — point `hostname`/`user`/`password`
  at your production Postgres instance (the dev stack's `postgres`
  container is local-only and isn't part of `docker-compose.prod.yml`).
  Apply `deploy/postgres/init/*.sql` against that database first.
- `cel.conf` — only needed if you want CEL, not just CDR.

Copy the files from `core/conf/` as a starting point and edit
the credentials and routing for production.
