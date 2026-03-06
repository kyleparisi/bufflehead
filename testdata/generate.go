//go:build ignore

package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/marcboeker/go-duckdb"
)

func main() {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		COPY (
			SELECT 
				i AS id,
				'user_' || i AS name,
				(random() * 100)::INT AS score,
				CASE WHEN i % 3 = 0 THEN 'admin' WHEN i % 3 = 1 THEN 'editor' ELSE 'viewer' END AS role,
				DATE '2024-01-01' + INTERVAL (i) DAY AS created_at
			FROM generate_series(1, 500) t(i)
		) TO 'testdata/sample.parquet' (FORMAT PARQUET)
	`)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote testdata/sample.parquet (500 rows)")
}
