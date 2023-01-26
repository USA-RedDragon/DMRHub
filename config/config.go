package config

import (
	"os"
	"strconv"

	"k8s.io/klog/v2"
)

type Config struct {
	loaded           bool
	RedisHost        string
	PostgresDSN      string
	postgresUser     string
	postgresPassword string
	postgresHost     string
	postgresPort     int
	postgresDatabase string
	Secret           string
	loaded    bool
	RedisHost string
}

var currentConfig Config

func GetConfig() *Config {
	if currentConfig.loaded {
		return &currentConfig
	} else {
		// Convert string to int
		portStr := os.Getenv("PG_PORT")
		port, err := strconv.ParseInt(portStr, 10, 0)
		if err != nil {
			port = 0
		}

		currentConfig = Config{
			loaded:           false,
			RedisHost:        os.Getenv("REDIS_HOST"),
			postgresUser:     os.Getenv("PG_USER"),
			postgresPassword: os.Getenv("PG_PASSWORD"),
			postgresHost:     os.Getenv("PG_HOST"),
			postgresPort:     int(port),
			postgresDatabase: os.Getenv("PG_PASSWORD"),
			Secret:           os.Getenv("SECRET"),
		}
		if currentConfig.RedisHost == "" {
			currentConfig.RedisHost = "localhost:6379"
		}
		if currentConfig.postgresUser == "" {
			currentConfig.postgresUser = "postgres"
		}
		if currentConfig.postgresPassword == "" {
			currentConfig.postgresPassword = "password"
		}
		if currentConfig.postgresHost == "" {
			currentConfig.postgresHost = "localhost"
		}
		if currentConfig.postgresPort == 0 {
			currentConfig.postgresPort = 5432
		}
		if currentConfig.postgresDatabase == "" {
			currentConfig.postgresDatabase = "postgres"
		}
		currentConfig.PostgresDSN = "host=" + currentConfig.postgresHost + " port=" + strconv.FormatInt(int64(currentConfig.postgresPort), 10) + " user=" + currentConfig.postgresUser + " dbname=" + currentConfig.postgresDatabase + " password=" + currentConfig.postgresPassword
		if currentConfig.Secret == "" {
			currentConfig.Secret = "secret"
			klog.Errorf("Session secret not set, using INSECURE default")
		}
		currentConfig.loaded = true
		return &currentConfig
	}
}
