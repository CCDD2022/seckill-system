package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/auth"
	"github.com/CCDD2022/seckill-system/proto_output/order"
	"github.com/CCDD2022/seckill-system/proto_output/product"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type errorAuthClient struct {
	auth.AuthServiceClient
	registerCode int32
	loginCode    int32
	err          error
}

func (m *errorAuthClient) Register(context.Context, *auth.RegisterRequest, ...grpc.CallOption) (*auth.RegisterResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &auth.RegisterResponse{Code: m.registerCode, Message: "private database detail"}, nil
}
func (m *errorAuthClient) Login(context.Context, *auth.LoginRequest, ...grpc.CallOption) (*auth.LoginResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &auth.LoginResponse{Code: m.loginCode, Message: "private account detail"}, nil
}

type errorOrderClient struct {
	order.OrderServiceClient
	code int32
	err  error
}

func (m *errorOrderClient) CancelOrder(context.Context, *order.CancelOrderRequest, ...grpc.CallOption) (*order.CancelOrderResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &order.CancelOrderResponse{Code: m.code, Message: "private order detail"}, nil
}

func requestProblem(t *testing.T, handler gin.HandlerFunc, method, path, body string, userID bool) (int, map[string]any, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, path, func(c *gin.Context) {
		if userID {
			c.Set("user_id", int64(7))
		}
		handler(c)
	})
	req := httptest.NewRequest(method, strings.Replace(path, ":id", "73", 1), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var value map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return w.Code, value, w.Header().Get("Content-Type")
}

func assertProblem(t *testing.T, gotStatus int, body map[string]any, contentType string, wantStatus int, wantCode, path string) {
	t.Helper()
	if gotStatus != wantStatus || contentType != "application/problem+json" || body["type"] != "about:blank" || body["title"] != http.StatusText(wantStatus) || body["status"] != float64(wantStatus) || body["code"] != wantCode || body["instance"] != path || body["detail"] == "" {
		t.Fatalf("status=%d content-type=%q body=%v", gotStatus, contentType, body)
	}
}

func TestAuthProblemMappings(t *testing.T) {
	cases := []struct {
		name       string
		handler    gin.HandlerFunc
		path, body string
		status     int
		code       string
	}{
		{"malformed JSON", NewAuthHandler(&errorAuthClient{}).Register, "/register", "{", 400, "invalid_json"},
		{"missing fields", NewAuthHandler(&errorAuthClient{}).Register, "/register", `{}`, 422, "validation_failed"},
		{"duplicate", NewAuthHandler(&errorAuthClient{registerCode: e.ERROR_USER_EXISTS}).Register, "/register", `{"username":"alice","password":"secret"}`, 409, "user_exists"},
		{"unknown login", NewAuthHandler(&errorAuthClient{loginCode: e.ERROR_USER_NOT_EXISTS}).Login, "/login", `{"username":"alice","password":"wrong"}`, 401, "invalid_credentials"},
		{"wrong password", NewAuthHandler(&errorAuthClient{loginCode: e.ERROR_PASSWORD}).Login, "/login", `{"username":"alice","password":"wrong"}`, 401, "invalid_credentials"},
		{"backend down", NewAuthHandler(&errorAuthClient{err: status.Error(codes.Unavailable, "private host")}).Login, "/login", `{"username":"alice","password":"wrong"}`, 503, "dependency_unavailable"},
		{"backend timeout", NewAuthHandler(&errorAuthClient{err: status.Error(codes.DeadlineExceeded, "private host")}).Login, "/login", `{"username":"alice","password":"wrong"}`, 504, "dependency_timeout"},
		{"unknown backend", NewAuthHandler(&errorAuthClient{err: errors.New("private SQL password")}).Login, "/login", `{"username":"alice","password":"wrong"}`, 500, "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body, ct := requestProblem(t, tc.handler, http.MethodPost, tc.path, tc.body, false)
			assertProblem(t, status, body, ct, tc.status, tc.code, tc.path)
			if strings.Contains(body["detail"].(string), "private") {
				t.Fatalf("internal detail leaked: %v", body)
			}
		})
	}
}

func TestOrderProblemMappings(t *testing.T) {
	cases := []struct {
		name    string
		code    int32
		status  int
		appCode string
	}{
		{"foreign order", e.ERROR, 404, "order_not_found"},
		{"missing order", e.ERROR_NOT_EXIST, 404, "order_not_found"},
		{"status changed", e.ERROR_ORDER_STATUS_CHANGED, 409, "order_status_conflict"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewOrderHandler(&errorOrderClient{code: tc.code})
			status, body, ct := requestProblem(t, h.CancelOrder, http.MethodPost, "/orders/:id/cancel", "", true)
			assertProblem(t, status, body, ct, tc.status, tc.appCode, "/orders/73/cancel")
			if strings.Contains(body["detail"].(string), "private") {
				t.Fatalf("internal detail leaked: %v", body)
			}
		})
	}
}

type invalidProductClient struct{ product.ProductServiceClient }

func (m *invalidProductClient) CreateProduct(context.Context, *product.CreateProductRequest, ...grpc.CallOption) (*product.CreateProductResponse, error) {
	return &product.CreateProductResponse{Code: e.INVALID_PARAMS, Message: "private product detail"}, nil
}

func TestProductValidationProblem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/products", func(c *gin.Context) {
		c.Set("user_id", int64(7))
		c.Set("is_admin", true)
		NewProductHandler(&invalidProductClient{}).CreateProduct(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/products", strings.NewReader(`{"name":"item","price":-1}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	assertProblem(t, w.Code, body, w.Header().Get("Content-Type"), 422, "validation_failed", "/products")
	if strings.Contains(body["detail"].(string), "private") {
		t.Fatalf("private detail leaked: %v", body)
	}
}

func TestWriteProblemExtensionsCannotReplaceStandardFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/reservation", func(c *gin.Context) {
		WriteProblem(c, http.StatusConflict, "duplicate_request", "该请求已受理", gin.H{
			"request_id": "req-123", "status_url": "/api/v1/seckill/requests/req-123", "status": 200,
		})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/reservation", nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	assertProblem(t, w.Code, body, w.Header().Get("Content-Type"), http.StatusConflict, "duplicate_request", "/reservation")
	if body["request_id"] != "req-123" || body["status_url"] != "/api/v1/seckill/requests/req-123" {
		t.Fatalf("extensions missing: %v", body)
	}
}
