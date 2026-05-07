// Package config loads server configuration from environment (and optional .env file).
package config

import (
	"os"

	"github.com/joho/godotenv"
)

// Config holds all server configuration values.
type Config struct {
	Port string
	DB   string
}

// Load reads .env (if present) and then reads environment variables.
// Missing .env is not an error.
func Load() Config {
	// Ignore error — .env is optional.
	_ = godotenv.Load()

	port := os.Getenv("CHANWIRE_PORT")
	if port == "" {
		port = "12306"
	}

	db := os.Getenv("CHANWIRE_DB")
	if db == "" {
		db = "./data/chanwire.db"
	}

	return Config{
		Port: port,
		DB:   db,
	}
}
