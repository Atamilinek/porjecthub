package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/projecthub/projecthub/internal/auth"
	"github.com/projecthub/projecthub/internal/ids"
	"github.com/projecthub/projecthub/internal/rbac"
)

const sessionCookieName = "projecthub_session"

type requestIDKey struct{}
type principalKey struct{}
type authHTTP struct {
	service       *auth.Service
	limiter       *auth.LoginLimiter
	sourceLimiter *auth.LoginLimiter
	db            *sql.DB
}

func (a *authHTTP) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/login", a.login)
	mux.Handle("GET /api/v1/session", a.require(rbac.PermissionRead, http.HandlerFunc(a.session)))
	mux.Handle("POST /api/v1/logout", a.require(rbac.PermissionRead, http.HandlerFunc(a.logout)))
}
func (a *authHTTP) login(w http.ResponseWriter, r *http.Request) {
	if !validOrigin(r) {
		http.Error(w, "origin rejected", http.StatusForbidden)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decodeJSON(w, r, &input) != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	key := loginKey(r, input.Username)
	sourceKey := sourceLoginKey(r)
	if !a.limiter.Allow(key) || !a.sourceLimiter.Allow(sourceKey) {
		http.Error(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	user, err := a.service.Authenticate(r.Context(), input.Username, input.Password)
	if err != nil {
		a.limiter.Failure(key)
		a.sourceLimiter.Failure(sourceKey)
		_ = a.audit(r, "", "login", "rejected")
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	a.limiter.Success(key)
	old, _ := r.Cookie(sessionCookieName)
	previous := ""
	if old != nil {
		previous = old.Value
	}
	session, err := a.service.NewSession(r.Context(), user.ID, previous)
	if err != nil {
		http.Error(w, "session unavailable", http.StatusInternalServerError)
		return
	}
	if err := a.auditTarget(r, user.ID, "user", user.ID, "login", "success"); err != nil {
		_ = a.service.RevokeSession(r.Context(), session.Token)
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	exp := time.Unix(session.ExpiresAt, 0)
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: session.Token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, Expires: exp, MaxAge: int(time.Until(exp).Seconds())})
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "csrfToken": session.CSRFToken, "expiresAt": session.ExpiresAt})
}
func (a *authHTTP) session(w http.ResponseWriter, r *http.Request) {
	p := principalFromRequest(r)
	writeJSON(w, http.StatusOK, map[string]any{"user": p.User, "csrfToken": p.CSRFToken})
}
func (a *authHTTP) logout(w http.ResponseWriter, r *http.Request) {
	p := principalFromRequest(r)
	c, _ := r.Cookie(sessionCookieName)
	if c != nil {
		if err := a.service.RevokeSession(r.Context(), c.Value); err != nil {
			http.Error(w, "logout failed", http.StatusInternalServerError)
			return
		}
	}
	if err := a.auditTarget(r, p.User.ID, "session", p.SessionID, "logout", "success"); err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *authHTTP) audit(r *http.Request, actor, action, outcome string) error {
	return a.auditTarget(r, actor, "", "", action, outcome)
}

func (a *authHTTP) auditTarget(r *http.Request, actor, targetType, targetID, action, outcome string) error {
	request, _ := r.Context().Value(requestIDKey{}).(string)
	eventID, err := ids.NewUUID()
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(r.Context(), "INSERT INTO audit_logs(id,actor_id,request_id,event_type,target_type,target_id,action,outcome,detail,created_at) VALUES(?,?,?,'security',?,?,?,?, '',?)", eventID, nullable(actor), request, targetType, targetID, action, outcome, time.Now().Unix())
	return err
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func (a *authHTTP) require(permission rbac.Permission, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		p, err := a.service.ResolveSession(r.Context(), c.Value)
		if err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if !rbac.Allows(p.User.Role, permission) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if !validOrigin(r) {
				http.Error(w, "origin rejected", http.StatusForbidden)
				return
			}
			if !constantEqual(r.Header.Get("X-CSRF-Token"), p.CSRFToken) {
				http.Error(w, "csrf validation failed", http.StatusForbidden)
				return
			}
		}
		ctx := context.WithValue(r.Context(), principalKey{}, p)
		ctx = rbac.WithPrincipal(ctx, rbac.Principal{UserID: p.User.ID, Role: p.User.Role})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func principalFromRequest(r *http.Request) auth.Principal {
	p, _ := r.Context().Value(principalKey{}).(auth.Principal)
	return p
}
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if len(id) > 64 || id == "" || strings.ContainsAny(id, "\r\n\x00") {
			raw := make([]byte, 16)
			_, _ = rand.Read(raw)
			id = hex.EncodeToString(raw)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}
func validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	proto := "https"
	if r.TLS == nil {
		if r.Header.Get("X-Forwarded-Proto") == "https" {
			proto = "https"
		} else if isLoopbackHost(r.Host) {
			proto = "http"
		} else {
			return false
		}
	}
	return strings.EqualFold(u.Scheme, proto) && strings.EqualFold(u.Host, r.Host)
}
func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func loginKey(r *http.Request, username string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return strings.ToLower(strings.TrimSpace(username)) + "\x00" + host
}

func sourceLoginKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return "source\x00" + host
}
func constantEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
