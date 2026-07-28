package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Database         DatabaseConfig
	Server           ServerConfig
	JWT              JWTConfig
	CORS             CORSConfig
	External         ExternalAPIConfig
	Redis            RedisConfig
	InferenceGateway InferenceGatewayConfig
	UZI              UZIServiceConfig
	OSS              OSSConfig
	LLM              LLMConfig
	PostgresHandler  PostgresHandlerConfig
	DSABridge        DSABridgeConfig
	MTFAgent         MTFAgentConfig
	AlipayService    AlipayServiceConfig
	OpenAPIV2        OpenAPIV2Config
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
}

type ServerConfig struct {
	Port        string
	Environment string
}

type JWTConfig struct {
	Secret     string
	Expiration int // hours
}

type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	AllowCredentials bool
}

type ExternalAPIConfig struct {
	AlphaVantageKey string
	PolygonKey      string
}

type RedisConfig struct {
	Enabled  bool
	Host     string
	Port     string
	Password string
	DB       int
}

type InferenceGatewayConfig struct {
	BaseURL string
	Timeout int // seconds
}

type UZIServiceConfig struct {
	Enabled      bool
	BaseURL      string
	QueueBaseURL string
	Timeout      int // seconds
	OpenTokenTTL int // seconds
}

type OSSConfig struct {
	Enabled            bool
	Endpoint           string
	Region             string
	Bucket             string
	PublicBaseURL      string
	AccessKeyID        string
	AccessKeySecret    string
	Prefix             string
	SignedURLTTL       int // seconds
	DisableSSL         bool
	UsePathStyle       bool
	ConnectTimeout     int // seconds
	ReadWriteTimeout   int // seconds
	InsecureSkipVerify bool
}

type LLMConfig struct {
	APIKey           string // OpenAI API Key
	BaseURL          string // OpenAI API Base URL (optional, for custom endpoints)
	DefaultModel     string // Default model (e.g., "gpt-3.5-turbo")
	MaxContextRounds int    // Maximum context rounds (default 3)
	Timeout          int    // API request timeout in seconds
}

type PostgresHandlerConfig struct {
	BaseURL  string // Postgres handler API base URL
	APIToken string // API token for authentication
	Timeout  int    // API request timeout in seconds
}

type DSABridgeConfig struct {
	SharedSecret string
	Issuer       string
}

type MTFAgentConfig struct {
	Enabled      bool
	BaseURL      string
	Timeout      int // seconds
	RuntimeToken string
	DefaultModel string
}

type AlipayServiceConfig struct {
	BaseURL      string
	APIToken     string
	ResourceID   string
	ResourceName string
	AmountCents  int
	Currency     string
	MerchantID   string
	MerchantName string
	Timeout      int
}

type OpenAPIV2Config struct {
	PrivateKey     string
	PrivateKeyFile string
	TimestampSkew  int // seconds
}

