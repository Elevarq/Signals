-- Elevarq Signals local-dev seed.
-- Runs automatically the first time the PostgreSQL container starts
-- (docker-entrypoint-initdb.d). It creates the monitoring role AND a
-- small but *representative* schema so the collectors actually have
-- something to report: several constraint types (so pg_constraints_v1
-- exercises the internal-"char" wire contract — contype 'p'/'f'/'u'/'c'
-- must appear as single-char STRINGS, not byte integers), an
-- intentionally UNINDEXED foreign key (so the missing-FK-index signal
-- has a real target), plus a plain PK table.

-- --- Monitoring role (non-superuser, pg_monitor) ---------------------
-- Named `signals` to match docs/database-connections.md.
CREATE ROLE signals WITH LOGIN PASSWORD 'monitor_pass';
GRANT pg_monitor TO signals;

CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- --- Representative schema with varied constraints -------------------
-- customers: PRIMARY KEY (contype 'p'), UNIQUE (contype 'u'), CHECK (contype 'c')
CREATE TABLE customers (
    id    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email text NOT NULL UNIQUE,
    tier  text NOT NULL DEFAULT 'starter'
              CHECK (tier IN ('starter','professional','business','enterprise'))
);

-- orders: FOREIGN KEY (contype 'f') left deliberately UNINDEXED so the
-- missing-FK-index detection path has a real target; plus a CHECK.
CREATE TABLE orders (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id  bigint NOT NULL REFERENCES customers(id),   -- contype 'f', UNINDEXED on purpose
    amount_cents bigint NOT NULL CHECK (amount_cents >= 0),  -- contype 'c'
    status       text   NOT NULL DEFAULT 'open',
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX orders_status_idx ON orders(status);           -- a normal (non-covering) index

-- order_items: a second FK + a composite UNIQUE.
CREATE TABLE order_items (
    order_id bigint NOT NULL REFERENCES orders(id),          -- contype 'f'
    line_no  int    NOT NULL,
    sku      text   NOT NULL,
    qty      int    NOT NULL CHECK (qty > 0),                -- contype 'c'
    UNIQUE (order_id, line_no)                               -- contype 'u'
);

-- Plain PK table (keeps the original example around).
CREATE TABLE example_data (
    id         serial PRIMARY KEY,
    value      text NOT NULL,
    created_at timestamptz DEFAULT now()
);

-- --- NOT VALID constraints (pg_constraints_v1.is_validated = false) --
-- A constraint added NOT VALID is enforced for new writes but was never
-- checked against existing rows, so pg_constraint.convalidated is false.
-- Every constraint above is validated (is_validated = true); these two
-- give the is_validated signal a false case to report as well (#342).

-- Self-referential FK on customers, added NOT VALID.
ALTER TABLE customers ADD COLUMN referred_by bigint;
ALTER TABLE customers
    ADD CONSTRAINT customers_referred_by_fk
    FOREIGN KEY (referred_by) REFERENCES customers(id) NOT VALID;   -- convalidated = false

-- CHECK on orders, added NOT VALID.
ALTER TABLE orders
    ADD CONSTRAINT orders_amount_sane_chk
    CHECK (amount_cents < 1000000000) NOT VALID;                    -- convalidated = false

-- --- Data, so stats/row counts are non-trivial ----------------------
INSERT INTO customers (email, tier)
SELECT 'user' || g || '@example.com',
       (ARRAY['starter','professional','business','enterprise'])[1 + (g % 4)]
FROM generate_series(1, 200) g;

INSERT INTO orders (customer_id, amount_cents, status)
SELECT 1 + (g % 200), (g * 37) % 100000, (ARRAY['open','paid','shipped'])[1 + (g % 3)]
FROM generate_series(1, 5000) g;

INSERT INTO order_items (order_id, line_no, sku, qty)
SELECT 1 + (g % 5000), 1 + (g % 3), 'SKU-' || (g % 500), 1 + (g % 5)
FROM generate_series(1, 15000) g;

INSERT INTO example_data (value)
SELECT 'sample-' || generate_series(1, 100);

ANALYZE;
