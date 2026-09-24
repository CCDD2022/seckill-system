package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGatewayFallbackProblems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	configureErrorResponses(r)
	r.GET("/known", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/panic", func(c *gin.Context) { panic("private database password") })
	for _, tc := range []struct {
		method, path string
		status       int
		code         string
	}{
		{http.MethodGet, "/missing", http.StatusNotFound, "route_not_found"},
		{http.MethodPost, "/known", http.StatusMethodNotAllowed, "method_not_allowed"},
		{http.MethodGet, "/panic", http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || w.Header().Get("Content-Type") != "application/problem+json" || body["type"] != "about:blank" || body["title"] != http.StatusText(tc.status) || body["status"] != float64(tc.status) || body["code"] != tc.code || body["instance"] != tc.path || body["detail"] == "private database password" {
				t.Fatalf("status=%d header=%v body=%v", w.Code, w.Header(), body)
			}
		})
	}
}
