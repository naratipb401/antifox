package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"antifox/db"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	jwtKeyFilename = "jwt.key"
	authPassKey    = "auth.password_hash" // legacy single-user password (only used to migrate to rbac_users on first boot)
	// defaultSessionTTL is the default session (JWT) lifetime and doubles as the
	// "idle timeout" window: active use slides the expiry forward (see requireAuth),
	// while idle past it expires the token and forces a re-login.
	// Overridable via the auth.session_ttl_minutes setting (in minutes).
	defaultSessionTTL = 60 * time.Minute
	sessionTTLKey     = "auth.session_ttl_minutes"
	keyChars          = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

type ctxKey int

const accessCtxKey ctxKey = 1

// authClaims carries the user id (Subject) + username + the password version at issue
// time (token_version). Ver makes "password change/disable kicks the session" work:
// at validation time it is compared against the current token_version in the DB.
type authClaims struct {
	Username string `json:"usr"`
	Ver      int    `json:"ver"`
	jwt.RegisteredClaims
}

// CurrentAccess returns the signed-in user's RBAC access profile from the request
// context (injected by requireAuth).
func CurrentAccess(r *http.Request) *db.RBACAccess {
	acc, _ := r.Context().Value(accessCtxKey).(*db.RBACAccess)
	return acc
}

// loadOrCreateJWTKey reads / generates the 32-byte HS256 signing key.
func loadOrCreateJWTKey(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, jwtKeyFilename)
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) >= 32 {
		return []byte(strings.TrimSpace(string(data))), nil
	}
	buf := make([]byte, 32)
	for i := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(keyChars))))
		if err != nil {
			return nil, fmt.Errorf("generate jwt key: %w", err)
		}
		buf[i] = keyChars[n.Int64()]
	}
	if err := os.WriteFile(path, buf, 0600); err != nil {
		return nil, fmt.Errorf("write jwt key: %w", err)
	}
	log.Printf("[auth] new JWT key written to %s", path)
	return buf, nil
}

// sessionTTL reads the session lifetime (minutes) setting; falls back to the default
// when unset or invalid.
func (s *Server) sessionTTL() time.Duration {
	if s.m != nil && s.m.pg != nil {
		if m := s.m.pg.GetInt(sessionTTLKey, 0); m > 0 {
			return time.Duration(m) * time.Minute
		}
	}
	return defaultSessionTTL
}

// signJWT issues an HS256 token with the default session TTL (tests use this).
func signJWT(key []byte, userID, username string) (string, error) {
	return signJWTTTL(key, userID, username, 0, defaultSessionTTL)
}

// signJWTTTL issues an HS256 token that expires after ttl, stamped with the user's
// current token_version (ver) so a later password change / disable invalidates it.
func signJWTTTL(key []byte, userID, username string, ver int, ttl time.Duration) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, authClaims{
		Username: username,
		Ver:      ver,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}).SignedString(key)
}

// parseJWT validates and returns the claims.
func parseJWT(tokenStr string, key []byte) (*authClaims, bool) {
	var c authClaims
	t, err := jwt.ParseWithClaims(tokenStr, &c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return key, nil
	})
	if err != nil || !t.Valid {
		return nil, false
	}
	return &c, true
}

// extractToken reads the JWT from Authorization: Bearer, antifox_token cookie, or ?token=.
func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("antifox_token"); err == nil && c.Value != "" {
		return c.Value
	}
	return r.URL.Query().Get("token")
}

