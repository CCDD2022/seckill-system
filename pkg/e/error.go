package e

// 错误码定义
const (
	SUCCESS        = 0
	ERROR          = 1
	INVALID_PARAMS = 2

	ERROR_AUTH_CHECK_TOKEN_FAIL    = 10001
	ERROR_AUTH_CHECK_TOKEN_TIMEOUT = 10002
	ERROR_AUTH_TOKEN               = 10003
	ERROR_AUTH                     = 10004

	ERROR_USER_EXISTS     = 20001
	ERROR_USER_NOT_EXISTS = 20002
	ERROR_PASSWORD        = 20003

	ERROR_PRODUCT_NOT_EXISTS = 30001
	ERROR_STOCK_NOT_ENOUGH   = 30002
)

var MsgFlags = map[int]string{
	SUCCESS:        "成功",
	ERROR:          "失败",
	INVALID_PARAMS: "请求参数错误",

	ERROR_AUTH_CHECK_TOKEN_FAIL:    "Token验证失败",
	ERROR_AUTH_CHECK_TOKEN_TIMEOUT: "Token已超时",
	ERROR_AUTH_TOKEN:               "Token生成失败",
	ERROR_AUTH:                     "认证失败",

	ERROR_USER_EXISTS:     "用户已存在",
	ERROR_USER_NOT_EXISTS: "用户不存在",
	ERROR_PASSWORD:        "密码错误",

	ERROR_PRODUCT_NOT_EXISTS: "商品不存在",
	ERROR_STOCK_NOT_ENOUGH:   "库存不足",
}

func GetMsg(code int) string {
	msg, ok := MsgFlags[code]
	if ok {
		return msg
	}
	return MsgFlags[ERROR]
}
