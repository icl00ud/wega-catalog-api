package config

import (
	"os"
	"strconv"
)

type Config struct {
	Database DatabaseConfig
	APIPort  string
	LogLevel string
}

type DatabaseConfig struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	SSLMode  string
	MaxConns int
	MinConns int
}

func Load() *Config {
	return &Config{
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnvInt("DB_PORT", 5432),
			Name:     getEnv("DB_NAME", "wega"),
			User:     getEnv("DB_USER", "wega"),
			Password: getEnv("DB_PASSWORD", ""),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
			MaxConns: getEnvInt("DB_MAX_CONNS", 25),
			MinConns: getEnvInt("DB_MIN_CONNS", 5),
		},
		APIPort:  getEnv("API_PORT", "8080"),
		LogLevel: getEnv("LOG_LEVEL", "info"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
