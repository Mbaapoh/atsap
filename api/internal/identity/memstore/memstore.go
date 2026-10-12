// Package memstore is an in-memory implementation of every identity
// storage port, for unit tests in identity/application and identity/rpc.
//
// It mirrors the database's constraints, not just its shape: emails are
// unique case-insensitively, one membership per (tenant, person), a grant
// is idempotent (the role_bindings primary key), the last tenant
// administrator cannot be revoked, a domain is claimed once, a ticket
// redeems once. A fake that is looser than the database lets a test pass
// that production would fail — that happened once already (fake bindings
// that allowed duplicates hid a revocation bug), so the rule here is: if
// the schema refuses it, so does this.
//
// Shipping code never imports this package.
package memstore

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// Store is the in-memory store. Exported fields are test knobs.
type Store struct {
	mu sync.Mutex

	tenants    map[shareddomain.TenantID]domain.Tenant
	principals map[shareddomain.PrincipalID]domain.Principal
	bindings   []domain.RoleBinding
	apiKeys    map[shareddomain.APIKeyID]domain.APIKey
	users      map[domain.UserID]domain.User
	sessions   map[domain.SessionID]domain.Session
	tickets    []ticket
	providers  map[domain.IdentityProviderID]domain.IdentityProvider
	claims     map[string]domain.DomainClaim
	externals  map[string]domain.ExternalIdentity
	throttle   map[string]*throttleRow

	// Audit and Events are appended in order; tests read them.
	Audit  []domain.AuditEntry
	Events []domain.AuthEvent

	// Failure injection.
	FailAudit    error
	FailBindings error
	FailRevoke   error
	TenantCap    int
	TenantCapErr error

	// Revoked records what RevokeAPIKey was asked to do.
	Revoked []Revocation
}

// Revocation is one RevokeAPIKey call.
type Revocation struct {
	TenantID shareddomain.TenantID
	KeyID    shareddomain.APIKeyID
	At       time.Time
}

type ticket struct {
	ports.ResetTicket
	usedAt *time.Time
}

type throttleRow struct {
	failures     int
	blockedUntil time.Time
	last         time.Time
}

// New returns an empty store.
func New() *Store {
	return &Store{
		tenants:    map[shareddomain.TenantID]domain.Tenant{},
		principals: map[shareddomain.PrincipalID]domain.Principal{},
		apiKeys:    map[shareddomain.APIKeyID]domain.APIKey{},
		users:      map[domain.UserID]domain.User{},
		sessions:   map[domain.SessionID]domain.Session{},
		providers:  map[domain.IdentityProviderID]domain.IdentityProvider{},
		claims:     map[string]domain.DomainClaim{},
		externals:  map[string]domain.ExternalIdentity{},
		throttle:   map[string]*throttleRow{},
	}
}

// MaxTenants makes the store its own tenant-cap source.
func (s *Store) MaxTenants(context.Context) (int, error) { return s.TenantCap, s.TenantCapErr }

// --- tenants --------------------------------------------------------------

func (s *Store) CreateTenant(_ context.Context, t domain.Tenant, maxTenants int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxTenants > 0 && len(s.tenants) >= maxTenants {
		return ports.ErrEntitlementExceeded
	}
	s.tenants[t.ID] = t
	return nil
}

func (s *Store) GetTenant(_ context.Context, id shareddomain.TenantID) (domain.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tenants[id]
	if !ok {
		return domain.Tenant{}, ports.ErrNotFound
	}
	return t, nil
}

func (s *Store) SetTenantStatus(_ context.Context, id shareddomain.TenantID, status domain.TenantStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tenants[id]
	if !ok {
		return ports.ErrNotFound
	}
	t.Status = status
	s.tenants[id] = t
	return nil
}

// --- principals --------------------------------------------------------------

func (s *Store) withUser(p domain.Principal) domain.Principal {
	if u, ok := s.users[p.UserID]; ok {
		p.Email, p.DisplayName = u.Email, u.DisplayName
	}
	return p
}

func (s *Store) CreatePrincipal(_ context.Context, p domain.Principal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.principals {
		if existing.TenantID == p.TenantID && existing.UserID == p.UserID {
			return ports.ErrAlreadyMember
		}
	}
	p.Email, p.DisplayName = "", ""
	s.principals[p.ID] = p
	return nil
}

