package v1

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/product"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

type ProductHandler struct{ client product.ProductServiceClient }

func NewProductHandler(client product.ProductServiceClient) *ProductHandler {
	return &ProductHandler{client: client}
}

func requireAdmin(c *gin.Context) bool {
	if _, ok := currentUserID(c); !ok {
		return false
	}
	if c.GetBool("is_admin") {
		return true
	}
	WriteProblem(c, http.StatusForbidden, "admin_required", "只有管理员可以执行此操作")
	return false
}

func productID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		WriteProblem(c, http.StatusBadRequest, "invalid_path_parameter", "商品 ID 格式不正确")
		return 0, false
	}
	if id <= 0 {
		writeInvalidInput(c, "商品 ID 必须大于 0")
		return 0, false
	}
	return id, true
}

func writeProductResultProblem(c *gin.Context, code int32) {
	switch code {
	case e.ERROR_PRODUCT_NOT_EXISTS:
		WriteProblem(c, http.StatusNotFound, "product_not_found", "商品不存在")
	case e.INVALID_PARAMS:
		writeInvalidInput(c, "商品参数无效或活动配置不可修改")
	default:
		WriteProblem(c, http.StatusInternalServerError, "internal_error", "服务器暂时无法处理请求")
	}
}

func (h *ProductHandler) GetProduct(c *gin.Context) {
	id, ok := productID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.client.GetProduct(ctx, &product.GetProductRequest{ProductId: id})
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		writeProductResultProblem(c, resp.GetCode())
		return
	}
	JSONProto(c, http.StatusOK, &product.GetProductResponse{Code: resp.GetCode(), Message: resp.GetMessage(), Product: resp.GetProduct()})
}

func (h *ProductHandler) ListProducts(c *gin.Context) {
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	filter, filterErr := strconv.Atoi(c.DefaultQuery("status", "-1"))
	if pageErr != nil || pageSizeErr != nil || filterErr != nil {
		WriteProblem(c, http.StatusBadRequest, "invalid_query_parameter", "分页或状态参数格式不正确")
		return
	}
	if page < 1 || page > math.MaxInt32 || pageSize < 1 || pageSize > 100 || (page-1) > math.MaxInt32/pageSize || filter < -1 || filter > 2 {
		writeInvalidInput(c, "分页或状态参数超出允许范围")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.client.ListProducts(ctx, &product.ListProductsRequest{Page: int32(page), PageSize: int32(pageSize), Status: int32(filter)})
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		writeProductResultProblem(c, resp.GetCode())
		return
	}
	JSONProto(c, http.StatusOK, &product.ListProductsResponse{Code: resp.GetCode(), Message: resp.GetMessage(), Products: resp.GetProducts(), Total: resp.GetTotal()})
}

func (h *ProductHandler) CreateProduct(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var req product.CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInvalidJSON(c)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.client.CreateProduct(ctx, &req)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			for _, detail := range st.Details() {
				info, valid := detail.(*errdetails.ErrorInfo)
				if !valid || info.Domain != "seckill-system" || info.Reason != "PRODUCT_CREATE_OUTCOME_UNKNOWN" {
					continue
				}
				productID := info.Metadata["product_id"]
				if id, parseErr := strconv.ParseInt(productID, 10, 64); parseErr == nil && id > 0 {
					statusURL := "/api/v1/products/" + productID
					c.Header("Location", statusURL)
					c.Header("Retry-After", "1")
					WriteProblem(c, http.StatusServiceUnavailable, "outcome_unknown", "商品创建结果尚未确认，请稍后查询商品", gin.H{
						"product_id": id, "status_url": statusURL,
					})
					return
				}
			}
		}
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		writeProductResultProblem(c, resp.GetCode())
		return
	}
	JSONProto(c, http.StatusCreated, &product.CreateProductResponse{Code: resp.GetCode(), Message: resp.GetMessage(), ProductId: resp.GetProductId()})
}

func (h *ProductHandler) UpdateProduct(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	id, ok := productID(c)
	if !ok {
		return
	}
	var req product.UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInvalidJSON(c)
		return
	}
	req.ProductId = id
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.client.UpdateProduct(ctx, &req)
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		writeProductResultProblem(c, resp.GetCode())
		return
	}
	JSONProto(c, http.StatusOK, &product.UpdateProductResponse{Code: resp.GetCode(), Message: resp.GetMessage()})
}

func (h *ProductHandler) DeleteProduct(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	id, ok := productID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.client.DeleteProduct(ctx, &product.DeleteProductRequest{ProductId: id})
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		writeProductResultProblem(c, resp.GetCode())
		return
	}
	JSONProto(c, http.StatusOK, &product.DeleteProductResponse{Code: resp.GetCode(), Message: resp.GetMessage()})
}

func (h *ProductHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/:id", h.GetProduct)
	rg.GET("", h.ListProducts)
	rg.POST("", h.CreateProduct)
	rg.PUT("/:id", h.UpdateProduct)
	rg.DELETE("/:id", h.DeleteProduct)
}
