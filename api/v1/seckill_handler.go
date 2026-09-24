package v1

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/CCDD2022/seckill-system/proto_output/seckill"
	"github.com/gin-gonic/gin"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SeckillHandler struct {
	seckillClient seckill.SeckillServiceClient
}

func NewSeckillHandler(client seckill.SeckillServiceClient) *SeckillHandler {
	return &SeckillHandler{seckillClient: client}
}

type seckillProblem struct {
	status int
	code   string
	detail string
}

var seckillProblems = map[string]seckillProblem{
	"INVALID_REQUEST":                {http.StatusUnprocessableEntity, "invalid_request", "商品 ID 和购买数量必须大于零"},
	"PRODUCT_NOT_FOUND":              {http.StatusNotFound, "product_not_found", "商品不存在"},
	"RESERVATION_NOT_FOUND":          {http.StatusNotFound, "reservation_not_found", "没有找到该商品的秒杀请求"},
	"CAMPAIGN_NOT_CONFIGURED":        {http.StatusConflict, "campaign_not_configured", "商品没有有效的秒杀活动"},
	"CAMPAIGN_INACTIVE":              {http.StatusConflict, "campaign_inactive", "当前不在秒杀活动时间内"},
	"ALREADY_PARTICIPATED":           {http.StatusConflict, "already_participated", "该商品已参与过秒杀"},
	"SOLD_OUT":                       {http.StatusConflict, "sold_out", "库存不足"},
	"INVENTORY_UNAVAILABLE":          {http.StatusServiceUnavailable, "inventory_unavailable", "库存暂不可用"},
	"EVENT_PERSISTENCE_FAILED":       {http.StatusServiceUnavailable, "event_persistence_failed", "订单事件暂无法保存，请稍后再试"},
	"DEPENDENCY_UNAVAILABLE":         {http.StatusServiceUnavailable, "dependency_unavailable", "活动信息暂不可用"},
	"STATUS_UNAVAILABLE":             {http.StatusServiceUnavailable, "status_unavailable", "暂时无法查询秒杀状态"},
	"OUTCOME_UNKNOWN":                {http.StatusServiceUnavailable, "outcome_unknown", "请求结果尚未确认，请通过状态地址查询"},
	"INVALID_CAMPAIGN_CONFIGURATION": {http.StatusInternalServerError, "invalid_campaign_configuration", "活动配置异常"},
	"INTERNAL_ERROR":                 {http.StatusInternalServerError, "internal_error", "暂时无法处理请求"},
}

func reservationPath(productID int64) string {
	return fmt.Sprintf("/api/v1/seckill/requests/%d", productID)
}

func reservationID(userID, productID int64) string {
	return fmt.Sprintf("create:%d:%d", userID, productID)
}

func authenticatedUserID(c *gin.Context) (int64, bool) {
	value, ok := c.Get("user_id")
	if !ok {
		WriteProblem(c, http.StatusUnauthorized, "authentication_required", "请先登录")
		return 0, false
	}
	userID, ok := value.(int64)
	if !ok || userID <= 0 {
		WriteProblem(c, http.StatusUnauthorized, "authentication_required", "登录状态无效")
		return 0, false
	}
	return userID, true
}

// ExecuteSeckill creates a reservation request, not a persisted order.
func (h *SeckillHandler) ExecuteSeckill(c *gin.Context) {
	var req seckill.SeckillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "invalid_json", "请求体必须是有效的 JSON")
		return
	}
	if req.ProductId <= 0 || req.Quantity <= 0 {
		WriteProblem(c, http.StatusUnprocessableEntity, "invalid_request", "商品 ID 和购买数量必须大于零")
		return
	}
	userID, ok := authenticatedUserID(c)
	if !ok {
		return
	}
	req.UserId = userID
	requestID := reservationID(userID, req.ProductId)
	statusURL := reservationPath(req.ProductId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.seckillClient.ExecuteSeckill(ctx, &req)
	if err != nil {
		writeSeckillRPCProblem(c, err, requestID, statusURL, true)
		return
	}
	if resp == nil || !resp.GetSuccess() {
		WriteProblem(c, http.StatusInternalServerError, "invalid_service_response", "暂时无法处理请求")
		return
	}
	c.Header("Location", statusURL)
	c.Header("Retry-After", "1")
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusAccepted, gin.H{
		"request_id": requestID,
		"state":      "processing",
		"status_url": statusURL,
	})
}

