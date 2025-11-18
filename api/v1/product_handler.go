package v1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/status"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/product"
)

type ProductHandler struct {
	client product.ProductServiceClient
}

func NewProductHandler(client product.ProductServiceClient) *ProductHandler {
	return &ProductHandler{client: client}
}

// GetProduct 获取单个商品信息
func (h *ProductHandler) GetProduct(c *gin.Context) {
	// 获取参数
	productIDStr := c.Param("id")
	// 10进制 64位
	productID, err := strconv.ParseInt(productIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    e.INVALID_PARAMS,
			"message": e.GetMsg(e.INVALID_PARAMS),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.client.GetProduct(ctx, &product.GetProductRequest{
		ProductId: productID,
	})
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    e.ERROR,
			"message": st.Message(),
		})
		return
	}

	if resp.GetCode() != e.SUCCESS {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    resp.GetCode(),
			"message": resp.GetMessage(),
		})
		return
	}

	JSONProto(c, http.StatusOK, &product.GetProductResponse{
		Code:    resp.GetCode(),
		Message: resp.GetMessage(),
		Product: resp.GetProduct(),
	})
}

// ListProducts 获取商品列表
func (h *ProductHandler) ListProducts(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "20")
	statusStr := c.DefaultQuery("status", "-1")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	pageSize, err := strconv.Atoi(pageSizeStr)
	if err != nil || pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// 解析状态
	stFilter, err := strconv.Atoi(statusStr)
	if err != nil {
		stFilter = -1
	}
	resp, err := h.client.ListProducts(ctx, &product.ListProductsRequest{
		Page:     int32(page),
		PageSize: int32(pageSize),
		Status:   int32(stFilter),
	})
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    e.ERROR,
			"message": st.Message(),
		})
		return
	}

	if resp.GetCode() != e.SUCCESS {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    resp.GetCode(),
			"message": resp.GetMessage(),
		})
		return
	}

	JSONProto(c, http.StatusOK, &product.ListProductsResponse{
		Code:     resp.GetCode(),
		Message:  resp.GetMessage(),
		Products: resp.GetProducts(),
		Total:    resp.GetTotal(),
	})
}

// CreateProduct 创建商品
func (h *ProductHandler) CreateProduct(c *gin.Context) {
	if username, ok := c.Get("username"); !ok || username.(string) != "admin" {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    e.ERROR,
			"message": "forbidden: admin only",
		})
		return
	}
	var req product.CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    e.INVALID_PARAMS,
			"message": e.GetMsg(e.INVALID_PARAMS),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.client.CreateProduct(ctx, &req)
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    e.ERROR,
			"message": st.Message(),
		})
		return
	}

	if resp.GetCode() != e.SUCCESS {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    resp.GetCode(),
			"message": resp.GetMessage(),
		})
		return
	}

	JSONProto(c, http.StatusCreated, &product.CreateProductResponse{
		Code:      resp.GetCode(),
		Message:   resp.GetMessage(),
		ProductId: resp.GetProductId(),
	})
}

// UpdateProduct 更新商品
func (h *ProductHandler) UpdateProduct(c *gin.Context) {
	if username, ok := c.Get("username"); !ok || username.(string) != "admin" {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    e.ERROR,
			"message": "forbidden: admin only",
		})
		return
	}
	productIDStr := c.Param("id")
	productID, err := strconv.ParseInt(productIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    e.INVALID_PARAMS,
			"message": e.GetMsg(e.INVALID_PARAMS),
		})
		return
	}

	var req product.UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    e.INVALID_PARAMS,
			"message": e.GetMsg(e.INVALID_PARAMS),
		})
		return
	}
	req.ProductId = productID

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.client.UpdateProduct(ctx, &req)
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    e.ERROR,
			"message": st.Message(),
		})
		return
	}

	if resp.GetCode() != e.SUCCESS {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    resp.GetCode(),
			"message": resp.GetMessage(),
		})
		return
	}

	JSONProto(c, http.StatusOK, &product.UpdateProductResponse{
		Code:    resp.GetCode(),
		Message: resp.GetMessage(),
	})
}

// DeleteProduct 删除商品
func (h *ProductHandler) DeleteProduct(c *gin.Context) {
	if username, ok := c.Get("username"); !ok || username.(string) != "admin" {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    e.ERROR,
			"message": "forbidden: admin only",
		})
		return
	}
	productIDStr := c.Param("id")
	productID, err := strconv.ParseInt(productIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    e.INVALID_PARAMS,
			"message": e.GetMsg(e.INVALID_PARAMS),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.client.DeleteProduct(ctx, &product.DeleteProductRequest{
		ProductId: productID,
	})
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    e.ERROR,
			"message": st.Message(),
		})
		return
	}

	if resp.GetCode() != e.SUCCESS {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    resp.GetCode(),
			"message": resp.GetMessage(),
		})
		return
	}

	JSONProto(c, http.StatusOK, &product.DeleteProductResponse{
		Code:    resp.GetCode(),
		Message: resp.GetMessage(),
	})
}

// RegisterRoutes 注册商品相关路由
func (h *ProductHandler) RegisterRoutes(rg *gin.RouterGroup) {
	products := rg.Group("/products")
	{
		products.GET("/:id", h.GetProduct)
		products.GET("", h.ListProducts)
		products.POST("", h.CreateProduct)
		products.PUT("/:id", h.UpdateProduct)
		products.DELETE("/:id", h.DeleteProduct)
	}
}
