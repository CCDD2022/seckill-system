package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/seckill"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/status"
)

// SeckillHandler 秒杀处理器
type SeckillHandler struct {
	seckillClient seckill.SeckillServiceClient
}

func NewSeckillHandler(seckillClient seckill.SeckillServiceClient) *SeckillHandler {
	return &SeckillHandler{
		seckillClient: seckillClient,
	}
}

// ExecuteSeckill 执行秒杀
func (h *SeckillHandler) ExecuteSeckill(c *gin.Context) {
	var req seckill.SeckillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    e.INVALID_PARAMS,
			"message": e.GetMsg(e.INVALID_PARAMS),
		})
		return
	}

	// 从JWT中获取用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    e.ERROR_AUTH,
			"message": "未授权访问",
		})
		return
	}
	req.UserId = userID.(int64)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.seckillClient.ExecuteSeckill(ctx, &req)
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    e.ERROR,
			"message": st.Message(),
		})
		return
	}

	if !resp.GetSuccess() {
		c.JSON(http.StatusOK, gin.H{
			"code":    e.ERROR,
			"message": resp.GetMessage(),
			"success": false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":     e.SUCCESS,
		"message":  resp.GetMessage(),
		"success":  true,
		"order_id": resp.GetOrderId(),
	})
}

// RegisterRoutes 注册路由
func (h *SeckillHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/execute", h.ExecuteSeckill)
}
