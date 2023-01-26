package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
	PasswordSalt     string
	ListenAddr       string
	DMRPort          int
	HTTPPort         int
	Verbose          bool
	CORSHosts        []string
}

var currentConfig Config

func GetConfig() *Config {
	if currentConfig.loaded {
		return &currentConfig
	} else {
		// Convert string to int
		portStr := os.Getenv("PG_PORT")
		pgPort, err := strconv.ParseInt(portStr, 10, 0)
		if err != nil {
			pgPort = 0
		}

		portStr = os.Getenv("DMR_PORT")
		dmrPort, err := strconv.ParseInt(portStr, 10, 0)
		if err != nil {
			dmrPort = 0
		}

		portStr = os.Getenv("HTTP_PORT")
		httpPort, err := strconv.ParseInt(portStr, 10, 0)
		if err != nil {
			httpPort = 0
		}

		currentConfig = Config{
			loaded:           false,
			RedisHost:        os.Getenv("REDIS_HOST"),
			postgresUser:     os.Getenv("PG_USER"),
			postgresPassword: os.Getenv("PG_PASSWORD"),
			postgresHost:     os.Getenv("PG_HOST"),
			postgresPort:     int(pgPort),
			postgresDatabase: os.Getenv("PG_DATABASE"),
			Secret:           os.Getenv("SECRET"),
			PasswordSalt:     os.Getenv("PASSWORD_SALT"),
			ListenAddr:       os.Getenv("LISTEN_ADDR"),
			DMRPort:          int(dmrPort),
			HTTPPort:         int(httpPort),
			Verbose:          os.Getenv("VERBOSE") != "",
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
		if currentConfig.PasswordSalt == "" {
			currentConfig.PasswordSalt = "salt"
			klog.Errorf("Password salt not set, using INSECURE default")
		}
		if currentConfig.ListenAddr == "" {
			currentConfig.ListenAddr = "0.0.0.0"
		}
		if currentConfig.DMRPort == 0 {
			currentConfig.DMRPort = 62031
		}
		if currentConfig.HTTPPort == 0 {
			currentConfig.HTTPPort = 3005
		}
		// CORS_HOSTS is a comma separated list of hosts that are allowed to access the API
		corsHosts := os.Getenv("CORS_HOSTS")
		if corsHosts == "" {
			currentConfig.CORSHosts = []string{
				fmt.Sprintf("http://localhost:%d", currentConfig.HTTPPort),
				fmt.Sprintf("http://127.0.0.1:%d", currentConfig.HTTPPort),
			}
		} else {
			currentConfig.CORSHosts = append([]string{}, strings.Split(corsHosts, ",")...)
		}
		currentConfig.loaded = true
		return &currentConfig
	}
}
