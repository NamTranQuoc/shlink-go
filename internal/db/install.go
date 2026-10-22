package db

import "context"

// EnsureCreated ports CreateDatabaseCommand: it creates the database when listing its tables fails, and the schema
// when none of Shlink's tables exist. It returns true when the schema was created.
func EnsureCreated(ctx context.Context, d *DB) (bool, error) {
	exists, err := d.AnyShlinkTableExists(ctx)
	if err != nil {
		if err := CreateDatabase(ctx, d.Params); err != nil {
			return false, err
		}
		exists = false
	}
	if exists {
		return false, nil
	}
	return true, d.CreateSchema(ctx)
}
