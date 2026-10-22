// Package db opens the database Shlink is configured with and hides the differences between the supported engines.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"

	"github.com/NamTranQuoc/shlink-go/internal/config"
)

// Driver is one of the values accepted by DB_DRIVER.
type Driver string

const (
	SQLite   Driver = "sqlite"
	Postgres Driver = "postgres"
	MySQL    Driver = "mysql"
	Maria    Driver = "maria"
	MSSQL    Driver = "mssql"
)

// IsMySQLCompatible tells if the driver speaks the MySQL dialect.
func (d Driver) IsMySQLCompatible() bool { return d == MySQL || d == Maria }

// Params are the connection parameters resolved from env vars, like config/autoload/entity-manager.global.php does.
type Params struct {
	Driver        Driver
	Name          string
	User          *string
	Password      *string
	Host          string
	Port          string
	UnixSocket    string
	UseEncryption bool
	SQLitePath    string
}

// ParamsFromEnv resolves connection params from the DB_* env vars.
func ParamsFromEnv() Params {
	driver := Driver(config.DBDriver.Load().AsString())
	switch driver {
	case Postgres, MySQL, Maria, MSSQL:
	default:
		// Upstream maps null and any unknown value other than postgres/mssql/mysql/maria to sqlite or mysql:
		// `null, 'sqlite'` → sqlite; anything else → pdo_mysql. Keep sqlite for null and "sqlite".
		if driver != "" && driver != SQLite {
			driver = MySQL
		} else {
			driver = SQLite
		}
	}
	p := Params{
		Driver:        driver,
		Name:          config.DBName.Load().AsString(),
		User:          config.DBUser.Load().StringOrNil(),
		Password:      config.DBPassword.Load().StringOrNil(),
		Host:          config.DBHost.Load().AsString(),
		Port:          config.DBPort.Load().AsString(),
		UseEncryption: config.DBUseEncryption.Load().AsBool(),
		SQLitePath:    "data/database.sqlite",
	}
	if driver.IsMySQLCompatible() {
		p.UnixSocket = config.DBUnixSocket.Load().AsString()
	}
	return p
}

// DB wraps *sql.DB with the dialect of the configured engine.
type DB struct {
	*sql.DB
	Driver Driver
	Params Params
	// Loc is the timezone dates are stored in (TIMEZONE env var), like PHP's default timezone upstream.
	Loc *time.Location
}

// Open connects to the configured database. If withDBName is false, the connection is not bound to a database (used to
// create it, like upstream's NoDbNameConnection).
func Open(p Params, withDBName bool) (*DB, error) {
	goDriver, dsn := dsnFor(p, withDBName)
	conn, err := sql.Open(goDriver, dsn)
	if err != nil {
		return nil, err
	}
	if p.Driver == SQLite {
		// SQLite allows a single writer; serialize access through one connection. Like upstream's
		// CloseDbConnectionMiddleware, the connection is not kept between uses, so writes made by other processes
		// (CLI commands, tests) are always read from the file.
		conn.SetMaxOpenConns(1)
		conn.SetMaxIdleConns(0)
	} else {
		conn.SetMaxOpenConns(32)
		conn.SetMaxIdleConns(16)
		conn.SetConnMaxIdleTime(5 * time.Minute)
	}
	return &DB{DB: conn, Driver: p.Driver, Params: p, Loc: config.Location()}, nil
}

func dsnFor(p Params, withDBName bool) (string, string) {
	user, pass := "", ""
	if p.User != nil {
		user = *p.User
	}
	if p.Password != nil {
		pass = *p.Password
	}
	switch p.Driver {
	case Postgres:
		u := url.URL{Scheme: "postgres", User: url.UserPassword(user, pass), Host: hostPort(p.Host, p.Port)}
		q := url.Values{}
		if withDBName {
			u.Path = "/" + p.Name
		} else {
			u.Path = "/postgres"
		}
		// Upstream passes DB_USE_ENCRYPTION's sslmode as a PDO driver option, which pdo_pgsql ignores: connections
		// always use libpq's default mode
		q.Set("sslmode", "prefer")
		u.RawQuery = q.Encode()
		return "pgx", u.String()
	case MSSQL:
		u := url.URL{Scheme: "sqlserver", User: url.UserPassword(user, pass), Host: hostPort(p.Host, p.Port)}
		q := url.Values{}
		if withDBName {
			q.Set("database", p.Name)
		}
		q.Set("TrustServerCertificate", "true")
		u.RawQuery = q.Encode()
		return "sqlserver", u.String()
	case MySQL, Maria:
		c := mysql.NewConfig()
		c.User = user
		c.Passwd = pass
		if p.UnixSocket != "" {
			c.Net = "unix"
			c.Addr = p.UnixSocket
		} else {
			c.Net = "tcp"
			c.Addr = hostPort(p.Host, p.Port)
		}
		if withDBName {
			c.DBName = p.Name
		}
		c.Params = map[string]string{"charset": "utf8mb4"}
		if p.UseEncryption {
			c.TLSConfig = "skip-verify"
		}
		return "mysql", c.FormatDSN()
	default:
		return "sqlite", "file:" + p.SQLitePath + "?_pragma=busy_timeout(10000)&_time_format=sqlite"
	}
}

