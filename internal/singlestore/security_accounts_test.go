package singlestore

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// Tests derive from specifications/mysql-collectors.acceptance.md (TC-MYC-19/20/21)
// and specifications/mariadb-collectors.acceptance.md (TC-MDC-24/25/26) — the #693
// user-accounts + global-privileges security collectors. These are the most
// sensitive collectors in the repo: the central guarantee (INV-MYC-04 / INV-MDC-04)
// is that NO password hash / secret material is ever selected, normalized, logged,
// or returned — the empty-password signal is carried only as a derived boolean.

// mustCollector returns the named collector from a catalog or fails the test
// (collectorByID is the shared package-test helper that returns nil when absent).
func mustCollector(t *testing.T, cat []Collector, id string) *Collector {
	t.Helper()
	c := collectorByID(cat, id)
	if c == nil {
		t.Fatalf("collector %q not found in catalog", id)
	}
	return c
}

// hashColumnTokens are the password/hash column identifiers that must NEVER appear
// as a selected column, an evidence key, or (for a value) a hash-shaped string.
// They are matched as whole select-list tokens, not as substrings of the computed
// has_empty_password predicate.
var hashColumnTokens = []string{"authentication_string", "password"}

// selectListOf returns the lowercase text between the leading SELECT and the FROM
// of a single-table SELECT — i.e. the projected columns only, excluding any
// WHERE/predicate clause. Used to prove no hash column is *selected* even when the
// computed has_empty_password boolean references the hash inside its expression.
func selectListOf(sql string) string {
	low := strings.ToLower(sql)
	start := strings.Index(low, "select ")
	if start < 0 {
		return low
	}
	start += len("select ")
	end := strings.Index(low[start:], " from ")
	if end < 0 {
		return low[start:]
	}
	return low[start : start+end]
}

// assertNoBareHashColumnSelected fails if a hash column appears in the select list
// as a standalone projected column. A hash token is allowed only inside the
// has_empty_password boolean expression (… = ” …) and never as a bare selected
// column followed by a comma, "AS", or the end of the select list.
func assertNoBareHashColumnSelected(t *testing.T, id, sql string) {
	t.Helper()
	sel := selectListOf(sql)
	for _, tok := range hashColumnTokens {
		// A bare projected hash column looks like ", authentication_string," or
		// ", password as ..." or "select password," — i.e. the token bounded by a
		// comma/start and a comma/AS/end, NOT immediately followed by a comparison
		// operator (which marks it as part of the computed boolean).
		re := regexp.MustCompile(`(^|,)\s*` + regexp.QuoteMeta(tok) + `\s*(,|\bas\b|$)`)
		if re.MatchString(sel) {
			t.Errorf("collector %q selects bare hash column %q in its select list: %q", id, tok, sel)
		}
	}
}

// hashShapedValue reports whether v looks like a stored password hash (a MySQL
// *HEX hash, a $...$ phc string, or a long opaque token) — used to prove no such
// value leaked into the normalized evidence.
func hashShapedValue(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	if strings.HasPrefix(s, "*") && len(s) >= 40 { // mysql_native_password *HEX40
		return true
	}
	if strings.HasPrefix(s, "$") { // caching_sha2 / argon2 phc-style
		return true
	}
	return false
}

// assertEvidenceSecretFree fails if any row key is a hash column, or any value is
// hash-shaped. The has_empty_password boolean and non-secret metadata are allowed.
func assertEvidenceSecretFree(t *testing.T, id string, rows []map[string]any) {
	t.Helper()
	for _, row := range rows {
		for k, v := range row {
			kl := strings.ToLower(k)
			for _, tok := range hashColumnTokens {
				if kl == tok {
					t.Errorf("collector %q evidence carries forbidden hash column key %q", id, k)
				}
			}
			if hashShapedValue(v) {
				t.Errorf("collector %q evidence carries a hash-shaped value under key %q: %v", id, k, v)
			}
		}
	}
}

// --- MySQL ------------------------------------------------------------------

