package database

import (
	"fmt"
	"gateway/utils/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func NewDBManager(pg config.Postgres) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", pg.Host, pg.Port, pg.Username, pg.Password, pg.Database)
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}
