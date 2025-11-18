package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/status"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/user"
)

// UserHandler 处理用户信息
type UserHandler struct {
	userClient user.UserServiceClient
}

func NewUserHandler(userClient user.UserServiceClient) *UserHandler {
	return &UserHandler{
		userClient: userClient,
	}
}

// GetProfile 获取当前登录用户信息
func (h *UserHandler) GetProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    e.ERROR_AUTH_CHECK_TOKEN_FAIL,
			"message": e.GetMsg(e.ERROR_AUTH_CHECK_TOKEN_FAIL),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	// ✅ 调用user.UserServiceClient的GetUser方法
	// ✅ GET 请求直接从上下文中获取 user_id，不需要绑定 JSON
	resp, err := h.userClient.GetUser(ctx, &user.GetUserRequest{
		UserId: userID.(int64),
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

	// ✅ 直接返回 Service 层的完整响应（统一使用 JSONProto）
	JSONProto(c, http.StatusOK, &user.GetUserResponse{
		Code:    resp.GetCode(),
		Message: resp.GetMessage(),
		User:    resp.GetUser(),
	})
}

// UpdateProfile 更新当前用户信息
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    e.ERROR_AUTH_CHECK_TOKEN_FAIL,
			"message": e.GetMsg(e.ERROR_AUTH_CHECK_TOKEN_FAIL),
		})
		return
	}

	var req user.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    e.INVALID_PARAMS,
			"message": e.GetMsg(e.INVALID_PARAMS),
		})
		return
	}
	req.UserId = userID.(int64)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.UpdateUser(ctx, &req)
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

	// ✅ 直接返回 Service 层的完整响应（统一使用 JSONProto）
	JSONProto(c, http.StatusOK, &user.UpdateUserResponse{
		Code:    resp.GetCode(),
		Message: resp.GetMessage(),
		User:    resp.GetUser(),
	})
}

// ChangePassword 修改密码
func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    e.ERROR_AUTH_CHECK_TOKEN_FAIL,
			"message": e.GetMsg(e.ERROR_AUTH_CHECK_TOKEN_FAIL),
		})
		return
	}

	var req user.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    e.INVALID_PARAMS,
			"message": e.GetMsg(e.INVALID_PARAMS),
		})
		return
	}
	req.UserId = userID.(int64)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.ChangePassword(ctx, &req)
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

	// ✅ 直接返回 Service 层的完整响应（统一使用 JSONProto）
	JSONProto(c, http.StatusOK, &user.ChangePasswordResponse{
		Code:    resp.GetCode(),
		Message: resp.GetMessage(),
	})
}

// RegisterRoutes 注册用户路由
func (h *UserHandler) RegisterRoutes(rg *gin.RouterGroup) {
	users := rg.Group("/users")
	{
		users.GET("/profile", h.GetProfile)
		users.PUT("/profile", h.UpdateProfile)
		users.PUT("/password", h.ChangePassword)
	}
}
