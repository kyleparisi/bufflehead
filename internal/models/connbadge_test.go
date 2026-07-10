package models

import "testing"

func TestConnBadge(t *testing.T) {
	cases := map[string]string{
		"Memory":               "mem",
		":memory:":             "mem",
		"in-memory":            "mem",
		"sales.parquet":        "SA",
		"analytics_warehouse":  "AW",
		"prod-db":              "PD",
		"prod":                 "PR",
		"analytics warehouse":  "AW",
		"x":                    "X",
		"warehouse":            "WA",
		"customers.duckdb":     "CU",
		"a_b_c":                "AB",
	}
	for in, want := range cases {
		if got := ConnBadge(in); got != want {
			t.Errorf("ConnBadge(%q) = %q, want %q", in, got, want)
		}
	}
}
