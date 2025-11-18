package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/status"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/auth"
)

// AuthHandler 处理认证
type AuthHandler struct {
	authClient auth.AuthServiceClient
}

func NewAuthHandler(authClient auth.AuthServiceClient) *AuthHandler {
	return &AuthHandler{
		authClient: authClient,
	}
}

// Login 用户登录
func (h *AuthHandler) Login(c *gin.Context) {
	var req auth.LoginRequest
	// 登录参数要匹配
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    e.INVALID_PARAMS,
			"message": e.GetMsg(e.INVALID_PARAMS),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	//  调用auth.AuthServiceClient的Login方法
	resp, err := h.authClient.Login(ctx, &req)
	if err != nil {
		st, _ := status.FromError(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    e.ERROR,
			"message": st.Message(),
		})
		return
	}

	//  使用getter方法访问字段
	if resp.GetCode() != e.SUCCESS {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    resp.GetCode(),
			"message": resp.GetMessage(),
		})
		return
	}

	JSONProto(c, http.StatusOK, &auth.LoginResponse{
		Code:    resp.GetCode(),
		Message: resp.GetMessage(),
		Token:   resp.GetToken(),
		User:    resp.GetUser(),
	})
}

// Register 用户注册
func (h *AuthHandler) Register(c *gin.Context) {
	var req auth.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    e.INVALID_PARAMS,
			"message": e.GetMsg(e.INVALID_PARAMS),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.authClient.Register(ctx, &req)
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

	JSONProto(c, http.StatusOK, &auth.RegisterResponse{
		Code:    resp.GetCode(),
		Message: resp.GetMessage(),
		User:    resp.GetUser(),
	})
}

// RegisterRoutes 注册路由
func (h *AuthHandler) RegisterRoutes(rg *gin.RouterGroup) {
	auth := rg.Group("/auth")
	{
		auth.POST("/login", h.Login)
		auth.POST("/register", h.Register)
	}
}
