package apimodels

import (
	"github.com/USA-RedDragon/DMRHub/internal/config"
)

type POSTConfig struct {
	config.Config
	Secret       string `json:"secret"`
	PasswordSalt string `json:"password-salt"`
	HIBPAPIKey   string `json:"hibp-api-key"`
	SMTP         struct {
		config.SMTP
		Password string `json:"password"`
	}
	Database struct {
		config.Database
		Password string `json:"password"`
	}
	Redis struct {
		config.Redis
		Password string `json:"password"`
	}
}
