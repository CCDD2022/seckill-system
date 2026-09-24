package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CCDD2022/seckill-system/proto_output/order"
	"github.com/CCDD2022/seckill-system/proto_output/product"
	"github.com/gin-gonic/gin"
)

// Nil clients have no List method implementation. If validation lets the
// request through, the handler panics instead of returning the expected 422.
func TestPaginationOffsetOverflowIsRejectedBeforeRPC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, path string
		handler    gin.HandlerFunc
	}{
		{"products", "/products", NewProductHandler(product.ProductServiceClient(nil)).ListProducts},
		{"orders", "/orders/my", NewOrderHandler(order.OrderServiceClient(nil)).ListOrders},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET(tc.path, func(c *gin.Context) {
				c.Set("user_id", int64(7))
				tc.handler(c)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path+"?page=2147483647&page_size=100", nil))
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			assertProblem(t, w.Code, body, w.Header().Get("Content-Type"), http.StatusUnprocessableEntity, "validation_failed", tc.path)
		})
	}
}
