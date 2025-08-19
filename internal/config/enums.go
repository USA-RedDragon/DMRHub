package config

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type DatabaseDriver string

const (
	DatabaseDriverSQLite   DatabaseDriver = "sqlite"
	DatabaseDriverPostgres DatabaseDriver = "postgres"
	DatabaseDriverMySQL    DatabaseDriver = "mysql"
)

type SMTPAuthMethod string

const (
	SMTPAuthMethodPlain SMTPAuthMethod = "plain"
	SMTPAuthMethodLogin SMTPAuthMethod = "login"
	SMTPAuthMethodNone  SMTPAuthMethod = "none"
)

type SMTPTLS string

const (
	SMTPTLSNone     SMTPTLS = "none"
	SMTPTLSStartTLS SMTPTLS = "starttls"
	SMTPTLSImplicit SMTPTLS = "implicit"
)

type RobotsTXTMode string

const (
	RobotsTXTModeAllow    RobotsTXTMode = "allow"
	RobotsTXTModeDisabled RobotsTXTMode = "disabled"
	RobotsTXTModeCustom   RobotsTXTMode = "custom"
)
