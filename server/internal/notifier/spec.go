package notifier

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"

	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

// Spec is the per-type configuration of a channel.
//
// Validate returns *utils.ValidationError with a dotted field path, so the API
// layer answers 400 {error, field} for a bad token with no per-transport code at
// the boundary -- the same funnel task specs already go through.
type Spec interface {
	Validate() error
	// Redacted is the view safe to hand back over the API: secrets masked to
	// their last four characters, everything else intact. A channel's secrets
	// are write-only from the dashboard's perspective.
	Redacted() map[string]any
}

// mask shows enough of a secret to recognise which one it is, and not enough to
// use it. Short values are hidden entirely rather than mostly revealed.
func mask(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return "••••"
	}
	return "••••" + secret[len(secret)-4:]
}

// --- Telegram ---------------------------------------------------------------

type TelegramSpec struct {
	Token  string `json:"token"`
	ChatID string `json:"chat_id"`
}

func (s *TelegramSpec) Validate() error {
	if strings.TrimSpace(s.Token) == "" {
		return utils.Fail("config.token", "Bot token is required. Create a bot with @BotFather to get one.")
	}
	if strings.TrimSpace(s.ChatID) == "" {
		return utils.Fail("config.chat_id", "Chat ID is required. Message @userinfobot to find yours.")
	}
	return nil
}

func (s *TelegramSpec) Redacted() map[string]any {
	return map[string]any{"token": mask(s.Token), "chat_id": s.ChatID}
}

// --- Discord ----------------------------------------------------------------

type DiscordSpec struct {
	WebhookURL string `json:"webhook_url"`
}

func (s *DiscordSpec) Validate() error {
	return validateWebhookURL("config.webhook_url", s.WebhookURL,
		"Webhook URL is required. Server Settings → Integrations → Webhooks.")
}

func (s *DiscordSpec) Redacted() map[string]any {
	return map[string]any{"webhook_url": maskURL(s.WebhookURL)}
}

// --- Slack ------------------------------------------------------------------

type SlackSpec struct {
	WebhookURL string `json:"webhook_url"`
}

func (s *SlackSpec) Validate() error {
	return validateWebhookURL("config.webhook_url", s.WebhookURL,
		"Webhook URL is required. Create one at api.slack.com/apps → Incoming Webhooks.")
}

func (s *SlackSpec) Redacted() map[string]any {
	return map[string]any{"webhook_url": maskURL(s.WebhookURL)}
}

// --- Generic webhook --------------------------------------------------------

type WebhookSpec struct {
	URL string `json:"url"`
	// Defaults to POST. Only POST and PUT are accepted -- an alert carries a
	// body, and a GET that puts it in the query string would leak it into logs.
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (s *WebhookSpec) Validate() error {
	if err := validateWebhookURL("config.url", s.URL, "URL is required."); err != nil {
		return err
	}

	if s.Method != "" {
		method := strings.ToUpper(s.Method)
		if method != "POST" && method != "PUT" {
			return utils.Fail("config.method", "Method must be POST or PUT, got %q.", s.Method)
		}
	}

	for name := range s.Headers {
		if strings.TrimSpace(name) == "" {
			return utils.Fail("config.headers", "Header names cannot be empty.")
		}
	}

	return nil
}

// method is the verb to send with, defaulted once so the transport does not
// repeat the fallback.
func (s *WebhookSpec) method() string {
	if s.Method == "" {
		return "POST"
	}
	return strings.ToUpper(s.Method)
}

func (s *WebhookSpec) Redacted() map[string]any {
	// Headers commonly carry an Authorization value, so mask every one rather
	// than guessing which names are sensitive.
	headers := map[string]any{}
	for name, value := range s.Headers {
		headers[name] = mask(value)
	}

	return map[string]any{
		"url":     s.URL,
		"method":  s.method(),
		"headers": headers,
	}
}

// --- Email (SMTP) -----------------------------------------------------------

type EmailSpec struct {
	// Defaults to Gmail, which is what this is for, but any SMTP server that
	// speaks STARTTLS works.
	Host        string   `json:"host,omitempty"`
	Port        int      `json:"port,omitempty"`
	Username    string   `json:"username"`
	AppPassword string   `json:"app_password"`
	From        string   `json:"from,omitempty"`
	To          []string `json:"to"`
}

func (s *EmailSpec) Validate() error {
	if strings.TrimSpace(s.Username) == "" {
		return utils.Fail("config.username", "Username is required -- your full email address.")
	}
	if strings.TrimSpace(s.AppPassword) == "" {
		return utils.Fail("config.app_password",
			"App password is required. Gmail rejects your account password: create an app password at myaccount.google.com/apppasswords.")
	}
	if s.Port < 0 || s.Port > 65535 {
		return utils.Fail("config.port", "Port must be between 1 and 65535, got %d.", s.Port)
	}
	if s.From != "" {
		if _, err := mail.ParseAddress(s.From); err != nil {
			return utils.Fail("config.from", "%q is not a valid email address.", s.From)
		}
	}

	recipients := s.recipients()
	if len(recipients) == 0 {
		return utils.Fail("config.to", "At least one recipient is required.")
	}
	for _, address := range recipients {
		if _, err := mail.ParseAddress(address); err != nil {
			return utils.Fail("config.to", "%q is not a valid email address.", address)
		}
	}

	return nil
}

func (s *EmailSpec) host() string {
	if s.Host == "" {
		return types.DefaultSMTPHost
	}
	return s.Host
}

func (s *EmailSpec) port() int {
	if s.Port == 0 {
		return types.DefaultSMTPPort
	}
	return s.Port
}

// from defaults to the authenticated user, which is the only sender Gmail will
// accept anyway.
func (s *EmailSpec) from() string {
	if s.From == "" {
		return s.Username
	}
	return s.From
}

func (s *EmailSpec) recipients() []string {
	addresses := []string{}
	for _, address := range s.To {
		if trimmed := strings.TrimSpace(address); trimmed != "" {
			addresses = append(addresses, trimmed)
		}
	}
	return addresses
}

func (s *EmailSpec) Redacted() map[string]any {
	return map[string]any{
		"host":         s.host(),
		"port":         s.port(),
		"username":     s.Username,
		"app_password": mask(s.AppPassword),
		"from":         s.from(),
		"to":           s.recipients(),
	}
}

// --- Shared validation ------------------------------------------------------

func validateWebhookURL(field, raw, missing string) error {
	if strings.TrimSpace(raw) == "" {
		return utils.Fail(field, "%s", missing)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return utils.Fail(field, "%q is not a valid URL.", raw)
	}
	// Rejected here rather than at send time: a channel that can never deliver
	// should not be savable, because the failure would otherwise surface as a
	// missed alert.
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return utils.Fail(field, "URL must start with http:// or https://, got %q.", raw)
	}
	if parsed.Host == "" {
		return utils.Fail(field, "%q is missing a host.", raw)
	}

	return nil
}

// maskURL keeps a webhook URL recognisable while hiding the token in its path --
// for Slack and Discord the path *is* the credential.
func maskURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return mask(raw)
	}
	return fmt.Sprintf("%s://%s/••••", parsed.Scheme, parsed.Host)
}
