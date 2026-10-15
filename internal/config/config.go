package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ClickHouseDSN string
	Port          string

	// UI auth mode for the single-page app under /ui/ (PYLOGGLE_UI_AUTH; see inferUIAuthMode):
	// - "oidc": any OpenID Connect provider (PYLOGGLE_OIDC_*)
	// - "azure": Microsoft Entra ID single-tenant login (an OIDC preset)
	// - "disabled": no sign-in, for a trusted network only
	UIAuthMode string

	// Azure preset (single-tenant; OIDC v2 endpoints). Required when UIAuthMode == "azure".
	AzureTenantID     string
	AzureClientID     string
	AzureClientSecret string
	// Optional. If empty, derived from the request (scheme+host+"/ui/oauth2/callback").
	AzureRedirectURL string

	// Generic OIDC (PYLOGGLE_OIDC_*). Required when UIAuthMode == "oidc": the issuer serves
	// /.well-known/openid-configuration; client id + secret identify this app.
	OIDCIssuer         string
	OIDCClientID       string
	OIDCClientSecret   string
	OIDCScopes         string   // default "openid profile email"
	OIDCRedirectURL    string   // optional, derived from the request when empty
	OIDCDisplayName    string   // login button label, e.g. "Okta"
	OIDCAllowedDomains []string // optional e-mail domain allow-list
	OIDCAllowedEmails  []string // optional e-mail allow-list

	// Required when UI auth is on (azure or oidc). Used to sign UI cookies (session + transient
	// oauth state).
	UISessionSecret string

	// Optional allowlist (emails, UPNs or subject ids) for project, key and alert changes and
	// the System page when sign-in is on. Empty: any signed-in user may manage them.
	UIAdmins []string

	// Ingestion protections
	MaxQueueSize      int
	MaxStreams        int
	MaxQueuePerStream int
	BatchSize         int
	BatchTimeout      time.Duration
	SharedSecret      string // optional 0.1 shared key: sends to the default project
	AdminToken        string // optional; for project management endpoints
	DefaultEnv        string
	DefaultSvc        string
	DefaultProjID     string

	// Rate limiting (per project+service) and sampling (non-errors only).
	RateLimitRPS    float64
	RateLimitBurst  float64
	SampleDebug     float64
	SampleInfo      float64
	SampleWarn      float64
	SampleByRequest bool // seed sampling by request_id so a kept request keeps all its lines

	// Schema management and retention.
	AutoMigrate          bool // apply embedded migrations at startup
	MigrateRebuild       bool // v2 upgrade: rebuild event tables with daily partitions (copies the retention window)
	RetentionLogsDays    int
	RetentionErrorsDays  int
	RetentionStatsDays   int
	RetentionSpansDays   int
	RetentionMetricsDays int

	// OTLP ingestion. OTLPStoreSpans keeps every span in the spans table (7-day TTL by default).
	// Every OK server span is counted as a request; OTLPStoreServerSpans also lists it on Logs as
	// a "request" row, like the Python package's request lines.
	OTLPStoreSpans       bool
	OTLPStoreServerSpans bool
	OTLPStoreMetrics     bool // keep OTLP metric points (30-day TTL by default)

	// PublicURL is the externally reachable base URL (for links in alerts), e.g. https://logs.example.com
	PublicURL string

	// ExportMaxRows caps one CSV/NDJSON export (PYLOGGLE_EXPORT_MAX_ROWS, default 100000).
	ExportMaxRows int

	// MetricsToken, when set, is the bearer token GET /metrics requires (PYLOGGLE_METRICS_TOKEN).
	MetricsToken string

	// DocsURL is where the app's Help page and "Learn more" links point (PYLOGGLE_DOCS_URL):
	// the public docs by default, an internal mirror, or empty to hide the links.
	DocsURL string

	// Alerting (optional)
	AlertingEnabled      bool
	AlertingPollInterval time.Duration
	// Evaluation windows end this long before "now" so batches still in flight are not counted
	// as missing (absence monitors) or under-counted (threshold monitors).
	AlertingEvalDelay time.Duration
	// Slack incoming webhook used when a rule lists the "slack" channel without its own URLs.
	AlertSlackWebhookURL string

	// Email notifier (SMTP). If SMTPHost is empty, email notifications are disabled.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPTLS      bool // if true, connect using implicit TLS (SMTPS) to host:port

	// Webhook notifier
	AlertWebhookTimeout time.Duration

	// Telegram notifier (Bot API). If AlertTelegramBotToken is empty, Telegram notifications are disabled.
	AlertTelegramBotToken  string
	AlertTelegramTimeout   time.Duration
	AlertTelegramParseMode string // HTML (default) | none
	AlertTelegramAPIURL    string // Bot API base URL; a self-hosted Bot API server, or tests
}

