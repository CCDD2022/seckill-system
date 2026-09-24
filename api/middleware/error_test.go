package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CCDD2022/seckill-system/pkg/utils"
	"github.com/gin-gonic/gin"
)

func checkProblem(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != wantStatus || w.Header().Get("Content-Type") != "application/problem+json" || body["type"] != "about:blank" || body["status"] != float64(wantStatus) || body["title"] != http.StatusText(wantStatus) || body["instance"] != "/private" || body["code"] != wantCode || body["detail"] == "" {
		t.Fatalf("status=%d headers=%v body=%v", w.Code, w.Header(), body)
	}
}

func TestJWTAuthenticationProblem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, token, code string }{
		{"missing", "", "authentication_required"},
		{"invalid", "Bearer invalid.token", "invalid_token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/private", JWTAuthMiddleware(utils.NewJWTUtil("test-secret", 1)), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			req := httptest.NewRequest(http.MethodGet, "/private", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", tc.token)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			checkProblem(t, w, http.StatusUnauthorized, tc.code)
			if w.Header().Get("WWW-Authenticate") != `Bearer realm="api"` {
				t.Fatalf("WWW-Authenticate=%q", w.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestRateLimitProblemIncludesRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/private", RateLimitMiddleware(0, 0), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/private", nil))
	checkProblem(t, w, http.StatusTooManyRequests, "rate_limited")
	if w.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After=%q", w.Header().Get("Retry-After"))
	}
}
