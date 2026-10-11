package archtest

import (
	"regexp"
	"strings"
)

// accountTables are identity's installation-wide tables (D-61–D-66,
// LLD-02 §5.4). They carry no tenant_id and no tenant RLS policy, because
// they describe a person or the installation and are read before any
// tenant is known. That is safe only while one package names them: the
// identity store, which exposes tenant-visible facts about a person only
// through RLS-scoped memberships. A query on one of these tables anywhere
// else would be a tenant-blind read of people's data.
var accountTables = []string{
	"users",
	"user_credentials",
	"sessions",
	"password_reset_tickets",
	"identity_provider_domains",
	"external_identities",
	"login_throttle",
	"auth_events",
}

// accountTableExemptPrefixes may name an account table: the identity
// store, and this checker.
var accountTableExemptPrefixes = []string{
	"internal/identity/postgres",
	"internal/archtest",
}

// accountTablePattern matches an account table where SQL names a table —
// after FROM, JOIN, INTO, UPDATE or TABLE — rather than anywhere as a
// word. "users" and "sessions" are ordinary English; a log message
// saying "list users" is not a query, and flagging it would teach people
// to ignore the gate.
var accountTablePattern = regexp.MustCompile(`(?i)\b(?:from|join|into|update|table)\s+(` +
	strings.Join(accountTables, "|") + `)\b`)

// ScanAccountTableNames reports each SQL reference to an account table
// outside identity's store (identity-accounts-and-sessions design D3).
// Like ScanEngineTableNames it inspects non-test string literals only.
func ScanAccountTableNames(apiRootDir string) ([]EngineTableUse, error) {
	return scanTableNames(apiRootDir, accountTablePattern, func(rel string) bool {
		for _, prefix := range accountTableExemptPrefixes {
			if strings.HasPrefix(rel, prefix+"/") {
				return true
			}
		}
		return false
	})
}
