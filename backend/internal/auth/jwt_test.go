package auth

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestService(t *testing.T, ttl time.Duration, pwHash string) *Service {
	t.Helper()
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{
		Secret:         []byte(secret),
		Username:       "admin",
		PasswordBcrypt: pwHash,
		TokenTTL:       ttl,
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestIssueAndValidate(t *testing.T) {
	svc := newTestService(t, time.Hour, "")
	tok, exp, err := svc.Issue("admin")
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(exp) <= 0 {
		t.Fatal("expected future exp")
	}
	c, err := svc.Validate(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "admin" {
		t.Fatalf("sub=%q", c.Subject)
	}
}

func TestExpiredToken(t *testing.T) {
	svc := newTestService(t, time.Millisecond, "")
	tok, _, err := svc.Issue("admin")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := svc.Validate(tok); err == nil {
		t.Fatal("expected expiration error")
	}
}

func TestInvalidSignature(t *testing.T) {
	svc := newTestService(t, time.Hour, "")
	tok, _, _ := svc.Issue("admin")
	// tamper last char
	bad := tok[:len(tok)-1] + "X"
	if bad == tok {
		bad = tok[:len(tok)-1] + "Y"
	}
	if _, err := svc.Validate(bad); err == nil {
		t.Fatal("expected signature mismatch")
	}
}

func TestDevModeAcceptsAnyPassword(t *testing.T) {
	svc := newTestService(t, time.Hour, "")
	if err := svc.CheckCredentials("admin", "whatever"); err != nil {
		t.Fatalf("dev mode should accept: %v", err)
	}
	if err := svc.CheckCredentials("other", "x"); err == nil {
		t.Fatal("expected wrong username rejection")
	}
}

func TestBcryptModeRejectsBad(t *testing.T) {
	hash, err := HashPassword("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t, time.Hour, hash)
	if err := svc.CheckCredentials("admin", "correct-horse"); err != nil {
		t.Fatalf("should accept correct pw: %v", err)
	}
	if err := svc.CheckCredentials("admin", "nope"); err == nil {
		t.Fatal("should reject wrong pw")
	}
}

func TestRequireAuthMiddleware(t *testing.T) {
	svc := newTestService(t, time.Hour, "")
	tok, _, _ := svc.Issue("admin")

	h := svc.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := ClaimsFromContext(r.Context())
		if !ok || c.Subject != "admin" {
			t.Fatal("claims missing")
		}
		w.WriteHeader(http.StatusOK)
	}))

	// no token → 401
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
	if rr.Code != 401 {
		t.Fatalf("no token got %d", rr.Code)
	}

	// bearer header → 200
	rr = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("bearer got %d", rr.Code)
	}

	// query token → 200
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/x?token="+tok, nil)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("query got %d", rr.Code)
	}
}