func FromEnv() Config {
	uiAuthMode := inferUIAuthMode()
	defaultProject, ok := os.LookupEnv("PYLOGGLE_DEFAULT_PROJECT_ID")
	if !ok {
		defaultProject = "default" // set it empty to require an access key everywhere
	}
	docsURL, ok := os.LookupEnv("PYLOGGLE_DOCS_URL")
	if !ok {
		docsURL = DocsHome // set it empty to hide the links to the docs
	}
	if docsURL = strings.TrimSpace(docsURL); docsURL != "" && !strings.HasSuffix(docsURL, "/") {
		docsURL += "/"
	}

	return Config{
		ClickHouseDSN:      getenv("CLICKHOUSE_DSN", "clickhouse://localhost:9000/pyloggle?dial_timeout=10s&read_timeout=20s"),
		Port:               getenv("INGEST_PORT", "8080"),
		UIAuthMode:         uiAuthMode,
		AzureTenantID:      strings.TrimSpace(os.Getenv("PYLOGGLE_AZURE_TENANT_ID")),
		AzureClientID:      strings.TrimSpace(os.Getenv("PYLOGGLE_AZURE_CLIENT_ID")),
		AzureClientSecret:  os.Getenv("PYLOGGLE_AZURE_CLIENT_SECRET"),
		AzureRedirectURL:   strings.TrimSpace(os.Getenv("PYLOGGLE_AZURE_REDIRECT_URL")),
		OIDCIssuer:         strings.TrimRight(strings.TrimSpace(os.Getenv("PYLOGGLE_OIDC_ISSUER")), "/"),
		OIDCClientID:       strings.TrimSpace(os.Getenv("PYLOGGLE_OIDC_CLIENT_ID")),
		OIDCClientSecret:   os.Getenv("PYLOGGLE_OIDC_CLIENT_SECRET"),
		OIDCScopes:         getenv("PYLOGGLE_OIDC_SCOPES", "openid profile email"),
		OIDCRedirectURL:    strings.TrimSpace(os.Getenv("PYLOGGLE_OIDC_REDIRECT_URL")),
		OIDCDisplayName:    strings.TrimSpace(os.Getenv("PYLOGGLE_OIDC_DISPLAY_NAME")),
		OIDCAllowedDomains: splitList(os.Getenv("PYLOGGLE_OIDC_ALLOWED_DOMAINS")),
		OIDCAllowedEmails:  splitList(os.Getenv("PYLOGGLE_OIDC_ALLOWED_EMAILS")),
		UISessionSecret:    os.Getenv("PYLOGGLE_UI_SESSION_SECRET"),
		UIAdmins:           splitList(os.Getenv("PYLOGGLE_UI_ADMINS")),
		MaxQueueSize:       getenvInt("INGEST_MAX_QUEUE_SIZE", 10000),
		MaxStreams:         getenvInt("INGEST_MAX_STREAMS", 200),
		MaxQueuePerStream:  getenvInt("INGEST_MAX_QUEUE_PER_STREAM", 2000),
		BatchSize:          getenvInt("INGEST_BATCH_SIZE", 500),
		BatchTimeout:       getenvDuration("INGEST_BATCH_TIMEOUT", 5*time.Second),
		SharedSecret:       os.Getenv("INGEST_SHARED_SECRET"),
		AdminToken:         os.Getenv("PYLOGGLE_ADMIN_TOKEN"),
		DefaultEnv:         getenv("PYLOGGLE_DEFAULT_ENV", "prod"),
		DefaultSvc:         getenv("PYLOGGLE_DEFAULT_SERVICE", "unknown"),
		DefaultProjID:      strings.TrimSpace(defaultProject),

		RateLimitRPS:    getenvFloat("INGEST_RATE_LIMIT_RPS", 2000),   // per service by default
		RateLimitBurst:  getenvFloat("INGEST_RATE_LIMIT_BURST", 4000), // allow bursts
		SampleDebug:     getenvFloat("INGEST_SAMPLE_DEBUG", 0.10),
		SampleInfo:      getenvFloat("INGEST_SAMPLE_INFO", 1.00), // a log explorer must show the INFO lines people search for
		SampleWarn:      getenvFloat("INGEST_SAMPLE_WARN", 1.00),
		SampleByRequest: getenvBool("INGEST_SAMPLE_BY_REQUEST", true),

		AutoMigrate:          getenvBool("PYLOGGLE_AUTO_MIGRATE", true),
		MigrateRebuild:       getenvBool("PYLOGGLE_MIGRATE_REBUILD", true),
		RetentionLogsDays:    getenvInt("PYLOGGLE_RETENTION_LOGS_DAYS", 30),
		RetentionErrorsDays:  getenvInt("PYLOGGLE_RETENTION_ERRORS_DAYS", 30),
		RetentionStatsDays:   getenvInt("PYLOGGLE_RETENTION_STATS_DAYS", 365),
		RetentionSpansDays:   getenvInt("PYLOGGLE_RETENTION_SPANS_DAYS", 7),
		RetentionMetricsDays: getenvInt("PYLOGGLE_RETENTION_METRICS_DAYS", 30),

		OTLPStoreSpans:       getenvBool("OTLP_STORE_SPANS", true),
		OTLPStoreServerSpans: getenvBool("OTLP_STORE_SERVER_SPANS", true),
		OTLPStoreMetrics:     getenvBool("OTLP_STORE_METRICS", true),
		PublicURL:            strings.TrimRight(strings.TrimSpace(os.Getenv("PYLOGGLE_PUBLIC_URL")), "/"),
		ExportMaxRows:        getenvInt("PYLOGGLE_EXPORT_MAX_ROWS", 100000),
		MetricsToken:         strings.TrimSpace(os.Getenv("PYLOGGLE_METRICS_TOKEN")),
		DocsURL:              docsURL,

		AlertingEnabled:      getenvBool("ALERTING_ENABLED", false),
		AlertingPollInterval: getenvDuration("ALERTING_POLL_INTERVAL", 30*time.Second),
		AlertingEvalDelay:    getenvDuration("ALERTING_EVAL_DELAY", 30*time.Second),
		AlertSlackWebhookURL: strings.TrimSpace(os.Getenv("ALERT_SLACK_WEBHOOK_URL")),

		SMTPHost:     os.Getenv("ALERT_SMTP_HOST"),
		SMTPPort:     getenvInt("ALERT_SMTP_PORT", 587),
		SMTPUsername: os.Getenv("ALERT_SMTP_USERNAME"),
		SMTPPassword: os.Getenv("ALERT_SMTP_PASSWORD"),
		SMTPFrom:     os.Getenv("ALERT_SMTP_FROM"),
		SMTPTLS:      getenvBool("ALERT_SMTP_TLS", false),

		AlertWebhookTimeout: getenvDuration("ALERT_WEBHOOK_TIMEOUT", 5*time.Second),

		AlertTelegramBotToken:  os.Getenv("ALERT_TELEGRAM_BOT_TOKEN"),
		AlertTelegramTimeout:   getenvDuration("ALERT_TELEGRAM_TIMEOUT", 5*time.Second),
		AlertTelegramParseMode: os.Getenv("ALERT_TELEGRAM_PARSE_MODE"),
		AlertTelegramAPIURL:    getenv("ALERT_TELEGRAM_API_URL", "https://api.telegram.org"),
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func getenvDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func getenvFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			return f
		}
	}
	return def
}

func getenvBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "t", "yes", "y", "on":
			return true
		case "0", "false", "f", "no", "n", "off":
			return false
		}
	}
	return def
}

// inferUIAuthMode reads PYLOGGLE_UI_AUTH. When it is unset, a configured Azure tenant means
// "azure" and a configured OIDC issuer means "oidc"; otherwise (or when both are set) the mode
// stays empty and Validate asks the operator to choose. There is no silent default: a server
// that does not know how people sign in must not start.
func inferUIAuthMode() string {
	if mode := strings.ToLower(strings.TrimSpace(os.Getenv("PYLOGGLE_UI_AUTH"))); mode != "" {
		return mode
	}
	tenant := strings.TrimSpace(os.Getenv("PYLOGGLE_AZURE_TENANT_ID")) != ""
	issuer := strings.TrimSpace(os.Getenv("PYLOGGLE_OIDC_ISSUER")) != ""
	switch {
	case tenant && !issuer:
		return "azure"
	case issuer && !tenant:
		return "oidc"
	}
	return ""
}

// DocsHome is the public documentation; DocsSignIn is where startup errors about sign-in point.
const (
	DocsHome   = "https://koneb71.github.io/pyloggle/"
	DocsSignIn = DocsHome + "self-hosting/sign-in"
)

// Validate reports configuration that would make the server unsafe or unusable, every problem
// at once. main refuses to start when it returns an error.
func (c Config) Validate() error {
	var errs []error
	switch mode := c.UIAuthMode; mode {
	case "":
		if strings.TrimSpace(c.AzureTenantID) != "" && strings.TrimSpace(c.OIDCIssuer) != "" {
			errs = append(errs, errors.New("both PYLOGGLE_AZURE_TENANT_ID and PYLOGGLE_OIDC_ISSUER are set: choose one with PYLOGGLE_UI_AUTH=azure or PYLOGGLE_UI_AUTH=oidc"))
		} else {
			errs = append(errs, errors.New("sign-in is not configured: set PYLOGGLE_UI_AUTH to oidc (any OpenID Connect provider) or azure (Microsoft Entra ID) "+
				"with their settings, or to disabled to run without sign-in on a trusted network only. See "+DocsSignIn))
		}
	case "azure", "oidc":
		if msg := c.OIDC().Missing(c.UISessionSecret); msg != "" {
			errs = append(errs, errors.New(msg))
		} else if _, err := DecodeSessionSecret(c.UISessionSecret); err != nil {
			errs = append(errs, err)
		}
	case "disabled":
	default:
		errs = append(errs, fmt.Errorf("PYLOGGLE_UI_AUTH=%q is not a sign-in mode: use oidc, azure or disabled", mode))
	}
	return errors.Join(errs...)
}