func (s *Store) GetPrincipal(_ context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID) (domain.Principal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.principals[id]
	if !ok || p.TenantID != tenantID { // RLS: another tenant's row is absent
		return domain.Principal{}, ports.ErrNotFound
	}
	return s.withUser(p), nil
}

func (s *Store) GetPrincipalByUser(_ context.Context, tenantID shareddomain.TenantID, userID domain.UserID) (domain.Principal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.principals {
		if p.TenantID == tenantID && p.UserID == userID {
			return s.withUser(p), nil
		}
	}
	return domain.Principal{}, ports.ErrNotFound
}

func (s *Store) SetPrincipalStatus(_ context.Context, tenantID shareddomain.TenantID, id shareddomain.PrincipalID, status domain.PrincipalStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.principals[id]
	if !ok || p.TenantID != tenantID {
		return ports.ErrNotFound
	}
	p.Status = status
	s.principals[id] = p
	return nil
}

func (s *Store) ListPrincipals(_ context.Context, tenantID shareddomain.TenantID, limit, offset int) ([]domain.Principal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []domain.Principal
	for _, p := range s.principals {
		if p.TenantID == tenantID {
			all = append(all, s.withUser(p))
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Email != all[j].Email {
			return all[i].Email < all[j].Email
		}
		return all[i].ID.String() < all[j].ID.String()
	})
	if offset >= len(all) {
		return nil, nil
	}
	all = all[offset:]
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	return all, nil
}

// --- role bindings --------------------------------------------------------------

func (s *Store) CreateRoleBinding(_ context.Context, b domain.RoleBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.bindings {
		if e.PrincipalID == b.PrincipalID && e.Role == b.Role && e.Scope == b.Scope {
			return nil // primary key: a re-grant is a no-op
		}
	}
	s.bindings = append(s.bindings, b)
	return nil
}

func (s *Store) ListRoleBindings(_ context.Context, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID) ([]domain.RoleBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailBindings != nil {
		return nil, s.FailBindings
	}
	return s.bindingsOf(tenantID, principalID), nil
}

func (s *Store) bindingsOf(tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID) []domain.RoleBinding {
	var out []domain.RoleBinding
	for _, b := range s.bindings {
		if b.TenantID == tenantID && b.PrincipalID == principalID {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role+out[i].Scope < out[j].Role+out[j].Scope })
	return out
}

func (s *Store) DeleteRoleBinding(_ context.Context, b domain.RoleBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeBinding(b)
	return nil
}

func (s *Store) removeBinding(b domain.RoleBinding) bool {
	for i, e := range s.bindings {
		if e.TenantID == b.TenantID && e.PrincipalID == b.PrincipalID && e.Role == b.Role && e.Scope == b.Scope {
			s.bindings = append(s.bindings[:i], s.bindings[i+1:]...)
			return true
		}
	}
	return false
}

func (s *Store) ListRoleBindingsFor(_ context.Context, tenantID shareddomain.TenantID, ids []shareddomain.PrincipalID) (map[shareddomain.PrincipalID][]domain.RoleBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[shareddomain.PrincipalID][]domain.RoleBinding{}
	for _, id := range ids {
		if bs := s.bindingsOf(tenantID, id); len(bs) > 0 {
			out[id] = bs
		}
	}
	return out, nil
}

func (s *Store) RevokeRoleBinding(_ context.Context, b domain.RoleBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b.Role == "TENANT_ADMIN" && b.Scope == domain.ScopeTenant {
		admins, holds := 0, false
		for _, e := range s.bindings {
			p := s.principals[e.PrincipalID]
			if e.TenantID == b.TenantID && e.Role == "TENANT_ADMIN" && e.Scope == domain.ScopeTenant && p.Status == domain.PrincipalActive {
				admins++
				if e.PrincipalID == b.PrincipalID {
					holds = true
				}
			}
		}
		if holds && admins <= 1 {
			return ports.ErrLastAdministrator
		}
	}
	if !s.removeBinding(b) {
		return ports.ErrNotFound
	}
	return nil
}

