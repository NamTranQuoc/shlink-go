package db

import (
	"context"
	"embed"
	"fmt"
	"strings"
)

// The DDL files (maria.sql equals mysql.sql) are the exact output of `bin/doctrine orm:schema-tool:create --dump-sql` from Shlink 5.1.8 for each
// engine, so a database created by this port is identical to one created by upstream.
//
//go:embed schema/*.sql
var schemaFS embed.FS

// ShlinkTables are the tables Shlink's ORM metadata defines.
var ShlinkTables = []string{
	"orphan_visits_counts", "short_urls", "short_urls_in_tags", "visits", "short_url_redirect_rules",
	"redirect_conditions_in_short_url_redirect_rules", "redirect_conditions", "tags", "visit_locations", "domains",
	"short_url_visits_counts", "geolocation_db_updates", "api_keys", "api_key_roles",
}

func (d *DB) schemaFile() string {
	if d.Driver == Maria {
		return "schema/maria.sql"
	}
	return "schema/" + string(d.Driver) + ".sql"
}

// CreateSchemaStatements returns the statements that create Shlink's tables from scratch.
func (d *DB) CreateSchemaStatements() ([]string, error) {
	raw, err := schemaFS.ReadFile(d.schemaFile())
	if err != nil {
		return nil, fmt.Errorf("no schema available for driver %s: %w", d.Driver, err)
	}
	var stmts []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		stmts = append(stmts, strings.TrimSuffix(line, ";"))
	}
	return stmts, nil
}

// CreateSchema creates all Shlink tables.
func (d *DB) CreateSchema(ctx context.Context) error {
	stmts, err := d.CreateSchemaStatements()
	if err != nil {
		return err
	}
	for _, stmt := range stmts {
		if _, err := d.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%w: %s", err, stmt)
		}
	}
	return nil
}

// AnyShlinkTableExists tells if at least one Shlink table exists, which is how upstream's db:create decides the
// database was already created.
func (d *DB) AnyShlinkTableExists(ctx context.Context) (bool, error) {
	tables, err := d.TableNames(ctx)
	if err != nil {
		return false, err
	}
	for _, existing := range tables {
		for _, t := range ShlinkTables {
			if existing == t {
				return true, nil
			}
		}
	}
	return false, nil
}

// CreateDatabase creates the configured database through a connection that is not bound to it.
func CreateDatabase(ctx context.Context, p Params) error {
	if p.Driver == SQLite {
		return nil
	}
	conn, err := Open(p, false)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.ExecContext(ctx, "CREATE DATABASE "+p.Name)
	return err
}
