package db

import "testing"

func TestFromTableName(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()

	cases := []struct {
		sql  string
		want string
	}{
		{`SELECT * FROM users`, "users"},
		{`select * from schema1.tbl2`, "schema1.tbl2"},
		{`SELECT * FROM "public"."django_session"`, "public.django_session"},
		{`SELECT a.* FROM users a JOIN orders o ON a.id = o.uid`, "users"},
		{`SELECT * FROM (SELECT 1) t`, ""},
		{`WITH c AS (SELECT 1) SELECT * FROM c`, "c"},
		{`SELECT 1`, ""},
		{``, ""},
		{`not even sql`, ""},
		{`SELECT * FROM t WHERE x = 1 ORDER BY y`, "t"},
	}
	for _, c := range cases {
		got := FromTableName(d, c.sql)
		if got != c.want {
			t.Errorf("FromTableName(%q) = %q, want %q", c.sql, got, c.want)
		}
	}
}

func TestFromTableNameNilDB(t *testing.T) {
	if got := FromTableName(nil, "SELECT * FROM x"); got != "" {
		t.Errorf("nil DB should return empty, got %q", got)
	}
}
