// Package schema holds the schema-diff gate: the database a target creates and migrates must have exactly the
// structure upstream's own db:create + db:migrate produce.
package schema

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// Snapshot is a normalized description of a database schema, comparable across implementations.
type Snapshot struct {
	Tables     map[string][]string `json:"tables"`
	Indexes    map[string][]string `json:"indexes"`
	ForeignKey map[string][]string `json:"foreignKeys"`
	Migrations []string            `json:"migrations"`
}

func queryRows(db *h.DB, query string, args ...any) ([][]string, error) {
	rows, err := db.Query(db.Rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out [][]string
	for rows.Next() {
		raw := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range raw {
			if v.Valid {
				row[i] = v.String
			} else {
				row[i] = "NULL"
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func group(rows [][]string) map[string][]string {
	out := map[string][]string{}
	for _, r := range rows {
		out[strings.ToLower(r[0])] = append(out[strings.ToLower(r[0])], strings.Join(r[1:], " | "))
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// Take introspects the connected database.
func Take(db *h.DB) (*Snapshot, error) {
	var tables, indexes, fks [][]string
	var err error
	switch db.Driver {
	case "postgres":
		if tables, err = queryRows(db, `SELECT table_name, column_name, udt_name, COALESCE(character_maximum_length::text, ''),
			is_nullable, COALESCE(column_default, ''), COALESCE(identity_generation, '')
			FROM information_schema.columns WHERE table_schema = current_schema()`); err != nil {
			return nil, err
		}
		if indexes, err = queryRows(db, `SELECT tablename, indexdef FROM pg_indexes WHERE schemaname = current_schema()`); err != nil {
			return nil, err
		}
		fks, err = queryRows(db, `SELECT tc.table_name, tc.constraint_name, kcu.column_name, ccu.table_name, rc.delete_rule
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
			JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name AND ccu.table_schema = tc.table_schema
			JOIN information_schema.referential_constraints rc ON rc.constraint_name = tc.constraint_name AND rc.constraint_schema = tc.table_schema
			WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = current_schema()`)
	case "mysql", "maria":
		if tables, err = queryRows(db, `SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COALESCE(COLUMN_DEFAULT, ''),
			EXTRA, COALESCE(CHARACTER_SET_NAME, ''), COALESCE(COLLATION_NAME, '')
			FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()`); err != nil {
			return nil, err
		}
		if indexes, err = queryRows(db, `SELECT TABLE_NAME, INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX, COLUMN_NAME, COALESCE(SUB_PART, '')
			FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE()`); err != nil {
			return nil, err
		}
		fks, err = queryRows(db, `SELECT k.TABLE_NAME, k.CONSTRAINT_NAME, k.COLUMN_NAME, k.REFERENCED_TABLE_NAME, r.DELETE_RULE
			FROM information_schema.KEY_COLUMN_USAGE k JOIN information_schema.REFERENTIAL_CONSTRAINTS r
			ON r.CONSTRAINT_NAME = k.CONSTRAINT_NAME AND r.CONSTRAINT_SCHEMA = k.TABLE_SCHEMA
			WHERE k.TABLE_SCHEMA = DATABASE() AND k.REFERENCED_TABLE_NAME IS NOT NULL`)
	case "mssql":
		if tables, err = queryRows(db, `SELECT t.name, c.name, ty.name, CAST(c.max_length AS varchar), CAST(c.is_nullable AS varchar),
			COALESCE(dc.definition, ''), CAST(c.is_identity AS varchar)
			FROM sys.columns c JOIN sys.tables t ON t.object_id = c.object_id JOIN sys.types ty ON ty.user_type_id = c.user_type_id
			LEFT JOIN sys.default_constraints dc ON dc.object_id = c.default_object_id`); err != nil {
			return nil, err
		}
		if indexes, err = queryRows(db, `SELECT t.name, i.name, CAST(i.is_unique AS varchar), CAST(ic.key_ordinal AS varchar), c.name,
			COALESCE(i.filter_definition, '')
			FROM sys.indexes i JOIN sys.tables t ON t.object_id = i.object_id
			JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
			JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
			WHERE i.is_primary_key = 0 AND i.name IS NOT NULL`); err != nil {
			return nil, err
		}
		fks, err = queryRows(db, `SELECT t.name, fk.name, c.name, rt.name, fk.delete_referential_action_desc
			FROM sys.foreign_keys fk JOIN sys.tables t ON t.object_id = fk.parent_object_id
			JOIN sys.tables rt ON rt.object_id = fk.referenced_object_id
			JOIN sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
			JOIN sys.columns c ON c.object_id = fkc.parent_object_id AND c.column_id = fkc.parent_column_id`)
	default:
		if tables, err = queryRows(db, `SELECT m.name, p.name, p.type, CAST(p."notnull" AS TEXT), COALESCE(p.dflt_value, ''), CAST(p.pk AS TEXT)
			FROM sqlite_master m JOIN pragma_table_info(m.name) p WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'`); err != nil {
			return nil, err
		}
		if indexes, err = queryRows(db, `SELECT tbl_name, COALESCE(sql, name) FROM sqlite_master WHERE type = 'index'`); err != nil {
			return nil, err
		}
		fks, err = queryRows(db, `SELECT m.name, f."from", f."table", f."to", f.on_delete
			FROM sqlite_master m JOIN pragma_foreign_key_list(m.name) f WHERE m.type = 'table'`)
	}
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{Tables: group(tables), Indexes: group(normalizeIndexes(db.Driver, indexes)), ForeignKey: group(fks)}
	migrations, err := queryRows(db, "SELECT version FROM migrations")
	if err != nil {
		return nil, fmt.Errorf("reading migrations table: %w", err)
	}
	for _, m := range migrations {
		snap.Migrations = append(snap.Migrations, m[0])
	}
	sort.Strings(snap.Migrations)
	return snap, nil
}

var spaces = regexp.MustCompile(`\s+`)

// normalizeIndexes collapses whitespace in index definitions, which engines keep verbatim.
func normalizeIndexes(driver string, rows [][]string) [][]string {
	for _, r := range rows {
		for i := range r {
			r[i] = spaces.ReplaceAllString(strings.TrimSpace(r[i]), " ")
		}
	}
	return rows
}
