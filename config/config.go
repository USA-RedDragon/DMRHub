package config

import "os"

type Config struct {
	loaded    bool
	RedisHost string
}

var currentConfig Config

func GetConfig() *Config {
	if currentConfig.loaded {
		return &currentConfig
	} else {
		currentConfig = Config{
			loaded:    true,
			RedisHost: os.Getenv("REDIS_HOST"),
		}
		if currentConfig.RedisHost == "" {
			currentConfig.RedisHost = "localhost:6379"
		}
		return &currentConfig
	}
}
