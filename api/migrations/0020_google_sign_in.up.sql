-- "Sign in with Google" (D-67, D-83): installation-wide OIDC, linked to
-- an existing person, never by matching email at sign-in.

-- One link per (issuer, subject), and one per person for Google. The
-- issuer is stored so generic OIDC providers can be added without a new
-- table; the application supports Google only.
CREATE TABLE IF NOT EXISTS federated_identities (
    issuer VARCHAR(255) NOT NULL,
    subject VARCHAR(255) NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email VARCHAR(320) NOT NULL,
    linked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (issuer, subject),
    UNIQUE (issuer, user_id)
);

-- Short-lived OAuth state and PKCE verifier. Server-side, installation-
-- wide and never exposed to the console; a used or expired row cannot be
-- consumed again.
CREATE TABLE IF NOT EXISTS oauth_login_states (
    state_digest VARCHAR(64) PRIMARY KEY,
    purpose VARCHAR(10) NOT NULL,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    invite_digest VARCHAR(64),
    return_to VARCHAR(512) NOT NULL,
    verifier VARCHAR(128) NOT NULL,
    nonce VARCHAR(128) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    CONSTRAINT oauth_login_states_purpose_valid CHECK (purpose IN ('SIGN_IN', 'LINK', 'INVITE'))
);

CREATE INDEX IF NOT EXISTS oauth_login_states_expiry ON oauth_login_states (expires_at);
