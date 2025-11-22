package v1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/status"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/order"
)

// OrderHandler 订单相关处理器
type OrderHandler struct {
	orderClient order.OrderServiceClient
}

func NewOrderHandler(orderClient order.OrderServiceClient) *OrderHandler {
	return &OrderHandler{orderClient: orderClient}
}

// GetOrder 获取订单详情
func (h *OrderHandler) GetOrder(c *gin.Context) {
	idStr := c.Param("id")
	// 字符串转int
	orderID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": e.INVALID_PARAMS, "message": e.GetMsg(e.INVALID_PARAMS)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderClient.GetOrder(ctx, &order.GetOrderRequest{OrderId: orderID})
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": e.ERROR, "message": st.Message()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ListOrders 获取当前用户的订单列表
func (h *OrderHandler) ListOrders(c *gin.Context) {
	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": e.ERROR_AUTH, "message": e.GetMsg(e.ERROR_AUTH)})
		return
	}
	userID := userIDVal.(int64)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 10
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderClient.ListUserOrders(ctx, &order.ListUserOrdersRequest{
		UserId:   userID,
		Page:     int32(page),
		PageSize: int32(pageSize),
	})
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": e.ERROR, "message": st.Message()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// CancelOrder 取消订单
func (h *OrderHandler) CancelOrder(c *gin.Context) {
	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": e.ERROR_AUTH, "message": e.GetMsg(e.ERROR_AUTH)})
		return
	}
	userID := userIDVal.(int64)

	idStr := c.Param("id")
	orderID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": e.INVALID_PARAMS, "message": e.GetMsg(e.INVALID_PARAMS)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderClient.CancelOrder(ctx, &order.CancelOrderRequest{OrderId: orderID, UserId: userID})
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": e.ERROR, "message": st.Message()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// PayOrder 支付订单
func (h *OrderHandler) PayOrder(c *gin.Context) {
	userIDVal, ok := c.Get("user_id")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": e.ERROR_AUTH, "message": e.GetMsg(e.ERROR_AUTH)})
		return
	}
	userID := userIDVal.(int64)

	idStr := c.Param("id")
	orderID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": e.INVALID_PARAMS, "message": e.GetMsg(e.INVALID_PARAMS)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderClient.PayOrder(ctx, &order.PayOrderRequest{OrderId: orderID, UserId: userID})
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": e.ERROR, "message": st.Message()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// RegisterRoutes 注册订单相关路由
func (h *OrderHandler) RegisterRoutes(rg *gin.RouterGroup) {
	orders := rg.Group("/orders")
	{
		orders.GET("/my", h.ListOrders)
		orders.GET("/:id", h.GetOrder)
		orders.POST("/:id/cancel", h.CancelOrder)
		orders.POST("/:id/pay", h.PayOrder)
	}
}
