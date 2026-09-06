REVOKE SELECT, UPDATE ON outbox FROM atsap_outbox_worker;

DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS usage_seconds;
DROP TABLE IF EXISTS channel_history;
DROP TABLE IF EXISTS call_participants;
DROP TABLE IF EXISTS calls;
DROP TABLE IF EXISTS tenants;
