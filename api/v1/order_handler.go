package v1

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/order"
)

type OrderHandler struct{ orderClient order.OrderServiceClient }

func NewOrderHandler(orderClient order.OrderServiceClient) *OrderHandler {
	return &OrderHandler{orderClient: orderClient}
}

func orderID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		WriteProblem(c, http.StatusBadRequest, "invalid_path_parameter", "订单 ID 格式不正确")
		return 0, false
	}
	if id <= 0 {
		writeInvalidInput(c, "订单 ID 必须大于 0")
		return 0, false
	}
	return id, true
}

func writeOrderResultProblem(c *gin.Context, code int32) {
	switch code {
	case e.ERROR_NOT_EXIST, e.ERROR:
		WriteProblem(c, http.StatusNotFound, "order_not_found", "订单不存在")
	case e.ERROR_AUTH:
		writeUnauthenticated(c)
	case e.ERROR_ORDER_STATUS_CHANGED:
		WriteProblem(c, http.StatusConflict, "order_status_conflict", "订单状态不允许此操作")
	default:
		WriteProblem(c, http.StatusInternalServerError, "internal_error", "服务器暂时无法处理请求")
	}
}

func (h *OrderHandler) GetOrder(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	id, ok := orderID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.orderClient.GetOrder(ctx, &order.GetOrderRequest{OrderId: id, UserId: userID})
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		writeOrderResultProblem(c, resp.GetCode())
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *OrderHandler) ListOrders(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if pageErr != nil || sizeErr != nil {
		WriteProblem(c, http.StatusBadRequest, "invalid_query_parameter", "分页参数格式不正确")
		return
	}
	if page < 1 || page > math.MaxInt32 || pageSize < 1 || pageSize > 100 || (page-1) > math.MaxInt32/pageSize {
		writeInvalidInput(c, "分页参数超出允许范围")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.orderClient.ListUserOrders(ctx, &order.ListUserOrdersRequest{UserId: userID, Page: int32(page), PageSize: int32(pageSize)})
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		WriteProblem(c, http.StatusInternalServerError, "internal_error", "服务器暂时无法处理请求")
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *OrderHandler) CancelOrder(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	id, ok := orderID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.orderClient.CancelOrder(ctx, &order.CancelOrderRequest{OrderId: id, UserId: userID})
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		writeOrderResultProblem(c, resp.GetCode())
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *OrderHandler) PayOrder(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	id, ok := orderID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.orderClient.PayOrder(ctx, &order.PayOrderRequest{OrderId: id, UserId: userID})
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		writeOrderResultProblem(c, resp.GetCode())
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *OrderHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/my", h.ListOrders)
	rg.GET("/:id", h.GetOrder)
	rg.POST("/:id/cancel", h.CancelOrder)
	rg.POST("/:id/pay", h.PayOrder)
}