// requireAuth validates the JWT and resolves/injects the user's permissions into the
// request context. /api/auth/* and /api/health are exempt.
func (s *Server) requireAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/auth/") || p == "/api/health" {
			h.ServeHTTP(w, r)
			return
		}
		claims, ok := parseJWT(extractToken(r), s.jwtKey)
		if !ok {
			writeErr(w, 401, "unauthorized or expired token")
			return
		}
		// Resolve the access profile (used for RBAC enforcement and administration);
		// reject when the account is disabled or the token version is stale.
		if s.m != nil && s.m.pg != nil && claims.Subject != "" {
			acc, err := s.m.pg.ResolveRBACAccess(claims.Subject)
			if err == nil && acc != nil {
				if !acc.User.Enabled {
					writeErr(w, 403, "account disabled")
					return
				}
				// Token version check: token_version is bumped on password change /
				// disable; a stale Ver carried by an old token is rejected, forcing
				// a re-login.
				if claims.Ver != acc.User.TokenVersion {
					writeErr(w, 401, "session expired, sign in again")
					return
				}
				// Sliding renewal (= idle timeout): once a token is past half its
				// lifetime and still in active use, a fresh token is returned via the
				// X-Refresh-Token response header for the frontend to swap in; idle
				// past the session TTL expires it and requires a re-login.
				if claims.ExpiresAt != nil {
					ttl := s.sessionTTL()
					if rem := time.Until(claims.ExpiresAt.Time); rem > 0 && rem < ttl/2 {
						if fresh, err := signJWTTTL(s.jwtKey, claims.Subject, claims.Username, acc.User.TokenVersion, ttl); err == nil {
							w.Header().Set("Access-Control-Expose-Headers", "X-Refresh-Token")
							w.Header().Set("X-Refresh-Token", fresh)
						}
					}
				}
				r = r.WithContext(context.WithValue(r.Context(), accessCtxKey, acc))
				// Permission enforcement. Safety valve: unmapped routes pass through;
				// admin (full permissions) is never blocked; the rbac.enforce switch
				// defaults to true.
				if s.m.pg.GetBool("rbac.enforce", true) {
					if perm := permissionForRequest(r.Method, r.URL.Path); perm != "" && !acc.Permissions[perm] {
						writeErr(w, 403, "insufficient permission: "+perm)
						return
					}
				}
			}
		}
		h.ServeHTTP(w, r)
	})
}

// bootstrapRBAC on first boot migrates the legacy single-user password into the
// built-in admin and seeds roles/permissions (idempotent).
func (s *Server) bootstrapRBAC() {
	if s.m == nil || s.m.pg == nil {
		return
	}
	legacyHash, _, _ := s.m.pg.GetSetting(authPassKey)
	if err := s.m.pg.BootstrapRBAC(legacyHash); err != nil {
		log.Printf("[rbac] bootstrap failed: %v", err)
		return
	}
	if strings.TrimSpace(legacyHash) != "" {
		log.Printf("[rbac] multi-user access ready (legacy password migrated to built-in admin)")
	} else {
		log.Printf("[rbac] multi-user access ready")
	}
}

// GET /api/auth/status - whether the platform is initialized (a platform user exists).
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	needs, err := pg.RBACNeedsBootstrap()
	if err != nil {
		writeErr(w, 500, "query failed")
		return
	}
	writeJSON(w, 200, map[string]any{"initialized": !needs})
}

// POST /api/auth/init - sets the admin password on first boot (creates the built-in admin).
func (s *Server) authInit(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	ip := clientIP(r)
	// fail-closed: when the init-guard query errors, always reject -- never treat a
	// query failure as "not initialized" and allow setting the password (otherwise a
	// query failure could be abused to reset the admin password).
	needs, err := pg.RBACNeedsBootstrap()
	if err != nil {
		pg.RecordAudit("", "auth", "init", "fail", ip, "init guard query failed (rejected)", nil)
		writeErr(w, 503, "temporarily unavailable")
		return
	}
	if !needs {
		pg.RecordAudit("", "auth", "init", "fail", ip, "platform already initialized, repeat setup rejected", nil)
		writeErr(w, 403, "already initialized")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if msg := validatePasswordStrength(req.Password); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "password hashing failed")
		return
	}
	if err := pg.BootstrapRBAC(string(hash)); err != nil {
		writeErr(w, 500, "init failed: "+err.Error())
		return
	}
	_ = pg.SetSetting(authPassKey, string(hash)) // kept for compatibility
	ver := 0
	if u, e := pg.GetRBACUserByID("admin"); e == nil && u != nil {
		ver = u.TokenVersion
	}
	tok, err := signJWTTTL(s.jwtKey, "admin", "admin", ver, s.sessionTTL())
	if err != nil {
		writeErr(w, 500, "token generation failed")
		return
	}
	pg.RecordAudit("admin", "auth", "init", "success", ip, "first-boot admin password init", nil)
	writeJSON(w, 200, map[string]any{"token": tok})
}