func hostPort(host, port string) string {
	if port == "" {
		return host
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return host + ":" + port
}

// Rebind converts `?` placeholders to the engine's syntax. Question marks inside quoted literals are not expected in
// Shlink queries.
func (d *DB) Rebind(query string) string {
	if d.Driver != Postgres && d.Driver != MSSQL {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 16)
	n := 0
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c != '?' {
			b.WriteByte(c)
			continue
		}
		n++
		if d.Driver == Postgres {
			b.WriteByte('$')
		} else {
			b.WriteString("@p")
		}
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}

// Q quotes an identifier.
func (d *DB) Q(ident string) string {
	switch d.Driver {
	case MySQL, Maria:
		return "`" + ident + "`"
	case MSSQL:
		return "[" + ident + "]"
	default:
		return `"` + ident + `"`
	}
}

// Querier is implemented by *sql.DB, *sql.Tx and *sql.Conn.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Exec runs a statement with `?` placeholders.
func (d *DB) Exec(ctx context.Context, q Querier, query string, args ...any) (sql.Result, error) {
	return q.ExecContext(ctx, d.Rebind(query), args...)
}

// Query runs a query with `?` placeholders.
func (d *DB) Query(ctx context.Context, q Querier, query string, args ...any) (*sql.Rows, error) {
	return q.QueryContext(ctx, d.Rebind(query), args...)
}

// QueryRow runs a single-row query with `?` placeholders.
func (d *DB) QueryRow(ctx context.Context, q Querier, query string, args ...any) *sql.Row {
	return q.QueryRowContext(ctx, d.Rebind(query), args...)
}

// Insert inserts a row and returns the generated id.
func (d *DB) Insert(ctx context.Context, q Querier, table string, cols []string, vals ...any) (int64, error) {
	quoted := make([]string, len(cols))
	marks := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = d.Q(c)
		marks[i] = "?"
	}
	head := "INSERT INTO " + table + " (" + strings.Join(quoted, ", ") + ")"
	values := " VALUES (" + strings.Join(marks, ", ") + ")"
	switch d.Driver {
	case Postgres:
		var id int64
		err := d.QueryRow(ctx, q, head+values+" RETURNING id", vals...).Scan(&id)
		return id, err
	case MSSQL:
		var id int64
		err := d.QueryRow(ctx, q, head+" OUTPUT INSERTED.id"+values, vals...).Scan(&id)
		return id, err
	default:
		res, err := d.Exec(ctx, q, head+values, vals...)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
}

// Limit appends the engine's LIMIT/OFFSET syntax to a query that already has an ORDER BY (required by MS SQL).
// A nil limit means no limit.
func (d *DB) Limit(query string, limit, offset *int) string {
	if limit == nil && (offset == nil || *offset == 0) {
		return query
	}
	off := 0
	if offset != nil {
		off = *offset
	}
	switch d.Driver {
	case MSSQL:
		s := fmt.Sprintf("%s OFFSET %d ROWS", query, off)
		if limit != nil {
			s += fmt.Sprintf(" FETCH NEXT %d ROWS ONLY", *limit)
		}
		return s
	case MySQL, Maria:
		if limit == nil {
			return fmt.Sprintf("%s LIMIT 18446744073709551615 OFFSET %d", query, off)
		}
	case SQLite:
		if limit == nil {
			return fmt.Sprintf("%s LIMIT -1 OFFSET %d", query, off)
		}
	case Postgres:
		if limit == nil {
			return fmt.Sprintf("%s OFFSET %d", query, off)
		}
	}
	return fmt.Sprintf("%s LIMIT %d OFFSET %d", query, *limit, off)
}

// DateTimeFormat is the format Doctrine uses to store datetimes on this engine.
func (d *DB) DateTimeFormat() string {
	if d.Driver == MSSQL {
		return "2006-01-02 15:04:05.000000"
	}
	return "2006-01-02 15:04:05"
}

// FormatTime converts a time to the stored representation: wall-clock time in the configured timezone.
func (d *DB) FormatTime(t time.Time) string {
	return t.In(d.Loc).Format(d.DateTimeFormat())
}

// TimeArg converts an optional time into a query argument.
func (d *DB) TimeArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return d.FormatTime(*t)
}

// InTx runs fn inside a transaction.
func (d *DB) InTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
