package smtp

import (
	"fmt"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/logging"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

func Send(toEmail string, subject string, body string) error {
	config := config.GetConfig()

	if !config.EnableEmail {
		logging.Errorf("Email is disabled, but an email was attempted to be sent")
		return fmt.Errorf("email is disabled, but an email was attempted to be sent")
	}

	var auth sasl.Client
	switch config.SMTPAuthMethod {
	case "PLAIN":
		auth = sasl.NewPlainClient("", config.SMTPUsername, config.SMTPPassword)
	case "LOGIN":
		auth = sasl.NewLoginClient(config.SMTPUsername, config.SMTPPassword)
	default:
		auth = nil
		logging.Errorf("Invalid SMTP auth method: %s", config.SMTPAuthMethod)
		return fmt.Errorf("invalid SMTP auth method: %s", config.SMTPAuthMethod)
	}

	msg := strings.NewReader(fmt.Sprintf("To: %s\r\n", toEmail) +
		fmt.Sprintf("Subject: %s\r\n", subject) +
		"\r\n" +
		body +
		"\r\n")

	err := smtp.SendMail(
		config.SMTPHost+":"+fmt.Sprint(config.SMTPPort),
		auth,
		config.SMTPFrom,
		[]string{toEmail},
		msg)
	if err != nil {
		logging.Errorf("Error sending email: %s", err)
		return fmt.Errorf("error sending email: %s", err)
	}
	return nil
}
