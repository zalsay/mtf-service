package payment

import (
	"strings"
	"time"
)

func NewVerifier(cfg Config) Verifier {
	if strings.EqualFold(strings.TrimSpace(cfg.Mode), "alipay") {
		return NewAlipayVerifier(cfg)
	}
	ttlSeconds := cfg.LocalTTL.Seconds()
	if ttlSeconds <= 0 {
		ttlSeconds = 600
	}
	return NewLocalVerifier(cfg.LocalSecret, time.Duration(ttlSeconds)*time.Second)
}
