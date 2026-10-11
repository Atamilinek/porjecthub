package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/projecthub/projecthub/internal/auth"
)

func TestLoginSessionLogoutAndCookieProtections(t *testing.T) {
	db := testDatabase(t)
	service := auth.NewService(db)
	user, err := service.BootstrapAdmin(t.Context(), "admin-user", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(db)
	login := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(`{"username":"admin-user","password":"correct-horse-battery-staple"}`))
	login.Host = "localhost:8080"
	login.Header.Set("Origin", "http://localhost:8080")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, login)
	if response.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.CSRF == "" {
		t.Fatalf("login response CSRF missing: %s", response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie flags unsafe: %#v", cookies)
	}
	cookie := cookies[0]
	var loginDetail string
	if err := db.QueryRow("SELECT detail FROM audit_logs WHERE actor_id=? AND action='login'", user.ID).Scan(&loginDetail); err != nil {
		t.Fatalf("login audit missing: %v", err)
	}
	if strings.Contains(loginDetail, "correct-horse") {
		t.Fatal("audit detail contains the password")
	}
	get := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	get.Host = "localhost:8080"
	get.AddCookie(cookie)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, get)
	if rr.Code != http.StatusOK {
		t.Fatalf("session status=%d body=%s", rr.Code, rr.Body.String())
	}
	logout := httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
	logout.Host = "localhost:8080"
	logout.Header.Set("Origin", "http://localhost:8080")
	logout.Header.Set("X-CSRF-Token", body.CSRF)
	logout.AddCookie(cookie)
	logout.Header.Set("X-Request-ID", "req-logout-1")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, logout)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := service.ResolveSession(t.Context(), cookie.Value); err == nil {
		t.Fatal("session still valid after logout")
	}
	var requestID, action string
	if err := db.QueryRow("SELECT request_id,action FROM audit_logs WHERE actor_id=? AND action='logout'", user.ID).Scan(&requestID, &action); err != nil {
		t.Fatalf("logout audit missing: %v", err)
	}
	if requestID != "req-logout-1" || action != "logout" {
		t.Fatalf("audit request/action=%q/%q", requestID, action)
	}
}

func TestLoginRejectsBadOriginAndMalformedJSON(t *testing.T) {
	handler := newHandler(testDatabase(t))
	for _, tc := range []struct {
		name, origin, body string
		want               int
	}{{"bad origin", "https://attacker.example", `{"username":"admin","password":"long-password"}`, http.StatusForbidden}, {"malformed json", "http://localhost:8080", `{"username":`, http.StatusBadRequest}, {"unknown field", "http://localhost:8080", `{"username":"admin","password":"long-password","extra":true}`, http.StatusBadRequest}} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/login", bytes.NewBufferString(tc.body))
			r.Host = "localhost:8080"
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestSessionAndLogoutRequireAuthenticationAndCSRF(t *testing.T) {
	db := testDatabase(t)
	service := auth.NewService(db)
	user, err := service.BootstrapAdmin(t.Context(), "admin-user", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.NewSession(t.Context(), user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(db)
	cookie := &http.Cookie{Name: sessionCookieName, Value: session.Token}
	cases := []struct {
		name, origin, csrf string
		want               int
	}{{"anonymous", "", "", http.StatusUnauthorized}, {"missing csrf", "http://localhost:8080", "", http.StatusForbidden}, {"bad origin", "https://attacker.example", session.CSRFToken, http.StatusForbidden}, {"valid csrf and origin", "http://localhost:8080", session.CSRFToken, http.StatusNoContent}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
			r.Host = "localhost:8080"
			if tc.name != "anonymous" {
				r.AddCookie(cookie)
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.csrf != "" {
				r.Header.Set("X-CSRF-Token", tc.csrf)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
		})
	}
}
