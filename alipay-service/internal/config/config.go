package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"alipay-service/internal/payment"
	"alipay-service/internal/server"
)

type Config struct {
	Server  server.Config
	Payment payment.Config
	Port    string
}

func Load() Config {
	amountCents := envInt("ALIPAY_AI_PAY_AMOUNT_CENTS", 199)
	return Config{
		Port: env("PORT", "59100"),
		Server: server.Config{
			MerchantID:   env("ALIPAY_MERCHANT_ID", "dev-merchant"),
			MerchantName: env("ALIPAY_MERCHANT_NAME", "FinTrack"),
			ResourceID:   env("ALIPAY_RESOURCE_ID", "mtf.predict.once"),
			ResourceName: env("ALIPAY_RESOURCE_NAME", "MTF 单次预测"),
			AmountCents:  amountCents,
			Currency:     env("ALIPAY_AI_PAY_CURRENCY", "CNY"),
		},
		Payment: payment.Config{
			Mode:              env("ALIPAY_AI_PAY_MODE", "local"),
			LocalSecret:       env("ALIPAY_AI_PAY_LOCAL_SECRET", "change-me"),
			LocalTTL:          time.Duration(envInt("ALIPAY_AI_PAY_LOCAL_TTL_SECONDS", 600)) * time.Second,
			CredentialAPIURL:  env("ALIPAY_CREDENTIAL_API_URL", ""),
			FulfillmentAPIURL: env("ALIPAY_FULFILLMENT_API_URL", ""),
			AppID:             env("ALIPAY_APP_ID", ""),
			PrivateKey:        env("ALIPAY_APP_PRIVATE_KEY", ""),
			AlipayPublicKey:   env("ALIPAY_PUBLIC_KEY", ""),
		},
	}
}

func env(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
