package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/lib/pq"
)

var DB *sql.DB

// Connect to PostgreSQL
func Connect() error {
	connStr := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if connStr == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}

	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err = DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("✅ PostgreSQL connected successfully")
	return nil
}

// Create tables
func InitSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
            id VARCHAR(36) PRIMARY KEY,
            email VARCHAR(255) UNIQUE NOT NULL,
            password_hash VARCHAR(255) NOT NULL,
            username VARCHAR(100) UNIQUE NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )`,
		`CREATE TABLE IF NOT EXISTS designs (
            id VARCHAR(36) PRIMARY KEY,
            title VARCHAR(255) NOT NULL,
            description TEXT,
            code TEXT NOT NULL,
            language VARCHAR(20) DEFAULT 'VHDL',
            entity_name VARCHAR(100),
            author VARCHAR(100),
            testbench TEXT,
            user_id VARCHAR(36) REFERENCES users(id) ON DELETE CASCADE,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            views INTEGER DEFAULT 0,
            likes INTEGER DEFAULT 0,
            is_public BOOLEAN DEFAULT true
        )`,
		`CREATE TABLE IF NOT EXISTS simulations (
            id VARCHAR(36) PRIMARY KEY,
            design_id VARCHAR(36) REFERENCES designs(id),
            input_code TEXT,
            output TEXT,
            waveform TEXT,
            success BOOLEAN DEFAULT false,
            error TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )`,

		`CREATE INDEX IF NOT EXISTS idx_designs_language ON designs(language)`,
		`CREATE INDEX IF NOT EXISTS idx_designs_public ON designs(is_public)`,
		`CREATE INDEX IF NOT EXISTS idx_designs_user_id ON designs(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_simulations_design_id ON simulations(design_id)`,
	}

	for _, query := range queries {
		if _, err := DB.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
	}

	log.Println("✅ Database schema initialized")
	return nil
}

// Close database connection
func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}