// DecodeSessionSecret turns PYLOGGLE_UI_SESSION_SECRET into the key that signs session cookies:
// base64 for 16+ bytes (e.g. `openssl rand -base64 32`), or a raw string of 32+ characters.
func DecodeSessionSecret(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("PYLOGGLE_UI_SESSION_SECRET is required when PYLOGGLE_UI_AUTH is azure or oidc (create one with: openssl rand -base64 32)")
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) >= 16 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(raw); err == nil && len(b) >= 16 {
		return b, nil
	}
	if len(raw) < 32 {
		return nil, errors.New("PYLOGGLE_UI_SESSION_SECRET must be at least 32 characters, or base64 for 16+ bytes (create one with: openssl rand -base64 32)")
	}
	return []byte(raw), nil
}

// OIDCSettings are the effective OpenID Connect parameters for the configured UI auth mode.
type OIDCSettings struct {
	Mode           string // azure | oidc | disabled
	Issuer         string
	ClientID       string
	ClientSecret   string
	Scopes         string
	RedirectURL    string
	DisplayName    string
	AllowedDomains []string
	AllowedEmails  []string
	// Azure is the preset: fixed endpoints, tenant check, object id as subject.
	Azure       bool
	AzureTenant string
}

// OIDC resolves the login settings: the Azure preset derives its issuer from the tenant and
// reuses the PYLOGGLE_AZURE_* values; "oidc" reads PYLOGGLE_OIDC_*.
func (c Config) OIDC() OIDCSettings {
	mode := strings.ToLower(strings.TrimSpace(c.UIAuthMode))
	switch mode {
	case "azure":
		tid := strings.TrimSpace(c.AzureTenantID)
		return OIDCSettings{Mode: mode, Azure: true, AzureTenant: tid,
			Issuer: "https://login.microsoftonline.com/" + tid + "/v2.0", ClientID: strings.TrimSpace(c.AzureClientID), ClientSecret: strings.TrimSpace(c.AzureClientSecret),
			Scopes: "openid profile email", RedirectURL: strings.TrimSpace(c.AzureRedirectURL), DisplayName: "Microsoft",
			AllowedDomains: c.OIDCAllowedDomains, AllowedEmails: c.OIDCAllowedEmails}
	case "oidc":
		name := c.OIDCDisplayName
		if name == "" {
			name = "SSO"
		}
		scopes := strings.TrimSpace(c.OIDCScopes)
		if scopes == "" {
			scopes = "openid profile email"
		}
		return OIDCSettings{Mode: mode, Issuer: c.OIDCIssuer, ClientID: c.OIDCClientID, ClientSecret: strings.TrimSpace(c.OIDCClientSecret), Scopes: scopes,
			RedirectURL: c.OIDCRedirectURL, DisplayName: name, AllowedDomains: c.OIDCAllowedDomains, AllowedEmails: c.OIDCAllowedEmails}
	case "disabled":
		return OIDCSettings{Mode: "disabled"}
	}
	// Empty or unknown: Validate stops the server; anything that runs anyway treats it as
	// "sign-in required" (see webui.loginRequired), never as "disabled".
	return OIDCSettings{Mode: mode}
}

