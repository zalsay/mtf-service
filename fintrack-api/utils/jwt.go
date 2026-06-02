package utils

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID              int        `json:"user_id"`
	Email               string     `json:"email"`
	IsAdmin             bool       `json:"is_admin"`
	MembershipLevel     int        `json:"membership_level"`
	MembershipExpiresAt *time.Time `json:"membership_expires_at,omitempty"`
	jwt.RegisteredClaims
}

func GenerateJWT(userID int, email string, isAdmin bool, membershipLevel int, membershipExpiresAt *time.Time) (string, error) {
	secret := getJWTSecret()
	expiryHours := getJWTExpiryHours()

	claims := &Claims{
		UserID:              userID,
		Email:               email,
		IsAdmin:             isAdmin,
		MembershipLevel:     membershipLevel,
		MembershipExpiresAt: membershipExpiresAt,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expiryHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "fintrack-api",
			Subject:   strconv.Itoa(userID),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

func ValidateJWT(tokenString string) (*Claims, error) {
	secret := getJWTSecret()

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

func getJWTSecret() string {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "default_secret_change_in_production"
	}
	return secret
}

func getJWTExpiryHours() int {
	hoursStr := os.Getenv("JWT_EXPIRATION_HOURS")
	if hoursStr == "" {
		hoursStr = os.Getenv("JWT_EXPIRY_HOURS")
	}
	if hoursStr == "" {
		return 24 // default 24 hours
	}

	hours, err := strconv.Atoi(hoursStr)
	if err != nil {
		return 24 // fallback to default
	}

	return hours
}
