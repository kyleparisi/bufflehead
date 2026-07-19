-- Regenerate testdata/test.sqlite:
--   rm -f testdata/test.sqlite && sqlite3 testdata/test.sqlite < testdata/generate_sqlite.sql
--
-- Mirrors testdata/test.duckdb (users table + user_orders view, 3 rows each)
-- so sqlite handling can be exercised in parallel with the DuckDB fixtures.
-- Note: the app cannot open sqlite files through its native DuckDB path yet;
-- DuckDB reads them via the sqlite extension (INSTALL sqlite; LOAD sqlite;
-- ATTACH '<file>' (TYPE SQLITE)).

CREATE TABLE users (
  id   INTEGER PRIMARY KEY,
  name TEXT    NOT NULL,
  role TEXT    NOT NULL
);
INSERT INTO users (id, name, role) VALUES
  (1, 'alice', 'admin'),
  (2, 'bob',   'editor'),
  (3, 'carol', 'viewer');

CREATE TABLE orders (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id),
  amount     REAL    NOT NULL,
  created_at TEXT    NOT NULL
);
INSERT INTO orders (id, user_id, amount, created_at) VALUES
  (1, 1,  19.99, '2024-01-05'),
  (2, 2, 149.50, '2024-02-11'),
  (3, 1,   5.00, '2024-03-02');

CREATE VIEW user_orders AS
  SELECT u.name AS name, o.amount AS amount, o.created_at AS created_at
  FROM orders o
  JOIN users u ON u.id = o.user_id;
