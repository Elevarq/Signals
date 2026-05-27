package collector

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SafetyResult holds the outcome of a role/session safety check.
type SafetyResult struct {
	// HardFailures are blocking — collection must not proceed.
	HardFailures []string
	// Warnings are informational — collection may proceed.
	Warnings []string
}

// IsSafe returns true if there are no hard failures.
func (r SafetyResult) IsSafe() bool {
	return len(r.HardFailures) == 0
}

// Error returns a formatted error string for operator display.
func (r SafetyResult) Error() string {
	if r.IsSafe() {
		return ""
	}
	var b strings.Builder
	b.WriteString("safety check failed for connected role:\n")
	for _, f := range r.HardFailures {
		b.WriteString("  BLOCKED: ")
		b.WriteString(f)
		b.WriteString("\n")
	}
	b.WriteString("\nRemediation: create a dedicated monitoring role:\n")
	b.WriteString("  CREATE ROLE arq_monitor WITH LOGIN PASSWORD '...';\n")
	b.WriteString("  GRANT pg_monitor TO arq_monitor;\n")
	return b.String()
}

// ValidateRoleSafety checks the connected role's attributes.
// It queries pg_roles for the current_user and checks for unsafe attributes.
// Hard failures: rolsuper, rolreplication, rolbypassrls.
// Warnings: membership in pg_write_all_data (PG 14+).
func ValidateRoleSafety(ctx context.Context, pool *pgxpool.Pool) (SafetyResult, error) {
	var result SafetyResult

	// Query role attributes for the current user.
	var rolsuper, rolreplication, rolbypassrls bool
	var rolname string
	err := pool.QueryRow(ctx,
		`SELECT rolname, rolsuper, rolreplication, rolbypassrls
		 FROM pg_roles WHERE rolname = current_user`).
		Scan(&rolname, &rolsuper, &rolreplication, &rolbypassrls)
	if err != nil {
		return result, fmt.Errorf("query role attributes: %w", err)
	}

	// Hard failures — these block collection.
	if rolsuper {
		result.HardFailures = append(result.HardFailures,
			fmt.Sprintf("role %q has superuser attribute (rolsuper=true) — collection requires a non-superuser role", rolname))
	}
	if rolreplication {
		result.HardFailures = append(result.HardFailures,
			fmt.Sprintf("role %q has replication attribute (rolreplication=true) — collection requires a role without replication privileges", rolname))
	}
	if rolbypassrls {
		result.HardFailures = append(result.HardFailures,
			fmt.Sprintf("role %q has bypassrls attribute (rolbypassrls=true) — collection requires a role without BYPASSRLS", rolname))
	}

	// Hygiene warnings — informational only.
	// Check for pg_write_all_data membership (PG 14+).
	var writeAllCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pg_auth_members
		 WHERE member = (SELECT oid FROM pg_roles WHERE rolname = current_user)
		   AND roleid = (SELECT oid FROM pg_roles WHERE rolname = 'pg_write_all_data')`).
		Scan(&writeAllCount)
	if err == nil && writeAllCount > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("role %q is member of pg_write_all_data — consider revoking write privileges for a monitoring role", rolname))
	}
	// Ignore error on the hygiene check (pg_write_all_data may not exist on older PG)

	return result, nil
}

// Note: Session read-only verification and timeout enforcement are now
// performed inline in collector.collectTarget() using a dedicated
// acquired connection. SET LOCAL is used inside the transaction to
// guarantee timeouts apply to the exact connection/transaction that
// executes collection queries. See collector.go:collectTarget().