// --- api keys ----------------------------------------------------------------

func (s *Store) CreateAPIKey(_ context.Context, k domain.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiKeys[k.ID] = k
	return nil
}

func (s *Store) GetAPIKeyByHash(_ context.Context, tenantID shareddomain.TenantID, keyHash string) (domain.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.apiKeys {
		if k.TenantID == tenantID && k.KeyHash == keyHash {
			return k, nil
		}
	}
	return domain.APIKey{}, ports.ErrNotFound
}

func (s *Store) RevokeAPIKey(_ context.Context, tenantID shareddomain.TenantID, id shareddomain.APIKeyID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailRevoke != nil {
		return s.FailRevoke
	}
	s.Revoked = append(s.Revoked, Revocation{TenantID: tenantID, KeyID: id, At: at})
	if k, ok := s.apiKeys[id]; ok && k.TenantID == tenantID {
		t := at
		k.RevokedAt = &t
		s.apiKeys[id] = k
	}
	return nil
}

// --- audit -------------------------------------------------------------------

func (s *Store) AppendAudit(_ context.Context, e domain.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailAudit != nil {
		return s.FailAudit
	}
	s.Audit = append(s.Audit, e)
	return nil
}

func (s *Store) AppendAuditTx(ctx context.Context, _ pgx.Tx, e domain.AuditEntry) error {
	return s.AppendAudit(ctx, e)
}

func (s *Store) ListAudit(_ context.Context, tenantID shareddomain.TenantID, _ int) ([]domain.AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.AuditEntry
	for _, e := range s.Audit {
		if e.TenantID == tenantID {
			out = append(out, e)
		}
	}
	return out, nil
}

// --- users -------------------------------------------------------------------

func (s *Store) CreateUser(_ context.Context, u domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.users {
		if strings.EqualFold(e.Email, u.Email) {
			return ports.ErrEmailTaken
		}
	}
	if u.Password != nil {
		p := *u.Password
		u.Password = &p
	}
	s.users[u.ID] = u
	return nil
}

func (s *Store) GetUser(_ context.Context, id domain.UserID) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return domain.User{}, ports.ErrNotFound
	}
	return copyUser(u), nil
}

func (s *Store) GetUserByEmail(_ context.Context, email string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			return copyUser(u), nil
		}
	}
	return domain.User{}, ports.ErrNotFound
}

func copyUser(u domain.User) domain.User {
	if u.Password != nil {
		p := *u.Password
		u.Password = &p
	}
	return u
}

func (s *Store) SetPassword(_ context.Context, id domain.UserID, cred domain.PasswordCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return ports.ErrNotFound
	}
	u.Password = &cred
	s.users[id] = u
	return nil
}

func (s *Store) SetUserStatus(_ context.Context, id domain.UserID, status domain.UserStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return ports.ErrNotFound
	}
	u.Status = status
	s.users[id] = u
	return nil
}

func (s *Store) ListMemberships(_ context.Context, id domain.UserID) ([]domain.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Membership
	for _, p := range s.principals {
		t := s.tenants[p.TenantID]
		if p.UserID != id || t.Status != domain.TenantActive {
			continue
		}
		m := domain.Membership{PrincipalID: p.ID, TenantID: p.TenantID, TenantName: t.Name, Status: p.Status, Roles: []string{}}
		for _, b := range s.bindingsOf(p.TenantID, p.ID) {
			m.Roles = append(m.Roles, b.Role)
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].TenantName+out[i].TenantID.String() < out[j].TenantName+out[j].TenantID.String()
	})
	return out, nil
}

// --- liveness ------------------------------------------------------------------

func (s *Store) LiveState(_ context.Context, tenantID shareddomain.TenantID, principalID shareddomain.PrincipalID, session domain.SessionID) (ports.LiveState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.principals[principalID]
	if !ok || p.TenantID != tenantID {
		return ports.LiveState{}, ports.ErrNotFound
	}
	t, ok := s.tenants[tenantID]
	if !ok {
		return ports.LiveState{}, ports.ErrNotFound
	}
	u := s.users[p.UserID]
	st := ports.LiveState{
		Principal: s.withUser(p), Tenant: t, UserStatus: u.Status,
		MustChange: u.Password != nil && u.Password.MustChange,
	}
	if s.FailBindings != nil {
		return ports.LiveState{}, s.FailBindings
	}
	st.Bindings = s.bindingsOf(tenantID, principalID)
	if !session.IsZero() {
		sess, ok := s.sessions[session]
		if !ok {
			return ports.LiveState{}, ports.ErrNotFound
		}
		st.Session = &sess
	}
	return st, nil
}

