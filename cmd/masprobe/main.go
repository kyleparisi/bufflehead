package main

import (
	"bufflehead/internal/db"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Result struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

func main() {
	results := []Result{}
	add := func(name string, pass bool, detail string) { results = append(results, Result{name, pass, detail}) }
	outside := os.Getenv("SPIKE_OUTSIDE_FILE")
	_, err := os.ReadFile(outside)
	add("sandbox_denies_ungranted_file", os.IsPermission(err), fmt.Sprint(err))
	temp, err := os.CreateTemp("", "bufflehead-spike-")
	add("container_temp_write", err == nil, fmt.Sprint(err))
	if err == nil {
		temp.Close()
		os.Remove(temp.Name())
	}
	d, err := db.New()
	if err != nil {
		add("duckdb_open", false, err.Error())
	} else {
		defer d.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, name := range []string{"sample.parquet", "sales.csv", "events.json"} {
			query := "SELECT count(*) FROM " + db.QuotePathLiteral(filepath.Join(os.Getenv("SPIKE_FIXTURES"), name))
			r, e := d.Query(ctx, query, 0, 0)
			add("read_"+name, e == nil, fmt.Sprintf("result=%+v error=%v", r, e))
		}
		raw, rawErr := sql.Open("duckdb", "")
		if rawErr != nil {
			panic(rawErr)
		}
		defer raw.Close()
		for _, q := range []string{"INSTALL httpfs", "FORCE INSTALL httpfs", "LOAD '/tmp/not-a-real-extension.duckdb_extension'", "SET autoinstall_known_extensions=true; INSTALL httpfs"} {
			_, e := raw.ExecContext(ctx, q)
			// The engine must reject at compile-time guard, not a network/SQL-wrapper failure.
			detail := fmt.Sprint(e)
			add(q, e != nil && contains(detail, "compile time flag"), detail)
		}
	}
	if s := os.Getenv("SPIKE_PG_PORT"); s != "" {
		port, _ := strconv.Atoi(s)
		pg, e := db.NewPostgresDirect("127.0.0.1", port, "postgres", "spike", "", "disable")
		if e != nil {
			add("postgres_connect", false, e.Error())
		} else {
			defer pg.Close()
			r, e := pg.Query(context.Background(), "SELECT 42 AS sandbox_answer", 0, 10)
			add("postgres_query", e == nil, fmt.Sprintf("result=%+v error=%v", r, e))
			r, e = pg.Query(context.Background(), "SELECT current_setting('default_transaction_read_only')", 0, 10)
			add("postgres_read_only", e == nil && len(r.Rows) > 0 && r.Rows[0][0] == "on", fmt.Sprintf("result=%+v error=%v", r, e))
		}
	}
	json.NewEncoder(os.Stdout).Encode(results)
	for _, r := range results {
		if !r.Pass {
			os.Exit(1)
		}
	}
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
