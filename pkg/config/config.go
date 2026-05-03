package config

import "os"

type Config struct {
	RedisAddr        string
	RegisterPassword string
}

func Load() *Config {
	return &Config{
		RedisAddr:        getEnv("REDIS_ADDR", "localhost:6379"),
		RegisterPassword: getEnv("REGISTER_PASSWORD", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
