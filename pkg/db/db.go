package db

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Init opens the database and performs auto-migration
// dbPath is where the sqlite file lives (e.g., "./database.db")
func Init(dbPath string) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Auto-Migrate creates the tables based on our structs
	// This is "Code First" migration
	err = db.AutoMigrate(&Region{}, &Store{}, &Product{}, &Sale{})
	if err != nil {
		log.Fatal("Failed to migrate database schema:", err)
	}

	return db
}
