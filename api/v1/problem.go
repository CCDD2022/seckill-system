package v1

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WriteProblem sends one RFC 9457 problem response. The code extension is a
// stable application identifier; detail must be safe to show to end users.
func WriteProblem(c *gin.Context, statusCode int, code, detail string, extras ...gin.H) {
	c.Header("Content-Type", "application/problem+json")
	if statusCode == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", `Bearer realm="api"`)
	}
	problem := gin.H{
		"type":     "about:blank",
		"title":    http.StatusText(statusCode),
		"status":   statusCode,
		"detail":   detail,
		"instance": c.Request.URL.Path,
		"code":     code,
	}
	for _, extra := range extras {
		for key, value := range extra {
			switch key {
			case "type", "title", "status", "detail", "instance", "code":
				continue
			default:
				problem[key] = value
			}
		}
	}
	c.JSON(statusCode, problem)
}

func writeRPCProblem(c *gin.Context, err error) {
	st, ok := status.FromError(err)
	if ok {
		switch st.Code() {
		case codes.Unavailable, codes.ResourceExhausted:
			WriteProblem(c, http.StatusServiceUnavailable, "dependency_unavailable", "服务暂时不可用，请稍后重试")
			return
		case codes.DeadlineExceeded:
			WriteProblem(c, http.StatusGatewayTimeout, "dependency_timeout", "服务响应超时，请稍后查询结果")
			return
		case codes.InvalidArgument:
			writeInvalidInput(c, "请求参数无效")
			return
		case codes.NotFound:
			WriteProblem(c, http.StatusNotFound, "resource_not_found", "资源不存在")
			return
		case codes.AlreadyExists, codes.Aborted, codes.FailedPrecondition:
			WriteProblem(c, http.StatusConflict, "resource_conflict", "资源当前状态不允许此操作")
			return
		case codes.Unauthenticated:
			writeUnauthenticated(c)
			return
		case codes.PermissionDenied:
			WriteProblem(c, http.StatusForbidden, "permission_denied", "没有权限执行此操作")
			return
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || c.Request.Context().Err() == context.DeadlineExceeded {
		WriteProblem(c, http.StatusGatewayTimeout, "dependency_timeout", "服务响应超时，请稍后查询结果")
		return
	}
	WriteProblem(c, http.StatusInternalServerError, "internal_error", "服务器暂时无法处理请求")
}

func writeInvalidJSON(c *gin.Context) {
	WriteProblem(c, http.StatusBadRequest, "invalid_json", "请求体不是有效的 JSON 或字段类型不正确")
}

func writeInvalidInput(c *gin.Context, detail string) {
	WriteProblem(c, http.StatusUnprocessableEntity, "validation_failed", detail)
}

func writeUnauthenticated(c *gin.Context) {
	WriteProblem(c, http.StatusUnauthorized, "authentication_required", "请先登录")
}
