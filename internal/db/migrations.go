package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Migrations are ported from module/Core/migrations. Doctrine records a migration in the `migrations` table only when
// it was not skipped; skipped ones are evaluated again on every run, which this port reproduces.
const migrationsNamespace = `ShlinkMigrations\`

type migration struct {
	version string
	// up applies the migration. It returns skipped=true when upstream's skipIf() condition holds.
	up func(ctx context.Context, d *DB) (skipped bool, err error)
}

var migrations = []migration{
	{"Version20230211171904", migrateVisitsPotentialBotIndex},
	{"Version20230303164233", migrateVisitsPotentialBotIndexRename},
	{"Version20240220214031", migrateTextColumns},
	{"Version20240224115725", migrateCreateRedirectRules},
	{"Version20240226214216", migrateDeviceLongUrlsToRules},
	{"Version20240227080629", migrateDropDeviceLongUrls},
	{"Version20240306132518", migrateCreateShortUrlVisitsCounts},
	{"Version20240318084804", migrateFillShortUrlVisitsCounts},
	{"Version20240331111103", migrateCreateOrphanVisitsCounts},
	{"Version20240331111447", migrateFillOrphanVisitsCounts},
	{"Version20241105094747", migrateApiKeyNames},
	{"Version20241105215309", migrateHashApiKeys},
	{"Version20241124112257", migrateVisitsRedirectUrl},
	{"Version20241125213106", migrateMsSqlUniqueShortCodeIndex},
	{"Version20241212131058", migrateCreateGeolocationDbUpdates},
	{"Version20250215100756", migrateMsSqlUniqueShortCodeIndexAgain},
	{"Version20250722060208", migrateNullableMatchValue},
	{"Version20260524105410", migrateLongUrlHashColumn},
	{"Version20260607082210", migrateFillLongUrlHashes},
}

// MigrationResult summarizes a migrate run.
type MigrationResult struct {
	Executed []string
	Skipped  []string
}

// Migrate runs all pending migrations.
func (d *DB) Migrate(ctx context.Context) (MigrationResult, error) {
	var res MigrationResult
	if err := d.ensureMigrationsTable(ctx); err != nil {
		return res, err
	}
	done, err := d.executedMigrations(ctx)
	if err != nil {
		return res, err
	}
	for _, m := range migrations {
		version := migrationsNamespace + m.version
		if done[version] {
			continue
		}
		start := time.Now()
		skipped, err := m.up(ctx, d)
		if err != nil {
			return res, fmt.Errorf("migration %s failed: %w", m.version, err)
		}
		if skipped {
			res.Skipped = append(res.Skipped, version)
			continue
		}
		elapsed := time.Since(start).Milliseconds()
		if _, err := d.Exec(ctx, d.DB, "INSERT INTO migrations (version, executed_at, execution_time) VALUES (?, ?, ?)",
			version, time.Now().In(d.Loc).Format("2006-01-02 15:04:05"), elapsed); err != nil {
			return res, err
		}
		res.Executed = append(res.Executed, version)
	}
	return res, nil
}

func (d *DB) ensureMigrationsTable(ctx context.Context) error {
	exists, err := d.HasTable(ctx, "migrations")
	if err != nil || exists {
		return err
	}
	var ddl string
	switch d.Driver {
	case Postgres:
		ddl = "CREATE TABLE migrations (version VARCHAR(191) NOT NULL, executed_at TIMESTAMP(0) WITHOUT TIME ZONE DEFAULT NULL, execution_time INT DEFAULT NULL, PRIMARY KEY(version))"
	case MySQL, Maria:
		ddl = "CREATE TABLE migrations (version VARCHAR(191) NOT NULL, executed_at DATETIME DEFAULT NULL, execution_time INT DEFAULT NULL, PRIMARY KEY(version)) DEFAULT CHARACTER SET utf8mb4"
	case MSSQL:
		ddl = "CREATE TABLE migrations (version NVARCHAR(191) NOT NULL, executed_at DATETIME2(6), execution_time INT, PRIMARY KEY (version))"
	default:
		ddl = "CREATE TABLE migrations (version VARCHAR(191) NOT NULL, executed_at DATETIME DEFAULT NULL, execution_time INTEGER DEFAULT NULL, PRIMARY KEY(version))"
	}
	_, err = d.ExecContext(ctx, ddl)
	return err
}

func (d *DB) executedMigrations(ctx context.Context) (map[string]bool, error) {
	versions, err := d.strings(ctx, "SELECT version FROM migrations")
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(versions))
	for _, v := range versions {
		out[v] = true
	}
	return out, nil
}

func (d *DB) execAll(ctx context.Context, stmts ...string) error {
	for _, s := range stmts {
		if _, err := d.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("%w: %s", err, s)
		}
	}
	return nil
}

func (d *DB) dropIndexSQL(table, index string) string {
	switch d.Driver {
	case MySQL, Maria, MSSQL:
		return "DROP INDEX " + index + " ON " + table
	case Postgres:
		return "DROP INDEX " + strings.ToLower(index)
	default:
		return "DROP INDEX " + index
	}
}

func migrateVisitsPotentialBotIndex(ctx context.Context, d *DB) (bool, error) {
	if has, err := d.HasIndex(ctx, "visits", "IDX_visits_potential_bot"); err != nil || has {
		return has, err
	}
	return false, d.execAll(ctx, "CREATE INDEX IDX_visits_potential_bot ON visits (short_url_id, potential_bot)")
}

func migrateVisitsPotentialBotIndexRename(ctx context.Context, d *DB) (bool, error) {
	if has, err := d.HasIndex(ctx, "visits", "visits_potential_bot_IDX"); err != nil || has {
		return has, err
	}
	return false, d.execAll(ctx,
		d.dropIndexSQL("visits", "IDX_visits_potential_bot"),
		"CREATE INDEX visits_potential_bot_IDX ON visits (potential_bot)",
	)
}

func migrateTextColumns(ctx context.Context, d *DB) (bool, error) {
	if d.Driver == SQLite {
		// VARCHAR and CLOB share the TEXT affinity in SQLite; there is nothing to change.
		return false, nil
	}
	cols := map[string][]string{
		"domains":    {"base_url_redirect", "regular_not_found_redirect", "invalid_short_url_redirect"},
		"short_urls": {"original_url"},
	}
	for _, table := range []string{"domains", "short_urls"} {
		for _, col := range cols[table] {
			info, err := d.ColumnInfo(ctx, table, col)
			if err != nil {
				return false, err
			}
			if info == nil || isTextType(d.Driver, info.Type) {
				continue
			}
			null := "NULL"
			if !info.Nullable {
				null = "NOT NULL"
			}
			var stmt string
			switch d.Driver {
			case Postgres:
				stmt = fmt.Sprintf("ALTER TABLE %s ALTER %s TYPE TEXT", table, col)
			case MySQL, Maria:
				stmt = fmt.Sprintf("ALTER TABLE %s CHANGE %s %s TEXT %s", table, col, col, null)
			case MSSQL:
				stmt = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s VARCHAR(MAX) %s", table, col, null)
			}
			if err := d.execAll(ctx, stmt); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

func isTextType(driver Driver, typ string) bool {
	t := strings.ToLower(typ)
	switch driver {
	case MSSQL:
		return t == "varchar" || t == "nvarchar" || t == "text" || t == "ntext"
	default:
		return strings.Contains(t, "text") || t == "clob"
	}
}

// Column type helpers for tables created by migrations, following Doctrine's type mapping per engine.
func (d *DB) idColumn() string {
	switch d.Driver {
	case Postgres:
		return "id BIGINT GENERATED BY DEFAULT AS IDENTITY NOT NULL"
	case MySQL, Maria:
		return "id BIGINT UNSIGNED AUTO_INCREMENT NOT NULL"
	case MSSQL:
		return "id BIGINT IDENTITY NOT NULL"
	default:
		return "id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL"
	}
}

func (d *DB) pk() string {
	if d.Driver == SQLite {
		return ""
	}
	return ", PRIMARY KEY (id)"
}

func (d *DB) bigintRef() string {
	if d.Driver.IsMySQLCompatible() || d.Driver == SQLite {
		return "BIGINT UNSIGNED"
	}
	return "BIGINT"
}

func (d *DB) boolType() string {
	switch d.Driver {
	case MySQL, Maria:
		return "TINYINT(1)"
	case MSSQL:
		return "BIT"
	default:
		return "BOOLEAN"
	}
}

func (d *DB) boolDefault(v bool) string {
	switch d.Driver {
	case Postgres:
		if v {
			return "true"
		}
		return "false"
	default:
		if v {
			return "1"
		}
		return "0"
	}
}

func (d *DB) textType() string {
	switch d.Driver {
	case MSSQL:
		return "VARCHAR(MAX)"
	case SQLite:
		return "CLOB"
	default:
		return "TEXT"
	}
}

func (d *DB) varchar(n int) string {
	if d.Driver == MSSQL {
		return fmt.Sprintf("NVARCHAR(%d)", n)
	}
	return fmt.Sprintf("VARCHAR(%d)", n)
}

func (d *DB) datetimeType() string {
	switch d.Driver {
	case Postgres:
		return "TIMESTAMP(0) WITHOUT TIME ZONE"
	case MSSQL:
		return "DATETIME2(6)"
	default:
		return "DATETIME"
	}
}

func (d *DB) tableSuffix() string {
	if d.Driver.IsMySQLCompatible() {
		return " DEFAULT CHARACTER SET utf8mb4"
	}
	return ""
}

func (d *DB) fk(name, col, refTable, onDelete string) string {
	if d.Driver == SQLite {
		return fmt.Sprintf(", CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (id) ON DELETE %s NOT DEFERRABLE INITIALLY IMMEDIATE", name, col, refTable, onDelete)
	}
	return ""
}

func (d *DB) addFk(table, name, col, refTable, onDelete string) []string {
	if d.Driver == SQLite {
		return nil
	}
	return []string{fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (id) ON DELETE %s", table, name, col, refTable, onDelete)}
}

func migrateCreateRedirectRules(ctx context.Context, d *DB) (bool, error) {
	if has, err := d.HasTable(ctx, "short_url_redirect_rules"); err != nil || has {
		return has, err
	}
	priority := "INT DEFAULT 1 NOT NULL"
	if d.Driver.IsMySQLCompatible() {
		priority = "INT UNSIGNED DEFAULT 1 NOT NULL"
	} else if d.Driver == SQLite {
		priority = "INTEGER UNSIGNED DEFAULT 1 NOT NULL"
	}
	stmts := []string{
		fmt.Sprintf("CREATE TABLE short_url_redirect_rules (%s, priority %s, long_url %s NOT NULL, short_url_id %s NOT NULL%s%s)%s",
			d.idColumn(), priority, d.textType(), d.bigintRef(),
			d.fk("FK_448348BCF1252BC8", "short_url_id", "short_urls", "CASCADE"), d.pk(), d.tableSuffix()),
		"CREATE INDEX IDX_448348BCF1252BC8 ON short_url_redirect_rules (short_url_id)",
		fmt.Sprintf("CREATE TABLE redirect_conditions (%s, type %s NOT NULL, match_key %s DEFAULT NULL, match_value %s NOT NULL%s)%s",
			d.idColumn(), d.varchar(255), d.varchar(512), d.varchar(512), d.pk(), d.tableSuffix()),
		fmt.Sprintf("CREATE TABLE redirect_conditions_in_short_url_redirect_rules (redirect_condition_id %s NOT NULL, short_url_redirect_rule_id %s NOT NULL, PRIMARY KEY (redirect_condition_id, short_url_redirect_rule_id)%s%s)%s",
			d.bigintRef(), d.bigintRef(),
			d.fk("FK_62CF4FA49AE7732E", "redirect_condition_id", "redirect_conditions", "CASCADE"),
			d.fk("FK_62CF4FA4BE19B441", "short_url_redirect_rule_id", "short_url_redirect_rules", "CASCADE"),
			d.tableSuffix()),
		"CREATE INDEX IDX_62CF4FA49AE7732E ON redirect_conditions_in_short_url_redirect_rules (redirect_condition_id)",
		"CREATE INDEX IDX_62CF4FA4BE19B441 ON redirect_conditions_in_short_url_redirect_rules (short_url_redirect_rule_id)",
	}
	stmts = append(stmts, d.addFk("short_url_redirect_rules", "FK_448348BCF1252BC8", "short_url_id", "short_urls", "CASCADE")...)
	stmts = append(stmts, d.addFk("redirect_conditions_in_short_url_redirect_rules", "FK_62CF4FA49AE7732E", "redirect_condition_id", "redirect_conditions", "CASCADE")...)
	stmts = append(stmts, d.addFk("redirect_conditions_in_short_url_redirect_rules", "FK_62CF4FA4BE19B441", "short_url_redirect_rule_id", "short_url_redirect_rules", "CASCADE")...)
	return false, d.execAll(ctx, stmts...)
}

func migrateDeviceLongUrlsToRules(ctx context.Context, d *DB) (bool, error) {
	if has, err := d.HasTable(ctx, "device_long_urls"); err != nil || !has {
		return !has, err
	}
	rows, err := d.Query(ctx, d.DB, "SELECT short_url_id, device_type, long_url FROM device_long_urls")
	if err != nil {
		return false, err
	}
	type rule struct {
		shortURLID       int64
		deviceType, long string
	}
	var rules []rule
	for rows.Next() {
		var r rule
		if err := rows.Scan(&r.shortURLID, &r.deviceType, &r.long); err != nil {
			rows.Close()
			return false, err
		}
		rules = append(rules, r)
	}
	rows.Close()
	priorities := map[int64]int{}
	for _, r := range rules {
		priority := priorities[r.shortURLID]
		if priority == 0 {
			priority = 1
		}
		ruleID, err := d.Insert(ctx, d.DB, "short_url_redirect_rules", []string{"priority", "long_url", "short_url_id"}, priority, r.long, r.shortURLID)
		if err != nil {
			return false, err
		}
		condID, err := d.Insert(ctx, d.DB, "redirect_conditions", []string{"type", "match_value", "match_key"}, "device", r.deviceType, nil)
		if err != nil {
			return false, err
		}
		if _, err := d.Exec(ctx, d.DB, "INSERT INTO redirect_conditions_in_short_url_redirect_rules (redirect_condition_id, short_url_redirect_rule_id) VALUES (?, ?)", condID, ruleID); err != nil {
			return false, err
		}
		priorities[r.shortURLID] = priority + 1
	}
	return false, nil
}

func migrateDropDeviceLongUrls(ctx context.Context, d *DB) (bool, error) {
	if has, err := d.HasTable(ctx, "device_long_urls"); err != nil || !has {
		return !has, err
	}
	return false, d.execAll(ctx, "DROP TABLE device_long_urls")
}

func (d *DB) countsTable(name string, withShortURL bool) []string {
	unsignedInt := "INT"
	unsignedBigint := "BIGINT"
	if d.Driver.IsMySQLCompatible() {
		unsignedInt, unsignedBigint = "INT UNSIGNED", "BIGINT UNSIGNED"
	} else if d.Driver == SQLite {
		unsignedInt, unsignedBigint = "INTEGER UNSIGNED", "BIGINT UNSIGNED"
	}
	cols := []string{d.idColumn()}
	if withShortURL {
		cols = append(cols, "short_url_id "+d.bigintRef()+" NOT NULL")
	}
	cols = append(cols,
		"potential_bot "+d.boolType()+" DEFAULT "+d.boolDefault(false)+" NOT NULL",
		"slot_id "+unsignedInt+" DEFAULT 1 NOT NULL",
		"count "+unsignedBigint+" DEFAULT 1 NOT NULL",
	)
	body := strings.Join(cols, ", ")
	if withShortURL {
		body += d.fk("FK_28E514E3F1252BC8", "short_url_id", "short_urls", "CASCADE")
	}
	stmts := []string{fmt.Sprintf("CREATE TABLE %s (%s%s)%s", name, body, d.pk(), d.tableSuffix())}
	if withShortURL {
		stmts = append(stmts,
			"CREATE INDEX IDX_28E514E3F1252BC8 ON short_url_visits_counts (short_url_id)",
			"CREATE UNIQUE INDEX UQ_slot_per_short_url ON short_url_visits_counts (short_url_id, potential_bot, slot_id)",
		)
		stmts = append(stmts, d.addFk(name, "FK_28E514E3F1252BC8", "short_url_id", "short_urls", "CASCADE")...)
	} else {
		stmts = append(stmts, "CREATE UNIQUE INDEX UQ_slot ON orphan_visits_counts (potential_bot, slot_id)")
	}
	return stmts
}

func migrateCreateShortUrlVisitsCounts(ctx context.Context, d *DB) (bool, error) {
	if has, err := d.HasTable(ctx, "short_url_visits_counts"); err != nil || has {
		return has, err
	}
	return false, d.execAll(ctx, d.countsTable("short_url_visits_counts", true)...)
}

func migrateCreateOrphanVisitsCounts(ctx context.Context, d *DB) (bool, error) {
	if has, err := d.HasTable(ctx, "orphan_visits_counts"); err != nil || has {
		return has, err
	}
	return false, d.execAll(ctx, d.countsTable("orphan_visits_counts", false)...)
}

func migrateFillShortUrlVisitsCounts(ctx context.Context, d *DB) (bool, error) {
	ids, err := d.int64s(ctx, "SELECT id FROM short_urls")
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		for _, bot := range []bool{true, false} {
			var count Int
			if err := d.QueryRow(ctx, d.DB, "SELECT COUNT(id) FROM visits WHERE short_url_id = ? AND potential_bot = ?", id, bot).Scan(&count); err != nil {
				return false, err
			}
			if count > 0 {
				if _, err := d.Exec(ctx, d.DB, "INSERT INTO short_url_visits_counts (short_url_id, count, potential_bot) VALUES (?, ?, ?)", id, int64(count), bot); err != nil {
					return false, err
				}
			}
		}
	}
	return false, nil
}

func migrateFillOrphanVisitsCounts(ctx context.Context, d *DB) (bool, error) {
	for _, bot := range []bool{true, false} {
		var count Int
		if err := d.QueryRow(ctx, d.DB, "SELECT COUNT(id) FROM visits WHERE short_url_id IS NULL AND potential_bot = ?", bot).Scan(&count); err != nil {
			return false, err
		}
		if count > 0 {
			if _, err := d.Exec(ctx, d.DB, "INSERT INTO orphan_visits_counts (count, potential_bot) VALUES (?, ?)", int64(count), bot); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

func (d *DB) int64s(ctx context.Context, query string, args ...any) ([]int64, error) {
	rows, err := d.Query(ctx, d.DB, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *DB) concat(parts ...string) string {
	switch d.Driver {
	case Postgres, SQLite:
		return strings.Join(parts, " || ")
	default:
		return "CONCAT(" + strings.Join(parts, ", ") + ")"
	}
}

func migrateApiKeyNames(ctx context.Context, d *DB) (bool, error) {
	key := d.Q("key")
	name := d.Q("name")
	return false, d.execAll(ctx,
		fmt.Sprintf("UPDATE api_keys SET %s = %s WHERE %s IS NOT NULL", name, d.concat(name, "' - '", key), name),
		fmt.Sprintf("UPDATE api_keys SET %s = %s WHERE %s IS NULL", name, key, name),
	)
}

func migrateHashApiKeys(ctx context.Context, d *DB) (bool, error) {
	key := d.Q("key")
	rows, err := d.Query(ctx, d.DB, "SELECT "+key+" FROM api_keys")
	if err != nil {
		return false, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return false, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	for _, k := range keys {
		if k == "" || k == "0" {
			// PHP's `while ($key = fetchOne())` stops at the first falsy key
			break
		}
		sum := sha256.Sum256([]byte(k))
		if _, err := d.Exec(ctx, d.DB, "UPDATE api_keys SET "+key+" = ? WHERE "+key+" = ?", fmt.Sprintf("%x", sum), k); err != nil {
			return false, err
		}
	}
	return false, nil
}

func migrateVisitsRedirectUrl(ctx context.Context, d *DB) (bool, error) {
	if col, err := d.ColumnInfo(ctx, "visits", "redirect_url"); err != nil || col != nil {
		return col != nil, err
	}
	return false, d.execAll(ctx, fmt.Sprintf("ALTER TABLE visits ADD redirect_url %s DEFAULT NULL", d.varchar(2048)))
}

func migrateMsSqlUniqueShortCodeIndex(ctx context.Context, d *DB) (bool, error) {
	if d.Driver != MSSQL {
		return true, nil
	}
	return false, d.execAll(ctx,
		"DROP INDEX unique_short_code_plus_domain ON short_urls",
		"CREATE UNIQUE INDEX unique_short_code_plus_domain ON short_urls (short_code, domain_id) WHERE short_code IS NOT NULL AND domain_id IS NOT NULL",
	)
}

func migrateCreateGeolocationDbUpdates(ctx context.Context, d *DB) (bool, error) {
	if has, err := d.HasTable(ctx, "geolocation_db_updates"); err != nil || has {
		return has, err
	}
	now := "CURRENT_TIMESTAMP"
	stmts := []string{
		fmt.Sprintf("CREATE TABLE geolocation_db_updates (%s, date_created %s DEFAULT %s NOT NULL, date_updated %s DEFAULT %s NOT NULL, status %s DEFAULT 'in-progress' NOT NULL, filesystem_id %s NOT NULL, reason %s NOT NULL, error %s DEFAULT NULL%s)%s",
			d.idColumn(), d.datetimeType(), now, d.datetimeType(), now, d.varchar(128), d.varchar(512), d.varchar(1024), d.varchar(1024), d.pk(), d.tableSuffix()),
		"CREATE INDEX IDX_geolocation_date_updated ON geolocation_db_updates (date_updated)",
		"CREATE INDEX IDX_geolocation_status_filesystem ON geolocation_db_updates (filesystem_id)",
	}
	return false, d.execAll(ctx, stmts...)
}

func migrateMsSqlUniqueShortCodeIndexAgain(ctx context.Context, d *DB) (bool, error) {
	if d.Driver != MSSQL {
		return true, nil
	}
	return false, d.execAll(ctx,
		"DROP INDEX unique_short_code_plus_domain ON short_urls",
		"CREATE UNIQUE INDEX unique_short_code_plus_domain ON short_urls (short_code, domain_id);",
	)
}

func migrateNullableMatchValue(ctx context.Context, d *DB) (bool, error) {
	col, err := d.ColumnInfo(ctx, "redirect_conditions", "match_value")
	if err != nil || col == nil || col.Nullable || d.Driver == SQLite {
		// SQLite would need a table rebuild; Doctrine does that, the effect (nullable column) is reproduced below.
		if d.Driver == SQLite && col != nil && !col.Nullable {
			return false, d.rebuildSQLiteRedirectConditions(ctx)
		}
		return false, err
	}
	var stmt string
	switch d.Driver {
	case Postgres:
		stmt = "ALTER TABLE redirect_conditions ALTER match_value DROP NOT NULL"
	case MySQL, Maria:
		stmt = "ALTER TABLE redirect_conditions CHANGE match_value match_value VARCHAR(512) DEFAULT NULL"
	case MSSQL:
		stmt = "ALTER TABLE redirect_conditions ALTER COLUMN match_value NVARCHAR(512)"
	}
	return false, d.execAll(ctx, stmt)
}

func (d *DB) rebuildSQLiteRedirectConditions(ctx context.Context) error {
	return d.execAll(ctx,
		"CREATE TEMPORARY TABLE __temp__redirect_conditions AS SELECT id, type, match_key, match_value FROM redirect_conditions",
		"DROP TABLE redirect_conditions",
		"CREATE TABLE redirect_conditions (id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL, type VARCHAR(255) NOT NULL, match_key VARCHAR(512) DEFAULT NULL, match_value VARCHAR(512) DEFAULT NULL)",
		"INSERT INTO redirect_conditions (id, type, match_key, match_value) SELECT id, type, match_key, match_value FROM __temp__redirect_conditions",
		"DROP TABLE __temp__redirect_conditions",
	)
}

func migrateLongUrlHashColumn(ctx context.Context, d *DB) (bool, error) {
	if col, err := d.ColumnInfo(ctx, "short_urls", "long_url_hash"); err != nil || col != nil {
		return false, err
	}
	var add string
	switch d.Driver {
	case MSSQL:
		add = "ALTER TABLE short_urls ADD long_url_hash VARBINARY(32) NOT NULL DEFAULT (0x00)"
	case Postgres:
		add = "ALTER TABLE short_urls ADD long_url_hash BYTEA DEFAULT '' NOT NULL"
	case MySQL, Maria:
		add = "ALTER TABLE short_urls ADD long_url_hash BINARY(32) DEFAULT '' NOT NULL"
	default:
		add = "ALTER TABLE short_urls ADD COLUMN long_url_hash BLOB DEFAULT '' NOT NULL"
	}
	return false, d.execAll(ctx, add, "CREATE INDEX IDX_long_url_hash ON short_urls (long_url_hash)")
}

func migrateFillLongUrlHashes(ctx context.Context, d *DB) (bool, error) {
	for {
		var query string
		var args []any
		if d.Driver == MSSQL {
			query = "SELECT id, original_url FROM short_urls WHERE long_url_hash = 0x00 ORDER BY (SELECT 0) OFFSET 0 ROWS FETCH NEXT 10000 ROWS ONLY"
		} else {
			query = "SELECT id, original_url FROM short_urls WHERE long_url_hash = ? LIMIT 10000"
			args = append(args, []byte{})
		}
		type row struct {
			id  int64
			url string
		}
		rows, err := d.Query(ctx, d.DB, query, args...)
		if err != nil {
			return false, err
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.url); err != nil {
				rows.Close()
				return false, err
			}
			batch = append(batch, r)
		}
		rows.Close()
		if len(batch) == 0 {
			return false, nil
		}
		update := func(q Querier) error {
			for _, r := range batch {
				sum := sha256.Sum256([]byte(r.url))
				if _, err := d.Exec(ctx, q, "UPDATE short_urls SET long_url_hash = ? WHERE id = ?", sum[:], r.id); err != nil {
					return err
				}
			}
			return nil
		}
		if d.Driver == MSSQL {
			err = update(d.DB)
		} else {
			err = d.InTx(ctx, func(tx *sql.Tx) error { return update(tx) })
		}
		if err != nil {
			return false, err
		}
	}
}