func LoadConfig() (*Config, error) {
	// 尝试加载.env文件
	godotenv.Load()

	config := &Config{
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", ""),
			DBName:   getEnv("DB_NAME", "fintrack_db"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Server: ServerConfig{
			Port:        getEnvWithAliases([]string{"SERVER_PORT", "PORT"}, "59000"),
			Environment: normalizeEnvironment(getEnvWithAliases([]string{"ENVIRONMENT", "GIN_MODE"}, "development")),
		},
		JWT: JWTConfig{
			Secret:     getEnv("JWT_SECRET", "your-secret-key"),
			Expiration: getEnvAsIntWithAliases([]string{"JWT_EXPIRATION_HOURS", "JWT_EXPIRY_HOURS"}, 24),
		},
		CORS: CORSConfig{
			AllowedOrigins:   splitAndTrim(getEnvWithAliases([]string{"CORS_ALLOWED_ORIGINS", "ALLOWED_ORIGINS"}, "*")),
			AllowedMethods:   splitAndTrim(getEnv("CORS_ALLOWED_METHODS", "GET,POST,PUT,PATCH,DELETE,OPTIONS")),
			AllowedHeaders:   splitAndTrim(getEnv("CORS_ALLOWED_HEADERS", "Origin,Content-Type,Accept,Authorization,X-Requested-With,Content-Length,captcha-verify-param")),
			AllowCredentials: getEnvAsBool("CORS_ALLOW_CREDENTIALS", true),
		},
		External: ExternalAPIConfig{
			AlphaVantageKey: getEnv("ALPHA_VANTAGE_API_KEY", ""),
			PolygonKey:      getEnv("POLYGON_API_KEY", ""),
		},
		Redis: RedisConfig{
			Enabled:  getEnvAsBool("REDIS_ENABLED", false),
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnv("REDIS_PORT", "6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvAsInt("REDIS_DB", 0),
		},
		InferenceGateway: InferenceGatewayConfig{
			BaseURL: getEnvWithAliases([]string{"INFERENCE_GATEWAY_URL", "PYTHON_SERVICE_URL"}, defaultInferenceGatewayURL()),
			Timeout: getEnvAsIntWithAliases([]string{"INFERENCE_GATEWAY_TIMEOUT", "PYTHON_SERVICE_TIMEOUT"}, 30),
		},
		UZI: UZIServiceConfig{
			Enabled:      getEnvAsBool("UZI_ENABLED", true),
			BaseURL:      getEnv("UZI_SERVICE_URL", defaultUZIServiceURL()),
			QueueBaseURL: getEnv("UZI_GATEWAY_URL", getEnvWithAliases([]string{"INFERENCE_GATEWAY_URL", "PYTHON_SERVICE_URL"}, defaultInferenceGatewayURL())),
			Timeout:      getEnvAsInt("UZI_SERVICE_TIMEOUT", 1800),
			OpenTokenTTL: getEnvAsInt("UZI_OPEN_TOKEN_TTL_SECONDS", 45),
		},
		OSS: OSSConfig{
			Enabled:            getEnvAsBool("OSS_ENABLED", false),
			Endpoint:           getEnv("OSS_ENDPOINT", ""),
			Region:             getEnv("OSS_REGION", ""),
			Bucket:             getEnv("OSS_BUCKET", ""),
			PublicBaseURL:      getEnv("OSS_PUBLIC_BASE_URL", ""),
			AccessKeyID:        getEnv("OSS_ACCESS_KEY_ID", ""),
			AccessKeySecret:    getEnv("OSS_ACCESS_KEY_SECRET", ""),
			Prefix:             getEnv("OSS_PREFIX", "uzi-reports"),
			SignedURLTTL:       getEnvAsInt("OSS_SIGNED_URL_TTL_SECONDS", 300),
			DisableSSL:         getEnvAsBool("OSS_DISABLE_SSL", false),
			UsePathStyle:       getEnvAsBool("OSS_USE_PATH_STYLE", false),
			ConnectTimeout:     getEnvAsInt("OSS_CONNECT_TIMEOUT", 10),
			ReadWriteTimeout:   getEnvAsInt("OSS_READ_WRITE_TIMEOUT", 30),
			InsecureSkipVerify: getEnvAsBool("OSS_INSECURE_SKIP_VERIFY", false),
		},
		LLM: LLMConfig{
			APIKey:           getEnv("OPENAI_API_KEY", ""),
			BaseURL:          getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			DefaultModel:     getEnv("OPENAI_DEFAULT_MODEL", "gpt-3.5-turbo"),
			MaxContextRounds: getEnvAsInt("OPENAI_MAX_CONTEXT_ROUNDS", 3),
			Timeout:          getEnvAsInt("OPENAI_TIMEOUT", 60),
		},
		PostgresHandler: PostgresHandlerConfig{
			BaseURL:  getEnv("POSTGRES_HANDLER_URL", "http://host.docker.internal:58004"),
			APIToken: getMTFServiceToken(),
			Timeout:  getEnvAsInt("POSTGRES_HANDLER_TIMEOUT", 10),
		},
		DSABridge: DSABridgeConfig{
			SharedSecret: getEnv("DSA_BRIDGE_SHARED_SECRET", ""),
			Issuer:       getEnv("DSA_BRIDGE_ISSUER", "daily_stock_analysis"),
		},
		MTFAgent: MTFAgentConfig{
			Enabled:      getEnvAsBool("MTF_AGENT_ENABLED", true),
			BaseURL:      strings.TrimRight(getEnv("MTF_AGENT_RUNTIME_URL", "http://ai-functions-gateway:9010/deepseek-tui"), "/"),
			Timeout:      getEnvAsInt("MTF_AGENT_TIMEOUT", 120),
			RuntimeToken: getMTFServiceToken(),
			DefaultModel: getEnv("MTF_AGENT_MODEL", "deepseek-v4-pro"),
		},
		AlipayService: AlipayServiceConfig{
			BaseURL:      strings.TrimRight(getEnv("ALIPAY_SERVICE_URL", "http://127.0.0.1:59100"), "/"),
			APIToken:     getEnv("ALIPAY_SERVICE_TOKEN", ""),
			ResourceID:   getEnv("ALIPAY_RESOURCE_ID", "mtf.predict.once"),
			ResourceName: getEnv("ALIPAY_RESOURCE_NAME", "MTF 单次预测"),
			AmountCents:  getEnvAsInt("ALIPAY_AI_PAY_AMOUNT_CENTS", 199),
			Currency:     getEnv("ALIPAY_AI_PAY_CURRENCY", "CNY"),
			MerchantID:   getEnv("ALIPAY_MERCHANT_ID", "dev-merchant"),
			MerchantName: getEnv("ALIPAY_MERCHANT_NAME", "FinTrack"),
			Timeout:      getEnvAsInt("ALIPAY_SERVICE_TIMEOUT", 10),
		},
		OpenAPIV2: OpenAPIV2Config{
			PrivateKey:     getEnv("MTF_V2_API_PRIVATE_KEY", ""),
			PrivateKeyFile: getEnv("MTF_V2_API_PRIVATE_KEY_FILE", ""),
			TimestampSkew:  getEnvAsInt("MTF_V2_API_TIMESTAMP_SKEW_SECONDS", 300),
		},
	}

	return config, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvWithAliases(keys []string, defaultValue string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return defaultValue
}

func getMTFServiceToken() string {
	return getEnvWithAliases([]string{
		"MTF_SERVICE_TOKEN",
		"POSTGRES_HANDLER_TOKEN",
		"MTF_AGENT_RUNTIME_TOKEN",
		"GATEWAY_API_TOKEN",
		"DEEPSEEK_TUI_PROXY_TOKEN",
	}, "fintrack-dev-token")
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsIntWithAliases(keys []string, defaultValue int) int {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			if intValue, err := strconv.Atoi(value); err == nil {
				return intValue
			}
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func splitAndTrim(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		return []string{"*"}
	}
	return values
}

func normalizeEnvironment(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "prod", "production", "release":
		return "production"
	default:
		return "development"
	}
}
func defaultInferenceGatewayURL() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "http://host.docker.internal:59010"
	}
	return "http://127.0.0.1:59010"
}

func defaultUZIServiceURL() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "http://host.docker.internal:59011"
	}
	return "http://127.0.0.1:59011"
}
