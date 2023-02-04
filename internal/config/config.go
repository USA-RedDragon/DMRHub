package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"k8s.io/klog/v2"
)

type Config struct {
	loaded                   bool
	RedisHost                string
	RedisPassword            string
	PostgresDSN              string
	postgresUser             string
	postgresPassword         string
	postgresHost             string
	postgresPort             int
	postgresDatabase         string
	Secret                   string
	PasswordSalt             string
	ListenAddr               string
	DMRPort                  int
	HTTPPort                 int
	CORSHosts                []string
	TrustedProxies           []string
	HIBPAPIKey               string
	OTLPEndpoint             string
	InitialAdminUserPassword string
	Debug                    bool
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
			loaded:                   false,
			RedisHost:                os.Getenv("REDIS_HOST"),
			postgresUser:             os.Getenv("PG_USER"),
			postgresPassword:         os.Getenv("PG_PASSWORD"),
			postgresHost:             os.Getenv("PG_HOST"),
			postgresPort:             int(pgPort),
			postgresDatabase:         os.Getenv("PG_DATABASE"),
			Secret:                   os.Getenv("SECRET"),
			PasswordSalt:             os.Getenv("PASSWORD_SALT"),
			ListenAddr:               os.Getenv("LISTEN_ADDR"),
			DMRPort:                  int(dmrPort),
			HTTPPort:                 int(httpPort),
			HIBPAPIKey:               os.Getenv("HIBP_API_KEY"),
			OTLPEndpoint:             os.Getenv("OTLP_ENDPOINT"),
			InitialAdminUserPassword: os.Getenv("INIT_ADMIN_USER_PASSWORD"),
			RedisPassword:            os.Getenv("REDIS_PASSWORD"),
			Debug:                    os.Getenv("DEBUG") != "",
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
		if currentConfig.InitialAdminUserPassword == "" {
			klog.Errorf("Initial admin user password not set, using auto-generated password")
			currentConfig.InitialAdminUserPassword, err = utils.RandomPassword(15, 4, 2)
			if err != nil {
				klog.Errorf("Password generation failed")
			}
		}
		if currentConfig.RedisPassword == "" {
			currentConfig.RedisPassword = "password"
			klog.Errorf("Redis password not set, using INSECURE default")
		}
		// CORS_HOSTS is a comma separated list of hosts that are allowed to access the API
		corsHosts := os.Getenv("CORS_HOSTS")
		if corsHosts == "" {
			currentConfig.CORSHosts = []string{
				fmt.Sprintf("http://localhost:%d", currentConfig.HTTPPort),
				fmt.Sprintf("http://127.0.0.1:%d", currentConfig.HTTPPort),
			}
		} else {
			currentConfig.CORSHosts = strings.Split(corsHosts, ",")
		}
		trustedProxies := os.Getenv("TRUSTED_PROXIES")
		if trustedProxies == "" {
			currentConfig.TrustedProxies = []string{}
		} else {
			currentConfig.TrustedProxies = strings.Split(trustedProxies, ",")
		}
		if currentConfig.Debug {
			klog.Warningf("Debug mode enabled, this should not be used in production")
			klog.Infof("Config: %+v", currentConfig)
		}
		currentConfig.loaded = true
		return &currentConfig
	}
}
