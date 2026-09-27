package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const jwtSecretKey = "jwt_secret"

type Auth struct {
	db              *state.DB
	jwtSecret       []byte
	sessionDuration time.Duration
	bcryptCost      int
	lanOnly         bool
	logger          *zap.Logger
}

type Claims struct {
	jwt.RegisteredClaims
}

func New(db *state.DB, sessionDuration time.Duration, bcryptCost int, lanOnly bool, logger *zap.Logger) *Auth {
	secret := loadOrCreateJWTSecret(db, logger)

	return &Auth{
		db:              db,
		jwtSecret:       secret,
		sessionDuration: sessionDuration,
		bcryptCost:      bcryptCost,
		lanOnly:         lanOnly,
		logger:          logger,
	}
}

// loadOrCreateJWTSecret persists the signing key in setup_state so sessions
// survive process restarts and reboots.
func loadOrCreateJWTSecret(db *state.DB, logger *zap.Logger) []byte {
	stored, err := db.GetSetupState(jwtSecretKey)
	if err == nil && stored != "" {
		if decoded, decErr := hex.DecodeString(stored); decErr == nil && len(decoded) >= 32 {
			return decoded
		}
		// Legacy / non-hex values: use raw bytes if long enough
		if len(stored) >= 32 {
			return []byte(stored)
		}
		if logger != nil {
			logger.Warn("stored jwt_secret is invalid; generating a new one")
		}
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		// Extremely unlikely; fall back to a non-crypto placeholder so boot continues
		raw = []byte("opendeploy-insecure-fallback-secret!!")
	}
	encoded := hex.EncodeToString(raw)
	if err := db.SetSetupState(jwtSecretKey, encoded); err != nil && logger != nil {
		logger.Warn("failed to persist jwt_secret; sessions will not survive restart", zap.Error(err))
	}
	return raw
}

func (a *Auth) IsPasswordSet() bool {
	hash, err := a.db.GetPasswordHash()
	return err == nil && hash != ""
}

func (a *Auth) SetPassword(password string) error {
	if len(password) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), a.bcryptCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	return a.db.SetPasswordHash(string(hash))
}

func (a *Auth) ValidatePassword(password string) bool {
	hash, err := a.db.GetPasswordHash()
	if err != nil || hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (a *Auth) GenerateToken() (string, error) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.sessionDuration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        generateID(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.jwtSecret)
}

func (a *Auth) ValidateToken(tokenString string) bool {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.jwtSecret, nil
	})

	return err == nil && token.Valid
}

// Middleware returns an HTTP middleware that enforces authentication.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// LAN-only check
		if a.lanOnly && !isLANRequest(r) {
			http.Error(w, `{"error":"access restricted to local network"}`, http.StatusForbidden)
			return
		}

		// Skip auth if no password set yet (first boot)
		if !a.IsPasswordSet() {
			next.ServeHTTP(w, r)
			return
		}

		// Check cookie
		cookie, err := r.Cookie("opendeploy_session")
		if err != nil {
			// Check Authorization header as fallback
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if !a.ValidateToken(tokenString) {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		if !a.ValidateToken(cookie.Value) {
			http.Error(w, `{"error":"session expired"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SetSessionCookie sets the session JWT as an httpOnly cookie.
func (a *Auth) SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "opendeploy_session",
		Value:    token,
		Path:     "/",
		MaxAge:   int(a.sessionDuration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearSessionCookie removes the session cookie.
func (a *Auth) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "opendeploy_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

func isLANRequest(r *http.Request) bool {
	ip := r.RemoteAddr
	// Strip port
	if idx := strings.LastIndex(ip, ":"); idx > 0 {
		ip = ip[:idx]
	}
	ip = strings.Trim(ip, "[]") // IPv6 brackets

	// RFC 1918 private ranges + localhost
	privateRanges := []string{
		"127.", "10.", "172.16.", "172.17.", "172.18.", "172.19.",
		"172.20.", "172.21.", "172.22.", "172.23.", "172.24.", "172.25.",
		"172.26.", "172.27.", "172.28.", "172.29.", "172.30.", "172.31.",
		"192.168.", "::1", "fe80:",
	}
	for _, prefix := range privateRanges {
		if strings.HasPrefix(ip, prefix) {
			return true
		}
	}
	return false
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
