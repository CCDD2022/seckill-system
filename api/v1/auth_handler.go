package v1

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/auth"
)

// AuthHandler 处理认证
type AuthHandler struct{ authClient auth.AuthServiceClient }

func NewAuthHandler(authClient auth.AuthServiceClient) *AuthHandler {
	return &AuthHandler{authClient: authClient}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req auth.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInvalidJSON(c)
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		writeInvalidInput(c, "用户名和密码不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.authClient.Login(ctx, &req)
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		switch resp.GetCode() {
		case e.ERROR_USER_NOT_EXISTS, e.ERROR_PASSWORD:
			WriteProblem(c, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		default:
			WriteProblem(c, http.StatusInternalServerError, "internal_error", "服务器暂时无法处理请求")
		}
		return
	}
	JSONProto(c, http.StatusOK, &auth.LoginResponse{Code: resp.GetCode(), Message: resp.GetMessage(), Token: resp.GetToken(), User: resp.GetUser()})
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req auth.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInvalidJSON(c)
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		writeInvalidInput(c, "用户名和密码不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.authClient.Register(ctx, &req)
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		switch resp.GetCode() {
		case e.ERROR_USER_EXISTS:
			WriteProblem(c, http.StatusConflict, "user_exists", "用户名已被使用")
		case e.INVALID_PARAMS:
			writeInvalidInput(c, "用户名不可注册")
		default:
			WriteProblem(c, http.StatusInternalServerError, "internal_error", "服务器暂时无法处理请求")
		}
		return
	}
	JSONProto(c, http.StatusOK, &auth.RegisterResponse{Code: resp.GetCode(), Message: resp.GetMessage(), User: resp.GetUser()})
}

func (h *AuthHandler) RegisterRoutes(rg *gin.RouterGroup) {
	auth := rg.Group("/auth")
	auth.POST("/login", h.Login)
	auth.POST("/register", h.Register)
}
