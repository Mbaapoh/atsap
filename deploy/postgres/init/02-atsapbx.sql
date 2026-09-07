-- Provisions the AtsaPBX application database, separate from the
-- `asterisk` CDR/CEL database Asterisk itself writes to (01-cdr-cel.sql).
-- Runs once, as the Postgres superuser (POSTGRES_USER), at container
-- bootstrap — this is the only point role attributes requiring elevated
-- privilege (BYPASSRLS) can be granted; see api/migrations/0001 for the
-- narrow object-level grant that follows once the outbox table exists.

-- App role: owns the atsapbx database outright, so its own golang-migrate
-- migrations (api/migrations/) can create and alter schema with no
-- further grants needed. Ordinary role: no BYPASSRLS, no CREATEROLE — it
-- must never itself bypass the RLS policies its migrations create.
CREATE ROLE atsapbx_app LOGIN PASSWORD 'devpassword123';
CREATE DATABASE atsapbx OWNER atsapbx_app;

-- Outbox worker role: BYPASSRLS is required to read across every
-- tenant's rows to publish that tenant's events (docs/hld/03-domain-model.md
-- §5, design.md "Outbox worker runs as a narrow BYPASSRLS platform
-- role"). Only a superuser can grant BYPASSRLS, hence creating it here
-- rather than in a migration. It is granted CONNECT only, and no table
-- privileges yet -- api/migrations/0001 grants SELECT/UPDATE on outbox
-- specifically, once that table exists, and never on any other table.
CREATE ROLE atsap_outbox_worker LOGIN PASSWORD 'devpassword123' BYPASSRLS NOSUPERUSER NOCREATEROLE NOCREATEDB;
GRANT CONNECT ON DATABASE atsapbx TO atsap_outbox_worker;
