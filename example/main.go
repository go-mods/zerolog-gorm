package main

import (
	"context"
	"fmt"
	"github.com/glebarez/sqlite"
	zerologgorm "github.com/go-mods/zerolog-gorm"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	"os"
	"time"
)

// User represents a user model
type User struct {
	ID        uint      `gorm:"primaryKey"`
	Name      string    `gorm:"size:100"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func main() {
	// Configure the logger
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

	// Initialize GORM with a SQLite in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NowFunc: func() time.Time { return time.Now() },
		Logger: &zerologgorm.GormLogger{
			FieldsExclude: []string{zerologgorm.DurationFieldName, zerologgorm.FileFieldName},
		},
	})

	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize GORM")
	}

	// Set the logger context for the database
	db = db.WithContext(logger.WithContext(context.Background()))

	// Migrate the User model (this will add any missing columns or indexes)
	if err := db.AutoMigrate(&User{}); err != nil {
		logger.Error().Err(err).Msg("Failed to migrate database")
		return
	}

	// Create a new user
	newUser := User{Name: "John Doe"}
	result := db.Create(&newUser)

	if result.Error != nil {
		logger.Error().Err(result.Error).Msg("Failed to create user")
	} else {
		fmt.Printf("User created: %v\n", newUser)
	}
}
