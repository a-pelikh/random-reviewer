package migrations

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"randomreviewer/migrations"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

const dbDialect = "postgres"

func Run(db *sql.DB) error {
	var isSourceClosed bool
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("create source: %w", err)
	}
	defer func() {
		if !isSourceClosed {
			if err := source.Close(); err != nil {
				slog.Warn("failed to close migrations source", "error", err)
			}
		}
	}()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, dbDialect, driver)
	if err != nil {
		return fmt.Errorf("create migrate instance: %w", err)
	}
	defer func() {
		isSourceClosed = true
		if sourceErr, dbErr := m.Close(); err != nil {
			slog.Warn("failed to close migrate instance", "source error", sourceErr, "db error", dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}
