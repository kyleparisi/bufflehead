package db

import (
	"fmt"
	"regexp"
)

// Extension describes a DuckDB extension and its install/load state.
type Extension struct {
	Name        string
	Description string
	Loaded      bool // currently loaded into this session
	Installed   bool // present on disk (installed)
}

// extNamePattern guards INSTALL/LOAD against SQL injection — DuckDB extension
// names are simple identifiers.
var extNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// Extensions lists all DuckDB extensions known to this connection, ordered by
// name, with their loaded/installed state.
func (d *DB) Extensions() ([]Extension, error) {
	rows, err := d.conn.Query(`
		SELECT extension_name, loaded, installed, coalesce(description, '')
		FROM duckdb_extensions()
		ORDER BY extension_name`)
	if err != nil {
		return nil, fmt.Errorf("list extensions: %w", err)
	}
	defer rows.Close()

	var exts []Extension
	for rows.Next() {
		var e Extension
		if err := rows.Scan(&e.Name, &e.Loaded, &e.Installed, &e.Description); err != nil {
			return nil, err
		}
		exts = append(exts, e)
	}
	return exts, rows.Err()
}

// InstallExtension runs INSTALL for the named extension (downloads it). The name
// is validated to be a plain identifier before interpolation.
func (d *DB) InstallExtension(name string) error {
	if !extNamePattern.MatchString(name) {
		return fmt.Errorf("invalid extension name %q", name)
	}
	if _, err := d.conn.Exec("INSTALL " + name); err != nil {
		return fmt.Errorf("install %s: %w", name, err)
	}
	return nil
}

// LoadExtension runs LOAD for the named extension (loads it into the session).
func (d *DB) LoadExtension(name string) error {
	if !extNamePattern.MatchString(name) {
		return fmt.Errorf("invalid extension name %q", name)
	}
	if _, err := d.conn.Exec("LOAD " + name); err != nil {
		return fmt.Errorf("load %s: %w", name, err)
	}
	return nil
}
