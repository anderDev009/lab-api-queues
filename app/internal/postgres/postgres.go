// Package postgres implements the reservation repository with GORM.
package postgres

import (
	"context"
	"fmt"
	"time"

	"api-memberships/internal/reservation"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type reservationModel struct {
	gorm.Model
	Email string `gorm:"not null;index"`
	Name  string `gorm:"not null"`
}

func (reservationModel) TableName() string { return "reservations" }

// Connect opens the database, configures the pool and migrates the schema.
func Connect(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := db.AutoMigrate(&reservationModel{}); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// Close releases the underlying connection pool.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, res *reservation.Reservation) error {
	m := reservationModel{Email: res.Email, Name: res.Name}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return fmt.Errorf("insert reservation: %w", err)
	}
	res.ID = m.ID
	res.CreatedAt = m.CreatedAt
	return nil
}
