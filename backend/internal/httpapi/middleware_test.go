package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCorsMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	tests := []struct {
		name       string
		allowed    []string
		origin     string
		method     string
		wantHeader string // 期望的 Access-Control-Allow-Origin 值，空串表示不该有此头
		wantStatus int
	}{
		{"empty-allowed-sameorigin", nil, "", http.MethodGet, "", 200},
		{"empty-allowed-crossorigin", nil, "http://evil.com", http.MethodGet, "", 200},
		{"exact-match", []string{"http://foo.com"}, "http://foo.com", http.MethodGet, "http://foo.com", 200},
		{"exact-mismatch", []string{"http://foo.com"}, "http://bar.com", http.MethodGet, "", 200},
		{"glob-localhost", []string{"http://localhost:*"}, "http://localhost:3000", http.MethodGet, "http://localhost:3000", 200},
		{"glob-localhost-mismatch", []string{"http://localhost:*"}, "http://example.com", http.MethodGet, "", 200},
		{"wildcard-all", []string{"*"}, "https://anywhere.io", http.MethodGet, "https://anywhere.io", 200},
		{"preflight-matched", []string{"http://foo.com"}, "http://foo.com", http.MethodOptions, "http://foo.com", 204},
		{"preflight-unmatched", []string{"http://foo.com"}, "http://bar.com", http.MethodOptions, "", 204},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mw := corsMiddleware(tc.allowed)
			h := mw(okHandler)

			req := httptest.NewRequest(tc.method, "http://backend/api/x", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
			got := rec.Header().Get("Access-Control-Allow-Origin")
			if got != tc.wantHeader {
				t.Errorf("Access-Control-Allow-Origin: got %q, want %q", got, tc.wantHeader)
			}
			if tc.wantHeader != "" {
				if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
					t.Errorf("missing Allow-Credentials header")
				}
				if rec.Header().Get("Vary") != "Origin" {
					t.Errorf("missing Vary: Origin header")
				}
			}
		})
	}
}
