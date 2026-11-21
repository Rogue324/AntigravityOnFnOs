package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/config"
)

const (
	CookieName      = "agy_session"
	sessionTTL      = 7 * 24 * time.Hour
	wsTicketTTL     = 30 * time.Second
	maxLoginFails   = 10
	rateLimitWindow = 5 * time.Minute
)

var (
	ErrAlreadyInitialized = errors.New("password already initialized")
	ErrNotInitialized     = errors.New("password not initialized")
	ErrInvalidPassword    = errors.New("invalid password")
	ErrWeakPassword       = errors.New("password must be at least 6 characters")
	ErrRateLimited        = errors.New("too many failed login attempts, please try again later")
)

type failBucket struct {
	count   int
	resetAt time.Time
}

// Manager handles password auth, session cookies/tokens, WS tickets, and login rate limiting.
type Manager struct {
	cfgStore *config.Store

	mu       sync.Mutex
	sessions map[string]time.Time
	tickets  map[string]time.Time
	fails    map[string]*failBucket
}

func NewManager(cfgStore *config.Store) *Manager {
	return &Manager{
		cfgStore: cfgStore,
		sessions: make(map[string]time.Time),
		tickets:  make(map[string]time.Time),
		fails:    make(map[string]*failBucket),
	}
}

func (m *Manager) IsInitialized() bool {
	return m.cfgStore.Get().PasswordHash != ""
}

func (m *Manager) IsAuthDisabled() bool {
	return m.cfgStore.Get().AuthDisabled
}

func (m *Manager) SkipInitialSetup() error {
	if m.IsInitialized() {
		return ErrAlreadyInitialized
	}
	return m.cfgStore.Update(func(c *config.Config) {
		c.AuthDisabled = true
	})
}

func (m *Manager) IssueSession() (string, error) {
	return m.createSession()
}

func (m *Manager) SetupPassword(password string) (string, error) {
	if len(password) < 6 {
		return "", ErrWeakPassword
	}
	if m.IsInitialized() {
		return "", ErrAlreadyInitialized
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	if err := m.cfgStore.Update(func(c *config.Config) {
		c.PasswordHash = string(hash)
		c.AuthDisabled = false
	}); err != nil {
		return "", err
	}
	return m.createSession()
}

func (m *Manager) Login(clientIP, password string) (string, error) {
	if !m.IsInitialized() {
		return "", ErrNotInitialized
	}
	if m.isRateLimited(clientIP) {
		return "", ErrRateLimited
	}

	hash := m.cfgStore.Get().PasswordHash
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		m.recordFailure(clientIP)
		return "", ErrInvalidPassword
	}

	m.clearFailure(clientIP)
	return m.createSession()
}

func (m *Manager) ChangePassword(currentPassword, newPassword string) error {
	if len(newPassword) < 6 {
		return ErrWeakPassword
	}
	hash := m.cfgStore.Get().PasswordHash
	if hash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)); err != nil {
			return ErrInvalidPassword
		}
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return m.cfgStore.Update(func(c *config.Config) {
		c.PasswordHash = string(newHash)
		c.AuthDisabled = false
	})
}

func (m *Manager) Logout(token string) {
	if token == "" {
		return
	}
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

func (m *Manager) ValidateRequest(r *http.Request) (string, bool) {
	if m.IsAuthDisabled() {
		return "no-auth", true
	}
	token := ExtractToken(r)
	if token == "" {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.sessions[token]
	if !ok {
		return "", false
	}
	if time.Now().After(exp) {
		delete(m.sessions, token)
		return "", false
	}
	// Sliding expiration
	m.sessions[token] = time.Now().Add(sessionTTL)
	return token, true
}

func (m *Manager) CreateWSTicket() (string, error) {
	ticket, err := randomHex(24)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.pruneLocked()
	m.tickets[ticket] = time.Now().Add(wsTicketTTL)
	m.mu.Unlock()
	return ticket, nil
}

func (m *Manager) ConsumeWSTicket(ticket string) bool {
	if ticket == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.tickets[ticket]
	if !ok {
		return false
	}
	delete(m.tickets, ticket)
	return time.Now().Before(exp)
}

func (m *Manager) SetSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func (m *Manager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func ExtractToken(r *http.Request) string {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return c.Value
	}
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	return ""
}

func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (m *Manager) createSession() (string, error) {
	token, err := randomHex(32)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.pruneLocked()
	m.sessions[token] = time.Now().Add(sessionTTL)
	m.mu.Unlock()
	return token, nil
}

func (m *Manager) isRateLimited(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.fails[ip]
	if !ok {
		return false
	}
	if time.Now().After(b.resetAt) {
		delete(m.fails, ip)
		return false
	}
	return b.count >= maxLoginFails
}

func (m *Manager) recordFailure(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	b, ok := m.fails[ip]
	if !ok || now.After(b.resetAt) {
		m.fails[ip] = &failBucket{count: 1, resetAt: now.Add(rateLimitWindow)}
		return
	}
	b.count++
}

func (m *Manager) clearFailure(ip string) {
	m.mu.Lock()
	delete(m.fails, ip)
	m.mu.Unlock()
}

func (m *Manager) pruneLocked() {
	now := time.Now()
	for k, exp := range m.sessions {
		if now.After(exp) {
			delete(m.sessions, k)
		}
	}
	for k, exp := range m.tickets {
		if now.After(exp) {
			delete(m.tickets, k)
		}
	}
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