// TC-MYC-19 (MYC-R016, INV-MYC-04): the user-accounts collector is secret-free
// with a computed empty-password flag.
func TestMySQLUserAccountsSecretFree(t *testing.T) {
	c := mustCollector(t, mysqlCatalog(), "mysql_user_accounts_v1")
	if HasWrite(c.SQL) {
		t.Errorf("mysql_user_accounts_v1 must be a read-only SELECT: %q", c.SQL)
	}
	if c.HighSensitivity {
		t.Error("mysql_user_accounts_v1 must not be HighSensitivity (metadata only)")
	}
	low := strings.ToLower(c.SQL)
	if !strings.Contains(low, "mysql.user") {
		t.Errorf("mysql_user_accounts_v1 must read mysql.user, got %q", c.SQL)
	}
	// Reads the hash-free projection + derived flag.
	for _, col := range []string{"user", "host", "plugin", "has_empty_password"} {
		if !strings.Contains(low, col) {
			t.Errorf("mysql_user_accounts_v1 must carry %q, got %q", col, c.SQL)
		}
	}
	// The hash column is never a bare selected column (only inside the boolean).
	assertNoBareHashColumnSelected(t, c.ID, c.SQL)

	// A fixture mirroring mysql.user: root@%, anonymous, a passwordless account.
	// The runner returns ONLY the hash-free projection the SQL asks for — if the
	// implementation ever selected the hash, this fixture would expose it; the
	// evidence guard asserts it never appears.
	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(strings.ToLower(sql), "mysql.user") {
			return []map[string]any{
				{"User": "root", "Host": "%", "plugin": "caching_sha2_password", "has_empty_password": int64(0)},
				{"User": "", "Host": "localhost", "plugin": "mysql_native_password", "has_empty_password": int64(1)},
				{"User": "app", "Host": "10.0.0.0/255.0.0.0", "plugin": "mysql_native_password", "has_empty_password": int64(1)},
			}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 3 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if rs[0].Err != "" {
		t.Errorf("collector errored: %q", rs[0].Err)
	}
	assertEvidenceSecretFree(t, c.ID, rs[0].Rows)
	// The empty-password fact is carried as the derived flag.
	if _, ok := rs[0].Rows[0]["has_empty_password"]; !ok {
		t.Error("evidence must carry the has_empty_password flag")
	}
}

// TC-MYC-20 (MYC-R017): the user-privileges collector carries grantee/privilege/
// grantable and degrades on a permission denial.
func TestMySQLUserPrivilegesCollector(t *testing.T) {
	c := mustCollector(t, mysqlCatalog(), "mysql_user_privileges_v1")
	if HasWrite(c.SQL) {
		t.Errorf("mysql_user_privileges_v1 must be a read-only SELECT: %q", c.SQL)
	}
	if c.HighSensitivity {
		t.Error("mysql_user_privileges_v1 must not be HighSensitivity")
	}
	low := strings.ToLower(c.SQL)
	if !strings.Contains(low, "information_schema.user_privileges") {
		t.Errorf("must read information_schema.USER_PRIVILEGES, got %q", c.SQL)
	}
	for _, col := range []string{"grantee", "privilege_type", "is_grantable"} {
		if !strings.Contains(low, col) {
			t.Errorf("must carry %q, got %q", col, c.SQL)
		}
	}
	assertNoBareHashColumnSelected(t, c.ID, c.SQL)

	runner := func(_ context.Context, _ string) ([]map[string]any, error) {
		return []map[string]any{
			{"GRANTEE": "'root'@'%'", "PRIVILEGE_TYPE": "SELECT", "IS_GRANTABLE": "YES"},
			{"GRANTEE": "'app'@'10.%'", "PRIVILEGE_TYPE": "SELECT", "IS_GRANTABLE": "NO"},
		}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 2 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if rs[0].Rows[0]["IS_GRANTABLE"] != "YES" {
		t.Errorf("WITH GRANT OPTION row must carry IS_GRANTABLE=YES, got %+v", rs[0].Rows[0])
	}
	assertEvidenceSecretFree(t, c.ID, rs[0].Rows)

	// A permission denial is a non-fatal Err skip (MYC-R002), never a cycle fail.
	deny := func(_ context.Context, _ string) ([]map[string]any, error) {
		return nil, errAccessDenied{}
	}
	rsd := Collect(context.Background(), deny, []Collector{*c}, true, false)
	if len(rsd) != 1 || rsd[0].Err == "" {
		t.Fatalf("permission denial must be captured in Err, got %+v", rsd)
	}
}

// errAccessDenied is a MySQL-wire-style permission error.
type errAccessDenied struct{}

func (errAccessDenied) Error() string {
	return "Error 1142: SELECT command denied to user 'signals_ro'@'%' for table 'user'"
}

// TC-MYC-21 (INV-MYC-04): no MySQL collector selects a password/hash column.
func TestMySQLCatalogNoPasswordColumn(t *testing.T) {
	for _, c := range mysqlCatalog() {
		assertNoBareHashColumnSelected(t, c.ID, c.SQL)
	}
}

// --- MariaDB ----------------------------------------------------------------

// TC-MDC-24 (MDC-R018, INV-MDC-04): the user-accounts collector is secret-free
// with a computed empty-password flag.
func TestMariaDBUserAccountsSecretFree(t *testing.T) {
	c := mustCollector(t, mariadbCatalog(), "mariadb_user_accounts_v1")
	if HasWrite(c.SQL) {
		t.Errorf("mariadb_user_accounts_v1 must be a read-only SELECT: %q", c.SQL)
	}
	if c.HighSensitivity {
		t.Error("mariadb_user_accounts_v1 must not be HighSensitivity (metadata only)")
	}
	low := strings.ToLower(c.SQL)
	if !strings.Contains(low, "mysql.user") {
		t.Errorf("mariadb_user_accounts_v1 must read the mysql.user view, got %q", c.SQL)
	}
	for _, col := range []string{"user", "host", "plugin", "has_empty_password"} {
		if !strings.Contains(low, col) {
			t.Errorf("mariadb_user_accounts_v1 must carry %q, got %q", col, c.SQL)
		}
	}
	assertNoBareHashColumnSelected(t, c.ID, c.SQL)

	runner := func(_ context.Context, sql string) ([]map[string]any, error) {
		if strings.Contains(strings.ToLower(sql), "mysql.user") {
			return []map[string]any{
				{"User": "root", "Host": "%", "plugin": "mysql_native_password", "has_empty_password": int64(0)},
				{"User": "", "Host": "localhost", "plugin": "mysql_native_password", "has_empty_password": int64(1)},
				{"User": "svc", "Host": "%", "plugin": "", "has_empty_password": int64(1)},
			}, nil
		}
		return []map[string]any{}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 3 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if rs[0].Err != "" {
		t.Errorf("collector errored: %q", rs[0].Err)
	}
	assertEvidenceSecretFree(t, c.ID, rs[0].Rows)
	if _, ok := rs[0].Rows[0]["has_empty_password"]; !ok {
		t.Error("evidence must carry the has_empty_password flag")
	}
}

// TC-MDC-24 (MDC-R018): the MariaDB accounts collector reads the lowest-exposure
// mysql.user VIEW and must NOT read mysql.global_priv (whose JSON embeds the hash).
func TestMariaDBUserAccountsReadsViewNotGlobalPriv(t *testing.T) {
	c := mustCollector(t, mariadbCatalog(), "mariadb_user_accounts_v1")
	low := strings.ToLower(c.SQL)
	if strings.Contains(low, "global_priv") {
		t.Errorf("mariadb_user_accounts_v1 must NOT read mysql.global_priv (its JSON embeds the hash): %q", c.SQL)
	}
	if !strings.Contains(low, "mysql.user") {
		t.Errorf("mariadb_user_accounts_v1 must read the hash-free mysql.user view, got %q", c.SQL)
	}
}

// TC-MDC-25 (MDC-R019): the user-privileges collector carries grantee/privilege/
// grantable and degrades on a permission denial.
func TestMariaDBUserPrivilegesCollector(t *testing.T) {
	c := mustCollector(t, mariadbCatalog(), "mariadb_user_privileges_v1")
	if HasWrite(c.SQL) {
		t.Errorf("mariadb_user_privileges_v1 must be a read-only SELECT: %q", c.SQL)
	}
	if c.HighSensitivity {
		t.Error("mariadb_user_privileges_v1 must not be HighSensitivity")
	}
	low := strings.ToLower(c.SQL)
	if !strings.Contains(low, "information_schema.user_privileges") {
		t.Errorf("must read information_schema.USER_PRIVILEGES, got %q", c.SQL)
	}
	for _, col := range []string{"grantee", "privilege_type", "is_grantable"} {
		if !strings.Contains(low, col) {
			t.Errorf("must carry %q, got %q", col, c.SQL)
		}
	}
	assertNoBareHashColumnSelected(t, c.ID, c.SQL)

	runner := func(_ context.Context, _ string) ([]map[string]any, error) {
		return []map[string]any{
			{"GRANTEE": "'root'@'%'", "PRIVILEGE_TYPE": "ALL PRIVILEGES", "IS_GRANTABLE": "YES"},
		}, nil
	}
	rs := Collect(context.Background(), runner, []Collector{*c}, true, false)
	if len(rs) != 1 || len(rs[0].Rows) != 1 {
		t.Fatalf("unexpected result: %+v", rs)
	}
	if rs[0].Rows[0]["IS_GRANTABLE"] != "YES" {
		t.Errorf("WITH GRANT OPTION row must carry IS_GRANTABLE=YES, got %+v", rs[0].Rows[0])
	}
	assertEvidenceSecretFree(t, c.ID, rs[0].Rows)

	deny := func(_ context.Context, _ string) ([]map[string]any, error) {
		return nil, errAccessDenied{}
	}
	rsd := Collect(context.Background(), deny, []Collector{*c}, true, false)
	if len(rsd) != 1 || rsd[0].Err == "" {
		t.Fatalf("permission denial must be captured in Err, got %+v", rsd)
	}
}

// TC-MDC-26 (INV-MDC-04): no MariaDB collector selects a password/hash column
// and no collector reads mysql.global_priv.
func TestMariaDBCatalogNoPasswordColumn(t *testing.T) {
	for _, c := range mariadbCatalog() {
		assertNoBareHashColumnSelected(t, c.ID, c.SQL)
		if strings.Contains(strings.ToLower(c.SQL), "global_priv") {
			t.Errorf("collector %q reads mysql.global_priv (hash-embedding JSON): %q", c.ID, c.SQL)
		}
	}
}
