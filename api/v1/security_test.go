package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CCDD2022/seckill-system/api/middleware"
	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/pkg/utils"
	"github.com/CCDD2022/seckill-system/proto_output/order"
	"github.com/CCDD2022/seckill-system/proto_output/product"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
)

type recordingProductClient struct {
	product.ProductServiceClient
	createCalls int
}

func (c *recordingProductClient) CreateProduct(context.Context, *product.CreateProductRequest, ...grpc.CallOption) (*product.CreateProductResponse, error) {
	c.createCalls++
	return &product.CreateProductResponse{Code: e.SUCCESS, ProductId: 1}, nil
}

type recordingOrderClient struct {
	order.OrderServiceClient
	request *order.GetOrderRequest
}

func (c *recordingOrderClient) GetOrder(_ context.Context, req *order.GetOrderRequest, _ ...grpc.CallOption) (*order.GetOrderResponse, error) {
	c.request = req
	return &order.GetOrderResponse{Code: e.SUCCESS, Order: &order.Order{Id: req.OrderId, UserId: req.UserId}}, nil
}

func TestProductAdminPermissionUsesSignedRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		username   string
		isAdmin    bool
		wantStatus int
		wantCalls  int
	}{
		{name: "admin username without role", username: "admin", isAdmin: false, wantStatus: http.StatusForbidden},
		{name: "admin role with ordinary username", username: "operator", isAdmin: true, wantStatus: http.StatusCreated, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jwtUtil := utils.NewJWTUtil("test-secret", 1)
			token, err := jwtUtil.GenerateToken(42, tc.username, tc.isAdmin)
			if err != nil {
				t.Fatal(err)
			}
			client := &recordingProductClient{}
			router := gin.New()
			group := router.Group("/products")
			group.Use(middleware.JWTAuthMiddleware(jwtUtil))
			NewProductHandler(client).RegisterRoutes(group)

			req := httptest.NewRequest(http.MethodPost, "/products", strings.NewReader(`{"name":"item","price":1,"stock":1}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			if res.Code != tc.wantStatus || client.createCalls != tc.wantCalls {
				t.Fatalf("status=%d calls=%d, want status=%d calls=%d: %s", res.Code, client.createCalls, tc.wantStatus, tc.wantCalls, res.Body.String())
			}
		})
	}
}

func TestGetOrderForwardsAuthenticatedUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwtUtil := utils.NewJWTUtil("test-secret", 1)
	token, err := jwtUtil.GenerateToken(42, "buyer", false)
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingOrderClient{}
	router := gin.New()
	group := router.Group("/orders")
	group.Use(middleware.JWTAuthMiddleware(jwtUtil))
	NewOrderHandler(client).RegisterRoutes(group)

	req := httptest.NewRequest(http.MethodGet, "/orders/73", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK || client.request == nil || client.request.OrderId != 73 || client.request.UserId != 42 {
		t.Fatalf("status=%d request=%+v body=%s", res.Code, client.request, res.Body.String())
	}
}