// Missing names the settings a sign-in mode still needs, or returns "" when it has them.
func (o OIDCSettings) Missing(sessionSecret string) string {
	var need []string
	if o.Azure {
		if o.AzureTenant == "" {
			need = append(need, "PYLOGGLE_AZURE_TENANT_ID")
		}
		if o.ClientID == "" {
			need = append(need, "PYLOGGLE_AZURE_CLIENT_ID")
		}
		if o.ClientSecret == "" {
			need = append(need, "PYLOGGLE_AZURE_CLIENT_SECRET")
		}
	} else {
		if o.Issuer == "" {
			need = append(need, "PYLOGGLE_OIDC_ISSUER")
		}
		if o.ClientID == "" {
			need = append(need, "PYLOGGLE_OIDC_CLIENT_ID")
		}
		if o.ClientSecret == "" {
			need = append(need, "PYLOGGLE_OIDC_CLIENT_SECRET")
		}
	}
	if strings.TrimSpace(sessionSecret) == "" {
		need = append(need, "PYLOGGLE_UI_SESSION_SECRET")
	}
	if len(need) == 0 {
		return ""
	}
	return fmt.Sprintf("sign-in with %s is turned on but not configured: set %s. See %s", o.Mode, strings.Join(need, ", "), DocsSignIn)
}

// Enabled reports whether the UI requires a login session.
func (o OIDCSettings) Enabled() bool { return o.Mode == "azure" || o.Mode == "oidc" }
