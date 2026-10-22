package db

import (
	"context"
	"database/sql"
	"strings"
)

// TableNames lists the tables of the connected database.
func (d *DB) TableNames(ctx context.Context) ([]string, error) {
	var query string
	switch d.Driver {
	case Postgres:
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'"
	case MySQL, Maria:
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'"
	case MSSQL:
		query = "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE = 'BASE TABLE' AND TABLE_CATALOG = DB_NAME()"
	default:
		query = "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
	}
	return d.strings(ctx, query)
}

func (d *DB) strings(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := d.Query(ctx, d.DB, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// HasTable tells if a table exists.
func (d *DB) HasTable(ctx context.Context, table string) (bool, error) {
	tables, err := d.TableNames(ctx)
	if err != nil {
		return false, err
	}
	for _, t := range tables {
		if strings.EqualFold(t, table) {
			return true, nil
		}
	}
	return false, nil
}

// IndexNames lists the index names of a table (primary keys excluded where the engine names them implicitly).
func (d *DB) IndexNames(ctx context.Context, table string) ([]string, error) {
	switch d.Driver {
	case Postgres:
		return d.strings(ctx, "SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ?", table)
	case MySQL, Maria:
		return d.strings(ctx, "SELECT DISTINCT index_name FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ?", table)
	case MSSQL:
		return d.strings(ctx, "SELECT i.name FROM sys.indexes i JOIN sys.tables t ON i.object_id = t.object_id WHERE t.name = ? AND i.name IS NOT NULL", table)
	default:
		rows, err := d.DB.QueryContext(ctx, "SELECT name FROM pragma_index_list(?)", table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, rows.Err()
	}
}

// HasIndex tells if a table has an index with given name (case-insensitive, like Doctrine's comparison).
func (d *DB) HasIndex(ctx context.Context, table, index string) (bool, error) {
	names, err := d.IndexNames(ctx, table)
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if strings.EqualFold(n, index) {
			return true, nil
		}
	}
	return false, nil
}

// Column describes a table column as reported by the engine.
type Column struct {
	Name     string
	Type     string
	Nullable bool
}

// Columns lists the columns of a table.
func (d *DB) Columns(ctx context.Context, table string) ([]Column, error) {
	var rows *sql.Rows
	var err error
	switch d.Driver {
	case Postgres:
		rows, err = d.Query(ctx, d.DB, "SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? ORDER BY ordinal_position", table)
	case MySQL, Maria:
		rows, err = d.Query(ctx, d.DB, "SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ordinal_position", table)
	case MSSQL:
		rows, err = d.Query(ctx, d.DB, "SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = ? ORDER BY ORDINAL_POSITION", table)
	default:
		rows, err = d.DB.QueryContext(ctx, "SELECT name, type, CASE WHEN \"notnull\" = 1 THEN 'NO' ELSE 'YES' END FROM pragma_table_info(?)", table)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var c Column
		var nullable string
		if err := rows.Scan(&c.Name, &c.Type, &nullable); err != nil {
			return nil, err
		}
		c.Nullable = strings.EqualFold(nullable, "YES")
		out = append(out, c)
	}
	return out, rows.Err()
}

// ColumnInfo returns a column of a table, if it exists.
func (d *DB) ColumnInfo(ctx context.Context, table, column string) (*Column, error) {
	cols, err := d.Columns(ctx, table)
	if err != nil {
		return nil, err
	}
	for _, c := range cols {
		if strings.EqualFold(c.Name, column) {
			c := c
			return &c, nil
		}
	}
	return nil, nil
}
