-- Rolls identity back to revision 1: per-tenant principals carrying their
-- own username, email and password hash.
--
-- Lossy by design for one case only: a user with no password (invited and
-- never set, or SSO-only) gets an unusable hash marker, so that principal
-- cannot sign in until an administrator provisions it again. Usernames are
-- rebuilt from the email's local part, suffixed where two members of one
-- tenant would collide.

DROP TABLE IF EXISTS auth_events;
DROP TABLE IF EXISTS login_throttle;
DROP TABLE IF EXISTS external_identities;
DROP POLICY IF EXISTS identity_providers_discovery ON identity_providers;
DROP TABLE IF EXISTS identity_provider_domains;
DROP TABLE IF EXISTS identity_providers;
DROP TABLE IF EXISTS password_reset_tickets;
DROP TABLE IF EXISTS sessions;

DROP POLICY IF EXISTS role_bindings_self ON role_bindings;
DROP POLICY IF EXISTS principals_self ON principals;

ALTER TABLE principals NO FORCE ROW LEVEL SECURITY;
ALTER TABLE principals DISABLE ROW LEVEL SECURITY;

ALTER TABLE principals ADD COLUMN username VARCHAR(100);
ALTER TABLE principals ADD COLUMN email VARCHAR(255);
ALTER TABLE principals ADD COLUMN password_hash VARCHAR(255);

UPDATE principals p SET
    email = u.email,
    password_hash = COALESCE(
        (SELECT c.secret FROM user_credentials c WHERE c.user_id = u.id AND c.kind = 'password'),
        '!no-password:revision-2-user')
  FROM users u WHERE u.id = p.user_id;

UPDATE principals p SET username = n.username
  FROM (
    SELECT id,
           CASE WHEN row_number() OVER (PARTITION BY tenant_id, split_part(email, '@', 1) ORDER BY created_at, id) = 1
                THEN split_part(email, '@', 1)
                ELSE split_part(email, '@', 1) || '-' ||
                     row_number() OVER (PARTITION BY tenant_id, split_part(email, '@', 1) ORDER BY created_at, id)
           END AS username
      FROM principals
  ) n
 WHERE n.id = p.id;

ALTER TABLE principals ALTER COLUMN username SET NOT NULL;
ALTER TABLE principals ALTER COLUMN email SET NOT NULL;
ALTER TABLE principals ALTER COLUMN password_hash SET NOT NULL;
ALTER TABLE principals ADD CONSTRAINT principals_tenant_id_username_key UNIQUE (tenant_id, username);

DROP INDEX IF EXISTS principals_user_idx;
ALTER TABLE principals DROP CONSTRAINT IF EXISTS principals_tenant_user_key;
ALTER TABLE principals DROP COLUMN user_id;

ALTER TABLE principals ENABLE ROW LEVEL SECURITY;
ALTER TABLE principals FORCE ROW LEVEL SECURITY;

DROP TABLE IF EXISTS user_credentials;
DROP TABLE IF EXISTS users;