// --- sessions --------------------------------------------------------------------

func (s *Store) CreateSession(_ context.Context, sess domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
	return nil
}

func (s *Store) GetSession(_ context.Context, id domain.SessionID) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return domain.Session{}, ports.ErrNotFound
	}
	return sess, nil
}

// UpdateSessionByDigest holds the store lock across fn, as the database
// holds the row lock, so concurrent refreshes serialize. fn may call back
// into the store, so the lock is released while it runs and the session
// is re-checked afterwards.
func (s *Store) UpdateSessionByDigest(_ context.Context, digest string, fn func(domain.Session) (domain.Session, error)) (domain.Session, error) {
	s.mu.Lock()
	var cur domain.Session
	found := false
	for _, sess := range s.sessions {
		if digest != "" && (sess.RefreshDigest == digest || sess.PreviousDigest == digest) {
			cur, found = sess, true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		return domain.Session{}, ports.ErrNotFound
	}
	next, fnErr := fn(cur)
	s.mu.Lock()
	defer s.mu.Unlock()
	if latest := s.sessions[cur.ID]; latest.RefreshDigest != cur.RefreshDigest || (latest.RevokedAt == nil) != (cur.RevokedAt == nil) {
		return domain.Session{}, ports.ErrSessionNotUsable // lost a race, as the row lock would have serialized it
	}
	s.sessions[next.ID] = next
	return next, fnErr
}

func (s *Store) RevokeUserSessions(_ context.Context, user domain.UserID, keep domain.SessionID, at time.Time, reason string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, sess := range s.sessions {
		if sess.UserID == user && sess.RevokedAt == nil && id != keep {
			s.sessions[id] = sess.Revoke(at, reason)
			n++
		}
	}
	return n, nil
}

func (s *Store) RevokeSession(_ context.Context, id domain.SessionID, at time.Time, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		s.sessions[id] = sess.Revoke(at, reason)
	}
	return nil
}

// --- tickets ----------------------------------------------------------------------

func (s *Store) CreateTicket(_ context.Context, t ports.ResetTicket) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tickets = append(s.tickets, ticket{ResetTicket: t})
	return nil
}

func (s *Store) CountTickets(_ context.Context, user domain.UserID, purpose domain.TicketPurpose, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, t := range s.tickets {
		if t.UserID == user && t.Purpose == purpose && !t.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (s *Store) RedeemTicket(_ context.Context, digest string, cred domain.PasswordCredential, at time.Time) (domain.UserID, domain.TicketPurpose, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tickets {
		if t.Digest != digest || t.usedAt != nil || !at.Before(t.ExpiresAt) {
			continue
		}
		for j := range s.tickets {
			if s.tickets[j].UserID == t.UserID && s.tickets[j].usedAt == nil {
				used := at
				s.tickets[j].usedAt = &used
			}
		}
		u := s.users[t.UserID]
		c := cred
		u.Password = &c
		s.users[t.UserID] = u
		for id, sess := range s.sessions {
			if sess.UserID == t.UserID {
				s.sessions[id] = sess.Revoke(at, domain.RevokePasswordReset)
			}
		}
		return s.tickets[i].UserID, t.Purpose, nil
	}
	return domain.UserID{}, "", ports.ErrNotFound
}

// --- identity providers -------------------------------------------------------------

func (s *Store) CreateProvider(_ context.Context, p domain.IdentityProvider) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[p.ID] = p
	return nil
}

func (s *Store) ClaimDomain(_ context.Context, c domain.DomainClaim) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := strings.ToLower(c.Domain)
	if _, taken := s.claims[d]; taken {
		return ports.ErrDomainClaimed
	}
	c.Domain = d
	s.claims[d] = c
	return nil
}