// GetReservationStatus exposes the outcome of an asynchronous request.
func (h *SeckillHandler) GetReservationStatus(c *gin.Context) {
	productID, err := strconv.ParseInt(c.Param("product_id"), 10, 64)
	if err != nil {
		WriteProblem(c, http.StatusBadRequest, "invalid_product_id", "商品 ID 格式错误")
		return
	}
	if productID <= 0 {
		WriteProblem(c, http.StatusUnprocessableEntity, "invalid_product_id", "商品 ID 必须大于零")
		return
	}
	userID, ok := authenticatedUserID(c)
	if !ok {
		return
	}
	requestID := reservationID(userID, productID)
	statusURL := reservationPath(productID)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.seckillClient.GetReservationStatus(ctx, &seckill.GetReservationStatusRequest{
		UserId: userID, ProductId: productID,
	})
	if err != nil {
		writeSeckillRPCProblem(c, err, requestID, statusURL, false)
		return
	}
	if resp == nil {
		WriteProblem(c, http.StatusInternalServerError, "invalid_service_response", "暂时无法查询秒杀状态")
		return
	}
	content := gin.H{"request_id": requestID, "state": resp.GetState()}
	if resp.GetOrderId() > 0 {
		content["order_id"] = resp.GetOrderId()
		content["order_status"] = orderStatusName(resp.GetOrderStatus())
	}
	if resp.GetState() == "processing" || resp.GetState() == "recovery_pending" {
		c.Header("Retry-After", "1")
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, content)
}

func orderStatusName(status int32) string {
	switch status {
	case 0:
		return "pending_payment"
	case 1:
		return "paid"
	case 2:
		return "cancelled"
	case 3:
		return "completed"
	default:
		return "unknown"
	}
}

func writeSeckillRPCProblem(c *gin.Context, err error, requestID, statusURL string, mayHaveCommitted bool) {
	st, ok := status.FromError(err)
	if !ok {
		if mayHaveCommitted {
			statusCode := http.StatusInternalServerError
			if errors.Is(err, context.DeadlineExceeded) {
				statusCode = http.StatusGatewayTimeout
			}
			c.Header("Location", statusURL)
			c.Header("Retry-After", "1")
			WriteProblem(c, statusCode, "outcome_unknown", "请求结果尚未确认，请通过状态地址查询", gin.H{
				"request_id": requestID, "status_url": statusURL,
			})
			return
		}
		WriteProblem(c, http.StatusInternalServerError, "internal_error", "暂时无法处理请求")
		return
	}
	reason := ""
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == "seckill-system" {
			reason = info.Reason
			break
		}
	}
	problem, known := seckillProblems[reason]
	if !known {
		switch st.Code() {
		case codes.DeadlineExceeded:
			problem = seckillProblem{http.StatusGatewayTimeout, "gateway_timeout", "下游服务响应超时"}
		case codes.Unavailable:
			problem = seckillProblem{http.StatusServiceUnavailable, "service_unavailable", "服务暂不可用"}
		case codes.ResourceExhausted:
			problem = seckillProblem{http.StatusServiceUnavailable, "service_unavailable", "服务暂不可用"}
		case codes.InvalidArgument:
			problem = seckillProblem{http.StatusUnprocessableEntity, "invalid_request", "请求参数无效"}
		case codes.NotFound:
			problem = seckillProblem{http.StatusNotFound, "not_found", "资源不存在"}
		default:
			problem = seckillProblem{http.StatusInternalServerError, "internal_error", "暂时无法处理请求"}
		}
	}
	if mayHaveCommitted && (reason == "OUTCOME_UNKNOWN" || reason == "INTERNAL_ERROR" ||
		st.Code() == codes.DeadlineExceeded ||
		(!known && (st.Code() == codes.Unavailable || st.Code() == codes.Internal ||
			st.Code() == codes.Unknown || st.Code() == codes.Canceled))) {
		problem.code = "outcome_unknown"
		problem.detail = "请求结果尚未确认，请通过状态地址查询"
	}
	if problem.code == "already_participated" || problem.code == "outcome_unknown" {
		c.Header("Location", statusURL)
		c.Header("Retry-After", "1")
		WriteProblem(c, problem.status, problem.code, problem.detail, gin.H{
			"request_id": requestID, "status_url": statusURL,
		})
		return
	}
	if problem.status == http.StatusServiceUnavailable {
		c.Header("Retry-After", "1")
	}
	WriteProblem(c, problem.status, problem.code, problem.detail)
}

func (h *SeckillHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/execute", h.ExecuteSeckill)
	rg.GET("/requests/:product_id", h.GetReservationStatus)
}
