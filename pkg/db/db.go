package db

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Init opens the database and performs auto-migration
// dbPath is where the sqlite file lives (e.g., "./database.db")
func Init(dbPath string) *gorm.DB {
	config := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}

	db, err := gorm.Open(sqlite.Open(dbPath), config)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Auto-Migrate creates the tables based on our structs
	// This is "Code First" migration
	err = db.AutoMigrate(&Region{}, &Store{}, &Customer{}, &Product{}, &Sale{})
	if err != nil {
		log.Fatal("Failed to migrate database schema:", err)
	}

	return db
}
