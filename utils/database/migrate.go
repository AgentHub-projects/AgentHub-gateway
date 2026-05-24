package database

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/gorm"
)

const DefaultMigrationPath = "scripts/DB"

func RunMigrations(dbManager *gorm.DB) error {
	if dbManager == nil {
		return fmt.Errorf("db manager is nil")
	}

	sqlDB, err := dbManager.DB()
	if err != nil {
		return fmt.Errorf("get raw sql db: %w", err)
	}

	driver, err := postgres.WithInstance(sqlDB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create migration postgres driver: %w", err)
	}

	migrationsAbsPath, err := filepath.Abs(DefaultMigrationPath)
	if err != nil {
		return fmt.Errorf("resolve migrations path: %w", err)
	}

	sourceURL := "file://" + filepath.ToSlash(migrationsAbsPath)
	m, err := migrate.NewWithDatabaseInstance(sourceURL, "postgres", driver)
	if err != nil {
		return fmt.Errorf("init migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations up: %w", err)
	}

	return nil
}
