package apimodels

import (
	"github.com/USA-RedDragon/DMRHub/internal/config"
)

type POSTConfig struct {
	config.Config
	Secret       string `json:"secret,omitempty"`
	PasswordSalt string `json:"password-salt,omitempty"`
	HIBPAPIKey   string `json:"hibp-api-key,omitempty"`
	SMTP         struct {
		config.SMTP
		Password string `json:"password,omitempty"`
	}
	Database struct {
		config.Database
		Password string `json:"password,omitempty"`
	}
	Redis struct {
		config.Redis
		Password string `json:"password,omitempty"`
	}
}

func (p POSTConfig) ToConfig(c *config.Config) {
	*c = p.Config
	c.Secret = p.Secret
	c.PasswordSalt = p.PasswordSalt
	c.HIBPAPIKey = p.HIBPAPIKey
	c.SMTP.Password = p.SMTP.Password
	c.Database.Password = p.Database.Password
	c.Redis.Password = p.Redis.Password
}
