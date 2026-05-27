# Acceptance Tests: pg_db_role_settings_v1

## Feature

`specifications/collectors/pg_db_role_settings_v1.md`

## Test Cases

### TC-DBROLESET-01: Database-level settings are emitted

**Rule:** Normal

**Scenario:** A database has a default planner setting set with
`ALTER DATABASE ... SET ...`.

**Given:**
- Database `customer_db`.
- `ALTER DATABASE customer_db SET random_page_cost = 1.2`.

**When:**
- A collection cycle runs.

**Then:**
- A row is present with `database_name = 'customer_db'`.
- `role_name IS NULL`.
- `setting_scope = 'database'`.
- `setconfig` includes `'random_page_cost=1.2'`.

**Expected Result:** Pass when the database-level override is emitted.

---

### TC-DBROLESET-02: Role-level settings are emitted

**Rule:** Normal

**Scenario:** A role has a default setting set with `ALTER ROLE ...
SET ...`.

**Given:**
- Role `app_user`.
- `ALTER ROLE app_user SET work_mem = '64MB'`.

**When:**
- A collection cycle runs.

**Then:**
- A row is present with `database_name IS NULL`.
- `role_name = 'app_user'`.
- `setting_scope = 'role'`.
- `setconfig` includes `'work_mem=64MB'`.

**Expected Result:** Pass when the role-level override is emitted.

---

### TC-DBROLESET-03: Role-in-database settings are emitted

**Rule:** Normal

**Scenario:** A role has a database-specific default setting.

**Given:**
- Role `app_user`.
- Database `customer_db`.
- `ALTER ROLE app_user IN DATABASE customer_db SET effective_cache_size = '12GB'`.

**When:**
- A collection cycle runs.

**Then:**
- A row is present with `database_name = 'customer_db'`.
- `role_name = 'app_user'`.
- `setting_scope = 'role_in_database'`.
- `setconfig` includes `'effective_cache_size=12GB'`.

**Expected Result:** Pass when the role-in-database override is emitted.

---

### TC-DBROLESET-04: Zero-OID scope markers are preserved

**Rule:** Scope interpretation

**Scenario:** A database default uses `setrole = 0`, as stored by
PostgreSQL for `ALTER DATABASE ... SET ...`.

**Given:**
- A row in `pg_db_role_setting` with `setdatabase <> 0` and `setrole = 0`.

**When:**
- A collection cycle runs.

**Then:**
- `role_oid = 0`.
- `role_name IS NULL`.
- `setting_scope = 'database'`.
- `setconfig` is preserved.

**Expected Result:** Pass when zero-OID role scope is represented
without dropping the row.

---

### TC-DBROLESET-05: setconfig preserved as raw text[]

**Rule:** Invariant

**Scenario:** Multiple default settings exist for the same scope.

**Given:**
- A database or role has multiple default settings.

**When:**
- A collection cycle runs.

**Then:**
- `setconfig` is emitted as a raw `text[]`.
- No parsing/splitting is done at collection time.

**Expected Result:** Pass when the raw array is preserved.
