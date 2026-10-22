package harness

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"
)

// DB is a thin, dialect-aware wrapper used only to purge and seed fixtures directly in the target's database.
// It is independent of the implementation under test on purpose.
type DB struct {
	*sql.DB
	Driver string // sqlite, postgres, mysql, maria, mssql
}

func OpenDB(driver, dsn string) (*DB, error) {
	goDriver := map[string]string{
		"sqlite":   "sqlite",
		"postgres": "pgx",
		"mysql":    "mysql",
		"maria":    "mysql",
		"mssql":    "sqlserver",
	}[driver]
	if goDriver == "" {
		return nil, fmt.Errorf("unsupported DB driver %q", driver)
	}
	conn, err := sql.Open(goDriver, dsn)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		conn.SetMaxOpenConns(1)
		if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
			return nil, err
		}
	}
	return &DB{DB: conn, Driver: driver}, conn.Ping()
}

// Rebind converts `?` placeholders into the target dialect's syntax.
func (d *DB) Rebind(query string) string {
	if d.Driver != "postgres" && d.Driver != "mssql" {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r != '?' {
			b.WriteRune(r)
			continue
		}
		n++
		if d.Driver == "postgres" {
			b.WriteString("$" + strconv.Itoa(n))
		} else {
			b.WriteString("@p" + strconv.Itoa(n))
		}
	}
	return b.String()
}

// Quote quotes an identifier (needed for reserved words like `key`, `name`, `date`).
func (d *DB) Quote(ident string) string {
	switch d.Driver {
	case "mysql", "maria":
		return "`" + ident + "`"
	case "mssql":
		return "[" + ident + "]"
	default:
		return `"` + ident + `"`
	}
}

func (d *DB) Exec(query string, args ...any) error {
	_, err := d.DB.Exec(d.Rebind(query), args...)
	if err != nil {
		return fmt.Errorf("%w: %s", err, query)
	}
	return nil
}

// Insert inserts a row and returns its generated id.
func (d *DB) Insert(table string, cols []string, vals ...any) (int64, error) {
	quoted := make([]string, len(cols))
	marks := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = d.Quote(c)
		marks[i] = "?"
	}
	base := fmt.Sprintf("INSERT INTO %s (%s)", table, strings.Join(quoted, ", "))
	values := fmt.Sprintf(" VALUES (%s)", strings.Join(marks, ", "))
	switch d.Driver {
	case "postgres":
		var id int64
		err := d.DB.QueryRow(d.Rebind(base+values+" RETURNING id"), vals...).Scan(&id)
		return id, wrap(err, table)
	case "mssql":
		var id int64
		err := d.DB.QueryRow(d.Rebind(base+" OUTPUT INSERTED.id"+values), vals...).Scan(&id)
		return id, wrap(err, table)
	default:
		res, err := d.DB.Exec(base+values, vals...)
		if err != nil {
			return 0, wrap(err, table)
		}
		return res.LastInsertId()
	}
}

// InsertNoID inserts a row in a table without an id column (join tables).
func (d *DB) InsertNoID(table string, cols []string, vals ...any) error {
	marks := make([]string, len(cols))
	for i := range cols {
		marks[i] = "?"
	}
	return d.Exec(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ", "), strings.Join(marks, ", ")), vals...)
}

func wrap(err error, table string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("insert into %s: %w", table, err)
}

// shlinkTables lists every Shlink table in an order where children come before parents, so rows can be deleted
// without violating foreign keys.
var shlinkTables = []string{
	"redirect_conditions_in_short_url_redirect_rules",
	"redirect_conditions",
	"short_url_redirect_rules",
	"short_url_visits_counts",
	"orphan_visits_counts",
	"visits",
	"visit_locations",
	"short_urls_in_tags",
	"short_urls",
	"tags",
	"api_key_roles",
	"api_keys",
	"domains",
	"geolocation_db_updates",
}

// Purge deletes every row from every Shlink table, the same way Doctrine's ORMPurger does (DELETE, no truncation, so
// auto-increment counters keep growing).
func (d *DB) Purge() error {
	for _, t := range shlinkTables {
		if err := d.Exec("DELETE FROM " + t); err != nil {
			return err
		}
	}
	return nil
}
