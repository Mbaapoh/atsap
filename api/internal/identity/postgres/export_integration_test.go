//go:build integration

package postgres

// TenantCapLock exposes the advisory-lock key to the external test
// package, so a test can hold the lock and observe that provisioning
// waits for it.
const TenantCapLock = tenantCapLock
