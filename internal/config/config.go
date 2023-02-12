package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"golang.org/x/crypto/pbkdf2"
	"k8s.io/klog/v2"
)

// Config stores the application configuration
type Config struct {
	RedisHost                string
	RedisPassword            string
	PostgresDSN              string
	postgresUser             string
	postgresPassword         string
	postgresHost             string
	postgresPort             int
	postgresDatabase         string
	Secret                   []byte
	strSecret                string
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

var currentConfig atomic.Value
var isInit atomic.Bool
var loaded atomic.Bool

func loadConfig() Config {
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

	tmpConfig := Config{
		RedisHost:                os.Getenv("REDIS_HOST"),
		postgresUser:             os.Getenv("PG_USER"),
		postgresPassword:         os.Getenv("PG_PASSWORD"),
		postgresHost:             os.Getenv("PG_HOST"),
		postgresPort:             int(pgPort),
		postgresDatabase:         os.Getenv("PG_DATABASE"),
		strSecret:                os.Getenv("SECRET"),
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
	if tmpConfig.RedisHost == "" {
		tmpConfig.RedisHost = "localhost:6379"
	}
	if tmpConfig.postgresUser == "" {
		tmpConfig.postgresUser = "postgres"
	}
	if tmpConfig.postgresPassword == "" {
		tmpConfig.postgresPassword = "password"
	}
	if tmpConfig.postgresHost == "" {
		tmpConfig.postgresHost = "localhost"
	}
	if tmpConfig.postgresPort == 0 {
		tmpConfig.postgresPort = 5432
	}
	if tmpConfig.postgresDatabase == "" {
		tmpConfig.postgresDatabase = "postgres"
	}
	tmpConfig.PostgresDSN = "host=" + tmpConfig.postgresHost + " port=" + strconv.FormatInt(int64(tmpConfig.postgresPort), 10) + " user=" + tmpConfig.postgresUser + " dbname=" + tmpConfig.postgresDatabase + " password=" + tmpConfig.postgresPassword
	if tmpConfig.strSecret == "" {
		tmpConfig.strSecret = "secret"
		klog.Errorf("Session secret not set, using INSECURE default")
	}
	if tmpConfig.PasswordSalt == "" {
		tmpConfig.PasswordSalt = "salt"
		klog.Errorf("Password salt not set, using INSECURE default")
	}
	if tmpConfig.ListenAddr == "" {
		tmpConfig.ListenAddr = "0.0.0.0"
	}
	if tmpConfig.DMRPort == 0 {
		tmpConfig.DMRPort = 62031
	}
	if tmpConfig.HTTPPort == 0 {
		tmpConfig.HTTPPort = 3005
	}
	if tmpConfig.InitialAdminUserPassword == "" {
		klog.Errorf("Initial admin user password not set, using auto-generated password")
		tmpConfig.InitialAdminUserPassword, err = utils.RandomPassword(15, 4, 2)
		if err != nil {
			klog.Errorf("Password generation failed")
		}
	}
	if tmpConfig.RedisPassword == "" {
		tmpConfig.RedisPassword = "password"
		klog.Errorf("Redis password not set, using INSECURE default")
	}
	// CORS_HOSTS is a comma separated list of hosts that are allowed to access the API
	corsHosts := os.Getenv("CORS_HOSTS")
	if corsHosts == "" {
		tmpConfig.CORSHosts = []string{
			fmt.Sprintf("http://localhost:%d", tmpConfig.HTTPPort),
			fmt.Sprintf("http://127.0.0.1:%d", tmpConfig.HTTPPort),
		}
	} else {
		tmpConfig.CORSHosts = strings.Split(corsHosts, ",")
	}
	trustedProxies := os.Getenv("TRUSTED_PROXIES")
	if trustedProxies == "" {
		tmpConfig.TrustedProxies = []string{}
	} else {
		tmpConfig.TrustedProxies = strings.Split(trustedProxies, ",")
	}
	if tmpConfig.Debug {
		klog.Warningf("Debug mode enabled, this should not be used in production")
		klog.Infof("Config: %+v", tmpConfig)
	}
	tmpConfig.Secret = pbkdf2.Key([]byte(tmpConfig.strSecret), []byte(tmpConfig.PasswordSalt), 4096, 32, sha256.New)
	return tmpConfig
}

// GetConfig obtains the current configuration
// On the first call, it will load the configuration from the environment variables.
func GetConfig() *Config {
	lastInit := isInit.Swap(true)
	if !lastInit {
		currentConfig.Store(loadConfig())
		loaded.Store(true)
	}
	for !loaded.Load() {
		time.Sleep(100 * time.Millisecond)
	}

	curConfig, ok := currentConfig.Load().(Config)
	if !ok {
		klog.Fatalf("Failed to load config")
	}
	return &curConfig
}
