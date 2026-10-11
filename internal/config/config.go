package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ClickHouseDSN string
	Port          string

	// UI auth mode for the server-rendered UI under /ui/.
	// Values:
	// - "azure" (default): enforce UI auth via Azure OIDC session cookies
	// - "disabled": bypass UI auth checks (intended for local dev)
	UIAuthMode string

	// Azure UI auth (single-tenant; OIDC v2 endpoints).
	// Required when UIAuthMode == "azure".
	AzureTenantID     string
	AzureClientID     string
	AzureClientSecret string
	// Optional. If empty, derived from the request (scheme+host+"/ui/oauth2/callback").
	AzureRedirectURL string

	// Required when UIAuthMode == "azure".
	// Used to sign UI cookies (session + transient oauth state).
	UISessionSecret string

	// Optional allowlist (emails, UPNs or subject ids) for project/alert mutations in Azure mode.
	// Empty keeps the historical behavior: any signed-in user may manage projects.
	UIAdmins []string

	// WebUI live refresh polling interval (HTMX). Used by results containers via hx-trigger="every Ns".
	// Set via PYLOGGLE_UI_POLL_SECONDS. Defaults to 2.
	UIPollSeconds int

	// Ingestion protections
	MaxQueueSize      int
	MaxStreams        int
	MaxQueuePerStream int
	BatchSize         int
	BatchTimeout      time.Duration
	SharedSecret      string // optional; if set, requires X-API-Key to match
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

	// OTLP ingestion: store OK server spans as "request" log rows (off by default to limit volume).
	OTLPStoreServerSpans bool

	// PublicURL is the externally reachable base URL (for links in alerts), e.g. https://logs.example.com
	PublicURL string

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
	AlertTelegramParseMode string // optional: MarkdownV2 | HTML
}

func FromEnv() Config {
	uiAuthMode := strings.ToLower(strings.TrimSpace(getenv("PYLOGGLE_UI_AUTH", "azure")))
	if uiAuthMode != "azure" && uiAuthMode != "disabled" {
		uiAuthMode = "azure"
	}

	return Config{
		ClickHouseDSN:     getenv("CLICKHOUSE_DSN", "clickhouse://localhost:9000/pyloggle?dial_timeout=10s&read_timeout=20s"),
		Port:              getenv("INGEST_PORT", "8080"),
		UIAuthMode:        uiAuthMode,
		AzureTenantID:     strings.TrimSpace(os.Getenv("PYLOGGLE_AZURE_TENANT_ID")),
		AzureClientID:     strings.TrimSpace(os.Getenv("PYLOGGLE_AZURE_CLIENT_ID")),
		AzureClientSecret: os.Getenv("PYLOGGLE_AZURE_CLIENT_SECRET"),
		AzureRedirectURL:  strings.TrimSpace(os.Getenv("PYLOGGLE_AZURE_REDIRECT_URL")),
		UISessionSecret:   os.Getenv("PYLOGGLE_UI_SESSION_SECRET"),
		UIAdmins:          splitList(os.Getenv("PYLOGGLE_UI_ADMINS")),
		UIPollSeconds:     getenvInt("PYLOGGLE_UI_POLL_SECONDS", 2),
		MaxQueueSize:      getenvInt("INGEST_MAX_QUEUE_SIZE", 10000),
		MaxStreams:        getenvInt("INGEST_MAX_STREAMS", 200),
		MaxQueuePerStream: getenvInt("INGEST_MAX_QUEUE_PER_STREAM", 2000),
		BatchSize:         getenvInt("INGEST_BATCH_SIZE", 500),
		BatchTimeout:      getenvDuration("INGEST_BATCH_TIMEOUT", 5*time.Second),
		SharedSecret:      os.Getenv("INGEST_SHARED_SECRET"),
		AdminToken:        os.Getenv("PYLOGGLE_ADMIN_TOKEN"),
		DefaultEnv:        getenv("PYLOGGLE_DEFAULT_ENV", "prod"),
		DefaultSvc:        getenv("PYLOGGLE_DEFAULT_SERVICE", "unknown"),
		DefaultProjID:     getenv("PYLOGGLE_DEFAULT_PROJECT_ID", "default"),

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

		OTLPStoreServerSpans: getenvBool("OTLP_STORE_SERVER_SPANS", false),
		PublicURL:            strings.TrimRight(strings.TrimSpace(os.Getenv("PYLOGGLE_PUBLIC_URL")), "/"),

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