// POST /api/auth/change-password - changes the signed-in user's password.
func (s *Server) authChangePassword(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	claims, ok := parseJWT(extractToken(r), s.jwtKey)
	if !ok {
		writeErr(w, 401, "unauthorized")
		return
	}
	u, err := pg.GetRBACUserByID(claims.Subject)
	if err != nil {
		writeErr(w, 401, "user not found")
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if msg := validatePasswordStrength(req.NewPassword); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	ip := clientIP(r)
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.OldPassword)); err != nil {
		pg.RecordAudit(u.Username, "auth", "change_password", "fail", ip, "current password incorrect", nil)
		writeErr(w, 401, "current password incorrect")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "password hashing failed")
		return
	}
	// UpdateRBACUserPassword bumps token_version, invalidating all older tokens including this request's.
	if err := pg.UpdateRBACUserPassword(u.ID, string(newHash)); err != nil {
		writeErr(w, 500, "save failed: "+err.Error())
		return
	}
	pg.RecordAudit(u.Username, "auth", "change_password", "success", ip, "password changed", nil)
	// Re-issue a token with the new version so the caller isn't logged out right after changing
	// their password; older tokens on other devices remain invalidated.
	fresh := ""
	if nu, e := pg.GetRBACUserByID(u.ID); e == nil && nu != nil {
		fresh, _ = signJWTTTL(s.jwtKey, nu.ID, nu.Username, nu.TokenVersion, s.sessionTTL())
	}
	writeJSON(w, 200, map[string]any{"ok": true, "token": fresh})
}

// POST /api/auth/login — validate credentials and return a JWT.
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	ip := clientIP(r)
	uname := strings.TrimSpace(req.Username)
	// Brute-force protection: check lockout on both the IP and account dimensions. A lockout
	// returns 429 immediately, skipping the password comparison (bcrypt is CPU-heavy; blocking
	// early also keeps a flood of attempts from saturating CPU/connections).
	if locked, retry := s.loginLimiter.locked(ip, uname); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
		pg.RecordAudit(uname, "auth", "login_locked", "fail", ip,
			fmt.Sprintf("brute-force lockout triggered, cooldown %ds left", int(retry.Seconds())), nil)
		writeErr(w, 429, fmt.Sprintf("too many attempts, account/source temporarily locked, retry in %ds", int(retry.Seconds())))
		return
	}
	u, err := pg.GetRBACUserByUsername(req.Username)
	if err != nil || u == nil || !u.Enabled {
		reason := "invalid username or password"
		if u != nil && !u.Enabled {
			reason = "account disabled"
		}
		s.registerLoginFailure(pg, ip, uname)
		pg.RecordAudit(uname, "auth", "login_failed", "fail", ip, reason, nil)
		log.Printf("[auth] login failed user=%q ip=%s reason=%s", uname, ip, reason)
		writeErr(w, 401, reason)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		s.registerLoginFailure(pg, ip, u.Username)
		pg.RecordAudit(u.Username, "auth", "login_failed", "fail", ip, "wrong password", nil)
		log.Printf("[auth] login failed user=%q ip=%s reason=wrong password", u.Username, ip)
		writeErr(w, 401, "invalid username or password")
		return
	}
	tok, err := signJWTTTL(s.jwtKey, u.ID, u.Username, u.TokenVersion, s.sessionTTL())
	if err != nil {
		writeErr(w, 500, "token generation failed")
		return
	}
	s.loginLimiter.reset(ip, u.Username) // a successful login clears the failure counts for this IP+account
	pg.RecordAudit(u.Username, "auth", "login", "success", ip, "login succeeded", nil)
	log.Printf("[auth] login succeeded user=%q ip=%s", u.Username, ip)
	writeJSON(w, 200, map[string]any{"token": tok})
}

// registerLoginFailure records one failure into the limiter; if it exactly triggers a
// lockout, an audit entry is written so the event shows up on the log page.
func (s *Server) registerLoginFailure(pg *db.DB, ip, username string) {
	if s.loginLimiter.fail(ip, username, s.loginPolicy()) {
		pg.RecordAudit(username, "auth", "login_locked", "fail", ip, "failure threshold exceeded, temporarily locked", nil)
		log.Printf("[auth] brute-force lockout user=%q ip=%s", username, ip)
	}
}

// validatePasswordStrength returns "" on pass, otherwise the reason shown to the user.
// Rules: length 8-128, at least two character classes (letters / digits / symbols), and
// not a single repeated character.
func validatePasswordStrength(pw string) string {
	if len(pw) < 8 {
		return "password must be at least 8 characters"
	}
	if len(pw) > 128 {
		return "password too long (max 128 characters)"
	}
	var hasLetter, hasDigit, hasOther bool
	allSame := true
	for i, r := range pw {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			hasOther = true
		}
		if i > 0 && rune(pw[i]) != rune(pw[0]) {
			allSame = false
		}
	}
	if allSame {
		return "password must not be a single repeated character"
	}
	classes := 0
	for _, b := range []bool{hasLetter, hasDigit, hasOther} {
		if b {
			classes++
		}
	}
	if classes < 2 {
		return "password needs at least two character classes (letters / digits / symbols)"
	}
	return ""
}

// clientIP extracts the client IP (X-Forwarded-For / X-Real-IP first, falling back to RemoteAddr).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return strings.TrimSpace(xr)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}
