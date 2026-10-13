// Package postgres implements the reservation repository with GORM.
package postgres

import (
	"context"
	"errors"
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

func (m reservationModel) toDomain() reservation.Reservation {
	return reservation.Reservation{ID: m.ID, Email: m.Email, Name: m.Name, CreatedAt: m.CreatedAt}
}

// GetByID ignores soft-deleted rows, so a deleted reservation is not found.
func (r *Repository) GetByID(ctx context.Context, id uint) (reservation.Reservation, error) {
	var m reservationModel
	err := r.db.WithContext(ctx).First(&m, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return reservation.Reservation{}, reservation.ErrNotFound
	}
	if err != nil {
		return reservation.Reservation{}, fmt.Errorf("get reservation: %w", err)
	}
	return m.toDomain(), nil
}

func (r *Repository) List(ctx context.Context, limit, offset int) ([]reservation.Reservation, error) {
	var models []reservationModel
	if err := r.db.WithContext(ctx).Order("id").Limit(limit).Offset(offset).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list reservations: %w", err)
	}
	items := make([]reservation.Reservation, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, nil
}

func (r *Repository) Update(ctx context.Context, res *reservation.Reservation) error {
	result := r.db.WithContext(ctx).Model(&reservationModel{}).
		Where("id = ?", res.ID).
		Updates(map[string]any{"email": res.Email, "name": res.Name})
	if result.Error != nil {
		return fmt.Errorf("update reservation: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return reservation.ErrNotFound
	}
	updated, err := r.GetByID(ctx, res.ID)
	if err != nil {
		return err
	}
	*res = updated
	return nil
}

// Delete soft-deletes the row (gorm.Model sets deleted_at).
func (r *Repository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&reservationModel{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete reservation: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return reservation.ErrNotFound
	}
	return nil
}
