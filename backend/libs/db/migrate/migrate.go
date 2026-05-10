package migrate

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type Options struct {
	MigrationsTable string
}

func Up(databaseURL string, migrations fs.FS, root string, opts Options) error {
	pgxCfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	db := stdlib.OpenDB(*pgxCfg)
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{
		MigrationsTable: opts.MigrationsTable,
	})
	if err != nil {
		return fmt.Errorf("create migrate postgres driver: %w", err)
	}

	src, err := iofs.New(migrations, root)
	if err != nil {
		return fmt.Errorf("create migrate iofs source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}

	return nil
}
