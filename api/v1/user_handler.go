package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/user"
)

type UserHandler struct{ userClient user.UserServiceClient }

func NewUserHandler(userClient user.UserServiceClient) *UserHandler {
	return &UserHandler{userClient: userClient}
}

func currentUserID(c *gin.Context) (int64, bool) {
	value, ok := c.Get("user_id")
	id, valid := value.(int64)
	if !ok || !valid || id <= 0 {
		writeUnauthenticated(c)
		return 0, false
	}
	return id, true
}

func (h *UserHandler) GetProfile(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.userClient.GetUser(ctx, &user.GetUserRequest{UserId: userID})
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		if resp.GetCode() == e.ERROR_USER_NOT_EXISTS {
			WriteProblem(c, http.StatusNotFound, "user_not_found", "用户不存在")
		} else {
			WriteProblem(c, http.StatusInternalServerError, "internal_error", "服务器暂时无法处理请求")
		}
		return
	}
	JSONProto(c, http.StatusOK, &user.GetUserResponse{Code: resp.GetCode(), Message: resp.GetMessage(), User: resp.GetUser()})
}

func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req user.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInvalidJSON(c)
		return
	}
	if req.Email == "" && req.Phone == "" {
		writeInvalidInput(c, "请提供邮箱或手机号")
		return
	}
	req.UserId = userID
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.userClient.UpdateUser(ctx, &req)
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		if resp.GetCode() == e.ERROR_USER_NOT_EXISTS {
			WriteProblem(c, http.StatusNotFound, "user_not_found", "用户不存在")
		} else {
			WriteProblem(c, http.StatusInternalServerError, "internal_error", "服务器暂时无法处理请求")
		}
		return
	}
	JSONProto(c, http.StatusOK, &user.UpdateUserResponse{Code: resp.GetCode(), Message: resp.GetMessage(), User: resp.GetUser()})
}

func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req user.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInvalidJSON(c)
		return
	}
	if req.OldPassword == "" || len(req.NewPassword) < 8 {
		writeInvalidInput(c, "旧密码不能为空，新密码至少 8 位")
		return
	}
	req.UserId = userID
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.userClient.ChangePassword(ctx, &req)
	if err != nil {
		writeRPCProblem(c, err)
		return
	}
	if resp.GetCode() != e.SUCCESS {
		switch resp.GetCode() {
		case e.ERROR_USER_NOT_EXISTS:
			WriteProblem(c, http.StatusNotFound, "user_not_found", "用户不存在")
		case e.ERROR_PASSWORD:
			WriteProblem(c, http.StatusUnprocessableEntity, "old_password_incorrect", "旧密码不正确")
		default:
			WriteProblem(c, http.StatusInternalServerError, "internal_error", "服务器暂时无法处理请求")
		}
		return
	}
	JSONProto(c, http.StatusOK, &user.ChangePasswordResponse{Code: resp.GetCode(), Message: resp.GetMessage()})
}

func (h *UserHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/profile", h.GetProfile)
	rg.PUT("/profile", h.UpdateProfile)
	rg.PUT("/password", h.ChangePassword)
}
