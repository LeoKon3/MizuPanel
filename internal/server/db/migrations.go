package db

import "database/sql"

func Migrate(db *sql.DB) error {
	return MigrateDialect(db, DialectSQLite)
}

func MigrateDialect(db *sql.DB, dialect Dialect) error {
	if dialect == DialectMySQL {
		return migrateStatements(db, DialectMySQL, mysqlMigrationStatements())
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	return migrateStatements(db, DialectSQLite, sqliteMigrationStatements())
}
