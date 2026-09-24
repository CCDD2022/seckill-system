package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/CCDD2022/seckill-system/pkg/utils"
	"github.com/gin-gonic/gin"
)

// Kept in middleware to avoid an import cycle with v1 handler tests.
func writeProblem(c *gin.Context, statusCode int, code, detail string) {
	c.Header("Content-Type", "application/problem+json")
	if statusCode == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", `Bearer realm="api"`)
	}
	c.JSON(statusCode, gin.H{
		"type": "about:blank", "title": http.StatusText(statusCode),
		"status": statusCode, "detail": detail,
		"instance": c.Request.URL.Path, "code": code,
	})
}

func JWTAuthMiddleware(jwtUtil *utils.JWTUtil) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" || strings.TrimSpace(parts[1]) == "" {
			writeProblem(c, http.StatusUnauthorized, "authentication_required", "请提供有效的 Bearer 令牌")
			c.Abort()
			return
		}
		claims, err := jwtUtil.ParseToken(parts[1])
		if err != nil {
			if errors.Is(err, utils.ErrTokenExpired) {
				writeProblem(c, http.StatusUnauthorized, "token_expired", "登录已过期，请重新登录")
			} else {
				writeProblem(c, http.StatusUnauthorized, "invalid_token", "令牌无效，请重新登录")
			}
			c.Abort()
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("is_admin", claims.IsAdmin)
		c.Next()
	}
}
