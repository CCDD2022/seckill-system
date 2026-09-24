# HTTP 错误与异步秒杀请求约定

本项目的 HTTP 错误使用 [RFC 9457 Problem Details](https://www.rfc-editor.org/rfc/rfc9457.html)：状态码表达 HTTP 语义，响应头为 `Content-Type: application/problem+json`，`code` 扩展字段提供稳定的业务标识。客户端应判断 **HTTP 状态码与 `code`**，不要解析可能变化或翻译的 `detail`。状态码语义依据 [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html)；内部 RPC 按 [gRPC Status Codes](https://grpc.io/docs/guides/status-codes/) 返回类型化错误。

例如库存售罄：

```http
HTTP/1.1 409 Conflict
Content-Type: application/problem+json

{
  "type": "about:blank",
  "title": "Conflict",
  "status": 409,
  "detail": "库存不足",
  "instance": "/api/v1/seckill/execute",
  "code": "sold_out"
}
```

`type: "about:blank"` 表示使用标准 HTTP 状态标题；稳定的领域分类由 `code` 承担。内部数据库地址、SQL、gRPC 原始错误和堆栈不进入公开错误体。

## 秒杀请求

`POST /api/v1/seckill/execute` 在完成 Redis 库存预占和待发事件持久记录后返回：

```http
HTTP/1.1 202 Accepted
Location: /api/v1/seckill/requests/1003
Retry-After: 1
Content-Type: application/json

{
  "request_id": "create:42:1003",
  "state": "processing",
  "status_url": "/api/v1/seckill/requests/1003"
}
```

`202` 代表**已受理异步处理**，不是已创建订单，因此不返回 `order_id: 0` 或 `success: true`。同一用户、同一商品只允许一次预占。客户端按 `Location` 使用原 JWT 请求 `GET /api/v1/seckill/requests/:product_id`，可得到：

| `state` | 含义 |
| --- | --- |
| `processing` | Redis 已记录预占，尚未看到对应 MySQL 订单 |
| `order_created` | 已落库；响应另外带真实 `order_id` 与 `order_status` |
| `recovery_pending` | 创建事件进死信归档，等待运维核查、重放或补偿 |
| `failed` | 该预占已受控补偿，库存已归还 |

没有该用户／商品的预占记录时，状态地址返回 `404 reservation_not_found`。状态查询只使用 JWT 中的用户身份；不能通过请求参数查询其他用户。

## 常用 HTTP 状态与业务代码

| 状态 | 示例 `code` | 用途 |
| --- | --- | --- |
| `400` | `invalid_json`、`invalid_path_parameter` | JSON 或路径／查询参数格式不正确 |
| `401` | `authentication_required`、`invalid_credentials` | 缺少或无效凭据；响应带 `WWW-Authenticate: Bearer` |
| `403` | `admin_required` | 已登录但无此操作权限 |
| `404` | `product_not_found`、`order_not_found`、`reservation_not_found` | 资源不存在，或订单不属于当前用户 |
| `409` | `sold_out`、`campaign_inactive`、`already_participated`、`user_exists`、`order_status_conflict` | 与当前库存、活动、参与或资源状态冲突 |
| `422` | `invalid_request`、`validation_failed` | JSON 语法正确，但业务字段不合法，例如购买数量为零 |
| `429` | `rate_limited` | 网关限流；带 `Retry-After: 1` |
| `500` | `internal_error`、`outcome_unknown` | 内部错误；写操作的未知结果会同时给出状态查询地址 |
| `503` | `dependency_unavailable`、`inventory_unavailable`、`outcome_unknown` | 依赖暂不可用，或写入结果不确定 |
| `504` | `outcome_unknown`、`dependency_timeout` | 下游响应超时；写操作可能已经执行 |

重复参与的 `409 already_participated` 和结果不确定的 `503/504 outcome_unknown` 会附带 `request_id`、`status_url`，并设置 `Location`。收到它们后应先查询状态地址。若立即查询得到 `404`，仍可能是请求与查询之间的短暂竞态；等待 `Retry-After` 后复查，再决定是否重试提交。不要把 `500/503/504` 或客户端超时直接计为“没有预占”。

管理员创建商品遇到 MySQL 提交或 Redis 活动激活结果不确定时，返回 `503 outcome_unknown`，附带 `product_id` 和指向 `GET /api/v1/products/:id` 的 `Location`。此时不要盲目重复创建同名商品，应先核对该 ID；未确认提交的活动保持不可售。

当前其他接口的成功响应仍保持原有 Protobuf JSON 结构；这次变更统一的是**失败的 HTTP 状态与错误表示**。调用方若曾用 HTTP 200 加 `success: false` 判断失败，需改为按非 2xx 状态和 `code` 处理。
