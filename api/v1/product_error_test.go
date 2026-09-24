package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CCDD2022/seckill-system/proto_output/product"
	"github.com/gin-gonic/gin"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type uncertainCreateClient struct {
	product.ProductServiceClient
	err error
}

func (c *uncertainCreateClient) CreateProduct(_ context.Context, _ *product.CreateProductRequest, _ ...grpc.CallOption) (*product.CreateProductResponse, error) {
	return nil, c.err
}

func TestUncertainProductCreationHasLookupLocation(t *testing.T) {
	base := status.New(codes.Unavailable, "do not expose database details")
	st, err := base.WithDetails(&errdetails.ErrorInfo{
		Reason: "PRODUCT_CREATE_OUTCOME_UNKNOWN", Domain: "seckill-system",
		Metadata: map[string]string{"product_id": "123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user_id", int64(42)); c.Set("is_admin", true); c.Next() })
	NewProductHandler(&uncertainCreateClient{err: st.Err()}).RegisterRoutes(router.Group("/api/v1/products"))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/products", strings.NewReader(`{"name":"demo","price":9.99,"stock":1}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 503 || response.Header().Get("Location") != "/api/v1/products/123" {
		t.Fatalf("status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "outcome_unknown" || body["product_id"] != float64(123) || strings.Contains(response.Body.String(), "database") {
		t.Fatalf("unexpected problem: %v", body)
	}
}
