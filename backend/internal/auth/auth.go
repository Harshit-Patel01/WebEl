package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const jwtSecretKey = "jwt_secret"

// minPasswordLength and maxLoginAttempts bound credential guessing. bcrypt cost
// is tuned for a Pi, so a cheap limiter matters more here than on a server.
const (
	minPasswordLength = 8
	maxLoginAttempts  = 5
	loginLockout      = 2 * time.Minute
)

type Auth struct {
	db              *state.DB
	jwtSecret       []byte
	sessionDuration time.Duration
	bcryptCost      int
	lanOnly         bool
	logger          *zap.Logger

	// loginMu guards loginFails. bcrypt at Pi cost is slow enough that without
	// a limiter an unauthenticated client can pin the CPU with guesses.
	loginMu    sync.Mutex
	loginFails map[string]*loginAttempt
}

type loginAttempt struct {
	count       int
	lockedUntil time.Time
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
		loginFails:      make(map[string]*loginAttempt),
	}
}

// LoginAllowed reports whether another attempt from key is permitted right now.
func (a *Auth) LoginAllowed(key string) bool {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()

	att, ok := a.loginFails[key]
	if !ok {
		return true
	}
	if time.Now().After(att.lockedUntil) {
		delete(a.loginFails, key) // window elapsed, start clean
		return true
	}
	return false
}

// RecordLoginFailure counts a failed attempt and locks the key once the limit is hit.
func (a *Auth) RecordLoginFailure(key string) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()

	att, ok := a.loginFails[key]
	if !ok {
		att = &loginAttempt{}
		a.loginFails[key] = att
	}
	att.count++
	if att.count >= maxLoginAttempts {
		att.lockedUntil = time.Now().Add(loginLockout)
		att.count = 0
	}
}

// ClearLoginFailures resets the counter after a successful login.
func (a *Auth) ClearLoginFailures(key string) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	delete(a.loginFails, key)
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
	if len(password) < minPasswordLength {
		return fmt.Errorf("password must be at least %d characters", minPasswordLength)
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

// RotateJWTSecret replaces the signing key, invalidating every existing session.
// Called after a password change so a session captured earlier cannot outlive it.
func (a *Auth) RotateJWTSecret() error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("generating jwt secret: %w", err)
	}
	if err := a.db.SetSetupState(jwtSecretKey, hex.EncodeToString(raw)); err != nil {
		return fmt.Errorf("persisting jwt secret: %w", err)
	}
	a.jwtSecret = raw
	return nil
}

// Authorize reports whether a request carries a valid session. Shared by the
// HTTP middleware and the WebSocket upgrade, which browsers authenticate with
// the same cookie.
func (a *Auth) Authorize(r *http.Request) bool {
	if cookie, err := r.Cookie("opendeploy_session"); err == nil {
		return a.ValidateToken(cookie.Value)
	}
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}
	return a.ValidateToken(strings.TrimPrefix(authHeader, "Bearer "))
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

		if !a.Authorize(r) {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
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

// isLANRequest trusts the TCP peer address only. RemoteAddr is rewritten from
// X-Forwarded-For by chi's RealIP middleware, so any client can claim a private
// address and defeat this check — read the connection itself instead.
// ponytail: nginx/cloudflared in front of the app means the peer is always the
// proxy. Add a trusted-proxy header allowlist if this is ever exposed publicly.
func isLANRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	host = strings.Trim(host, "[]") // IPv6 brackets

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() {
		return true
	}
	// fe80::/10 link-local and the IPv4-mapped form of a private address.
	return ip.IsLinkLocalMulticast()
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
