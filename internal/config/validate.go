package config

import "errors"

var (
	ErrInvalidLogLevel             = errors.New("invalid log level provided")
	ErrInvalidRedisHost            = errors.New("invalid Redis host provided")
	ErrInvalidRedisPort            = errors.New("invalid Redis port provided")
	ErrInvalidDatabaseDriver       = errors.New("invalid database driver provided")
	ErrInvalidDatabaseHost         = errors.New("invalid database host provided")
	ErrInvalidDatabasePort         = errors.New("invalid database port provided")
	ErrInvalidDatabaseName         = errors.New("invalid database name provided")
	ErrSecretRequired              = errors.New("secret key is required for the application")
	ErrPasswordSaltRequired        = errors.New("password salt is required for hashing user passwords")
	ErrInvalidHTTPHost             = errors.New("invalid HTTP host provided")
	ErrInvalidHTTPPort             = errors.New("invalid HTTP port provided")
	ErrInvalidDMRHBRPHost          = errors.New("invalid DMR HBRP host provided")
	ErrInvalidDMRHBRPPort          = errors.New("invalid DMR HBRP port provided")
	ErrInvalidDMROpenBridgeHost    = errors.New("invalid DMR OpenBridge host provided")
	ErrInvalidDMROpenBridgePort    = errors.New("invalid DMR OpenBridge port provided")
	ErrInvalidMetricsBindAddress   = errors.New("invalid metrics server bind address provided")
	ErrInvalidMetricsPort          = errors.New("invalid metrics server port provided")
	ErrInvalidPProfBindAddress     = errors.New("invalid PProf server bind address provided")
	ErrInvalidPProfPort            = errors.New("invalid PProf server port provided")
	ErrInvalidSMTPHost             = errors.New("invalid SMTP host provided")
	ErrInvalidSMTPPort             = errors.New("invalid SMTP port provided")
	ErrInvalidSMTPUsername         = errors.New("SMTP username is required when SMTP authentication is enabled")
	ErrInvalidSMTPPassword         = errors.New("SMTP password is required when SMTP authentication is enabled")
	ErrInvalidSMTPAuthMethod       = errors.New("invalid SMTP authentication method provided")
	ErrInvalidSMTPTLS              = errors.New("invalid SMTP TLS setting provided")
	ErrSMTPFromRequired            = errors.New("SMTP 'from' address is required when SMTP is enabled")
	ErrHTTPRobotsTXTModeInvalid    = errors.New("invalid robots.txt mode provided, must be one of allow, disabled, or custom")
	ErrInvalidHTTPRobotsTXTContent = errors.New("invalid robots.txt content provided, must be non-empty when mode is custom")
	ErrHTTPCanonicalHostRequired   = errors.New("canonical host is required for generating absolute URLs in the HTTP server")
)

func (c Config) Validate() error {
	if c.LogLevel != LogLevelDebug &&
		c.LogLevel != LogLevelInfo &&
		c.LogLevel != LogLevelWarn &&
		c.LogLevel != LogLevelError {
		return ErrInvalidLogLevel
	}

	if c.Redis.Enabled {
		if c.Redis.Host == "" {
			return ErrInvalidRedisHost
		}
		if c.Redis.Port <= 0 || c.Redis.Port > 65535 {
			return ErrInvalidRedisPort
		}
	}

	if c.Database.Driver != DatabaseDriverSQLite &&
		c.Database.Driver != DatabaseDriverPostgres &&
		c.Database.Driver != DatabaseDriverMySQL {
		return ErrInvalidDatabaseDriver
	}

	if c.Database.Driver != DatabaseDriverSQLite && c.Database.Host == "" {
		return ErrInvalidDatabaseHost
	}

	if c.Database.Driver != DatabaseDriverSQLite && (c.Database.Port <= 0 || c.Database.Port > 65535) {
		return ErrInvalidDatabasePort
	}

	if c.Database.Database == "" {
		return ErrInvalidDatabaseName
	}

	if c.Secret == "" {
		return ErrSecretRequired
	}

	if c.PasswordSalt == "" {
		return ErrPasswordSaltRequired
	}

	if c.HTTP.Bind == "" {
		return ErrInvalidHTTPHost
	}

	if c.HTTP.Port <= 0 || c.HTTP.Port > 65535 {
		return ErrInvalidHTTPPort
	}

	if c.DMR.HBRP.Bind == "" {
		return ErrInvalidDMRHBRPHost
	}

	if c.DMR.HBRP.Port <= 0 || c.DMR.HBRP.Port > 65535 {
		return ErrInvalidDMRHBRPPort
	}

	if c.DMR.OpenBridge.Enabled {
		if c.DMR.OpenBridge.Bind == "" {
			return ErrInvalidDMROpenBridgeHost
		}
		if c.DMR.OpenBridge.Port <= 0 || c.DMR.OpenBridge.Port > 65535 {
			return ErrInvalidDMROpenBridgePort
		}
	}

	if c.SMTP.Enabled {
		if c.SMTP.Host == "" {
			return ErrInvalidSMTPHost
		}
		if c.SMTP.Port <= 0 || c.SMTP.Port > 65535 {
			return ErrInvalidSMTPPort
		}
		if c.SMTP.AuthMethod != SMTPAuthMethodPlain &&
			c.SMTP.AuthMethod != SMTPAuthMethodLogin &&
			c.SMTP.AuthMethod != SMTPAuthMethodNone {
			return ErrInvalidSMTPAuthMethod
		}
		if c.SMTP.TLS != SMTPTLSNone &&
			c.SMTP.TLS != SMTPTLSStartTLS &&
			c.SMTP.TLS != SMTPTLSImplicit {
			return ErrInvalidSMTPTLS
		}
		if c.SMTP.From == "" {
			return ErrSMTPFromRequired
		}
		if c.SMTP.Username == "" && c.SMTP.AuthMethod != SMTPAuthMethodNone {
			return ErrInvalidSMTPUsername
		}
		if c.SMTP.Password == "" && c.SMTP.AuthMethod != SMTPAuthMethodNone {
			return ErrInvalidSMTPPassword
		}
	}

	if c.HTTP.RobotsTXT.Mode != RobotsTXTModeAllow &&
		c.HTTP.RobotsTXT.Mode != RobotsTXTModeDisabled &&
		c.HTTP.RobotsTXT.Mode != RobotsTXTModeCustom {
		return ErrHTTPRobotsTXTModeInvalid
	}

	if c.HTTP.RobotsTXT.Mode == RobotsTXTModeCustom && c.HTTP.RobotsTXT.Content == "" {
		return ErrInvalidHTTPRobotsTXTContent
	}

	if c.HTTP.CanonicalHost == "" {
		return ErrHTTPCanonicalHostRequired
	}

	if c.Metrics.Enabled {
		if c.Metrics.Bind == "" {
			return ErrInvalidMetricsBindAddress
		}
		if c.Metrics.Port <= 0 || c.Metrics.Port > 65535 {
			return ErrInvalidMetricsPort
		}
	}

	if c.PProf.Enabled {
		if c.PProf.Bind == "" {
			return ErrInvalidPProfBindAddress
		}
		if c.PProf.Port <= 0 || c.PProf.Port > 65535 {
			return ErrInvalidPProfPort
		}
	}

	return nil
}
