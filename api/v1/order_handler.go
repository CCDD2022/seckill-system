package v1

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/order"
	"github.com/gin-gonic/gin"
)

// OrderHandler 订单 HTTP 处理器
type OrderHandler struct {
	orderClient order.OrderServiceClient
}

func NewOrderHandler(orderClient order.OrderServiceClient) *OrderHandler {
	return &OrderHandler{orderClient: orderClient}
}

// RegisterRoutes 注册订单相关路由（需 JWT）
func (h *OrderHandler) RegisterRoutes(rg *gin.RouterGroup) {
	// 统一规范：不在 handler 内再创建分组或添加限流
	rg.GET("/my", h.ListMyOrders)
	rg.GET(":id", h.GetOrder)
	rg.POST(":id/cancel", h.CancelOrder)
	rg.POST(":id/pay", h.PayOrder)
}

func (h *OrderHandler) ListMyOrders(c *gin.Context) {
	userID := c.GetInt64("user_id")
	page := toInt32(c.DefaultQuery("page", "1"))
	pageSize := toInt32(c.DefaultQuery("page_size", "20"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.orderClient.ListUserOrders(ctx, &order.ListUserOrdersRequest{
		UserId:   userID,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": e.ERROR, "message": "获取订单失败"})
		return
	}
	JSONProto(c, http.StatusOK, resp)
}

func (h *OrderHandler) GetOrder(c *gin.Context) {
	userID := c.GetInt64("user_id")
	orderID := toInt64(c.Param("id"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.orderClient.GetOrder(ctx, &order.GetOrderRequest{OrderId: orderID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": e.ERROR, "message": "获取订单失败"})
		return
	}
	// 简单校验归属
	if resp.GetOrder() != nil && resp.GetOrder().GetUserId() != userID {
		c.JSON(http.StatusForbidden, gin.H{"code": e.ERROR, "message": "无权访问该订单"})
		return
	}
	JSONProto(c, http.StatusOK, resp)
}

func (h *OrderHandler) CancelOrder(c *gin.Context) {
	userID := c.GetInt64("user_id")
	orderID := toInt64(c.Param("id"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.orderClient.CancelOrder(ctx, &order.CancelOrderRequest{OrderId: orderID, UserId: userID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": e.ERROR, "message": err.Error()})
		return
	}
	JSONProto(c, http.StatusOK, resp)
}

func (h *OrderHandler) PayOrder(c *gin.Context) {
	userID := c.GetInt64("user_id")
	orderID := toInt64(c.Param("id"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.orderClient.PayOrder(ctx, &order.PayOrderRequest{OrderId: orderID, UserId: userID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": e.ERROR, "message": "支付失败"})
		return
	}
	JSONProto(c, http.StatusOK, resp)
}

// 工具
func toInt64(s string) int64 {
	var r int64
	_, _ = fmt.Sscan(s, &r)
	return r
}
func toInt32(s string) int32 {
	var r int32
	_, _ = fmt.Sscan(s, &r)
	if r <= 0 {
		r = 1
	}
	return r
}