func (s *Store) VerifyDomain(_ context.Context, d string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.claims[strings.ToLower(d)]
	if !ok {
		return ports.ErrNotFound
	}
	t := at
	c.VerifiedAt = &t
	s.claims[c.Domain] = c
	return nil
}

func (s *Store) ProviderForDomain(_ context.Context, d string) (domain.DomainClaim, domain.IdentityProvider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.claims[strings.ToLower(d)]
	if !ok || c.VerifiedAt == nil {
		return domain.DomainClaim{}, domain.IdentityProvider{}, ports.ErrNotFound
	}
	return c, s.providers[c.ProviderID], nil
}

func (s *Store) LinkExternalIdentity(_ context.Context, e domain.ExternalIdentity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := e.ProviderID.String() + "|" + e.Subject
	if _, ok := s.externals[key]; ok {
		return ports.ErrAlreadyLinked
	}
	s.externals[key] = e
	return nil
}

func (s *Store) UserForExternalIdentity(_ context.Context, provider domain.IdentityProviderID, subject string) (domain.UserID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.externals[provider.String()+"|"+subject]
	if !ok {
		return domain.UserID{}, ports.ErrNotFound
	}
	return e.UserID, nil
}

// --- auth events -----------------------------------------------------------------------

func (s *Store) AppendAuthEvent(_ context.Context, e domain.AuthEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Events = append(s.Events, e)
	return nil
}

func (s *Store) ListAuthEvents(_ context.Context, user domain.UserID, limit int) ([]domain.AuthEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.AuthEvent
	for i := len(s.Events) - 1; i >= 0; i-- {
		if e := s.Events[i]; e.UserID != nil && *e.UserID == user {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- throttle ----------------------------------------------------------------------------

func (s *Store) BlockedFor(_ context.Context, keys []string, now time.Time) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var longest time.Duration
	for _, k := range keys {
		if r, ok := s.throttle[k]; ok && r.blockedUntil.After(now) {
			if d := r.blockedUntil.Sub(now); d > longest {
				longest = d
			}
		}
	}
	return longest, nil
}

func (s *Store) RecordFailure(_ context.Context, keys []string, now time.Time, policy domain.ThrottlePolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		r, ok := s.throttle[k]
		if !ok {
			r = &throttleRow{}
			s.throttle[k] = r
		}
		if r.failures > 0 && !policy.Continues(r.last, now) {
			r.failures = 0
		}
		r.failures++
		r.last = now
		if d := policy.Delay(r.failures); d > 0 {
			r.blockedUntil = now.Add(d)
		}
	}
	return nil
}

func (s *Store) Clear(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.throttle, key)
	return nil
}

// --- test helpers ------------------------------------------------------------------------------

// Mail is one message the Outbox received.
type Mail struct{ To, Subject, Body string }

// Outbox is an in-memory Mailer.
type Outbox struct {
	mu   sync.Mutex
	Sent []Mail
	Fail error
}

// Send records a message.
func (o *Outbox) Send(_ context.Context, to, subject, body string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.Fail != nil {
		return o.Fail
	}
	o.Sent = append(o.Sent, Mail{To: to, Subject: subject, Body: body})
	return nil
}

// Last returns the last message sent to an address.
func (o *Outbox) Last(to string) (Mail, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := len(o.Sent) - 1; i >= 0; i-- {
		if strings.EqualFold(o.Sent[i].To, to) {
			return o.Sent[i], true
		}
	}
	return Mail{}, false
}

// TenantCount returns how many tenants exist.
func (s *Store) TenantCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tenants)
}

// Sessions returns a copy of every session, for assertions.
func (s *Store) Sessions() []domain.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

var _ interface {
	ports.TenantStore
	ports.PrincipalStore
	ports.RoleBindingStore
	ports.APIKeyStore
	ports.AuditStore
	ports.UserStore
	ports.SessionStore
	ports.ResetTicketStore
	ports.IdentityProviderStore
	ports.AuthEventStore
	ports.LoginThrottle
	ports.LivenessReader
	ports.TenantCap
} = (*Store)(nil)

var _ ports.Mailer = (*Outbox)(nil)
