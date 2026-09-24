# 秒杀链路设计与故障边界

本文描述仓库当前实现，适用范围是 `docker-compose.yml` 中的单机演示栈。对外只有 HTTP 网关；内部 gRPC、MySQL、Redis 和 RabbitMQ 位于 Compose 私有网络。

## 用户可见的状态

- `POST /api/v1/seckill/execute` 返回 HTTP 202：Redis 已预占库存并把待创建订单事件写入 Redis Stream。响应提供 `request_id` 与 `Location` 状态地址；订单尚未必然落库，不返回占位订单 ID。
- 已知业务拒绝使用相应的非 2xx 状态：例如购买数量无效为 422，商品不存在为 404，活动未开放、库存不足或重复参与为 409。错误体使用 `application/problem+json` 和稳定 `code`，详见 [API 错误约定](docs/api-errors.md)。
- 超时或返回 `outcome_unknown` 时，Redis 脚本的响应可能已在网络中丢失。客户端先查询 `GET /api/v1/seckill/requests/:product_id`，按 `Retry-After` 复查；用户与商品的参与标记会挡住重复预占。
- `POST /api/v1/orders/:id/cancel` 成功：订单状态和取消事件在同一个 MySQL 事务中提交；库存回补仍可能在处理队列中。

## 正常路径

1. 管理员创建商品时，MySQL 事务写商品，并在提交前初始化 Redis 库存和未激活的活动价格、起止时间；确认 MySQL 提交后才激活活动。提交结果不确定时活动保持不可售，后续需核对 MySQL。活动价格与时间创建后不可修改。旧数据首次访问时从 MySQL 补充活动元数据；库存键缺失时一律拒绝秒杀，不从可能滞后的 MySQL 库存自动恢复。
2. 网关校验 JWT 并限流。秒杀服务读取 Redis 活动元数据，计算以分为单位的订单金额。Redis Lua 用服务器时间检查活动窗口，在一次原子执行中完成参与去重、库存扣减、脏商品标记与 `XADD` 待发订单事件。
3. `reservation-relay` 消费 Redis Stream，等待 RabbitMQ 持久消息的发布确认且要求消息可路由后才 `XACK`。崩溃或确认结果不明时保留事件并重投。
4. `order-create-consumer` 在 MySQL 落库后才确认 RabbitMQ 消息。订单表对 `source_message_id` 以及 `(user_id, product_id)` 设唯一约束；重复投递产生相同业务结果。
5. 用户取消待支付订单时，订单状态 CAS 更新与 `outbox_events` 插入同事务提交。`order-outbox-relay` 确认发布取消事件后标记已发布；失败记录原因并退避重试。
6. `order-cancel-consumer` 先核对数据库中确有对应的已取消订单，再用 Redis Lua 把库存回补与取消事件去重标记放在同一次执行中；完成后才确认消息。
7. `stock-reconciler` 把 Redis 可售库存同步为 MySQL 商品库存快照。它只读取脏集合，写库成功后用 Lua 比较 Redis 库存是否仍等于本次样本；一致才移除脏标记。并发新扣减会保留或重新加入待对账集合。

## 需要始终成立的约束

| 约束 | 防线 |
| --- | --- |
| 可售库存不能变为负数 | Redis Lua 检查数量和当前库存，扣减与事件记录串行执行 |
| 一个用户对同一商品最多有一个有效预占/订单 | Redis 参与集合；MySQL `(user_id, product_id)` 唯一索引 |
| 已受理预占不能仅因 RabbitMQ 故障消失 | Redis AOF `appendfsync always`、持久卷、Stream 待发事件、确认后才 ACK |
| 重投不能创建第二单或二次回补 | MySQL 消息/业务唯一索引；Redis 取消事件和人工补偿标记 |
| 取消状态和回补意图不能只成功一半 | MySQL 事务 outbox；确认发布后标记完成 |
| 对账进程崩溃不能丢待对账商品 | 先更新数据库、再按库存样本比较并移除 Redis 脏标记 |

## 故障与恢复

| 故障点 | 系统行为 | 核验/恢复 |
| --- | --- | --- |
| 创建商品的 MySQL COMMIT 响应丢失 | 活动先保持未激活；核对 MySQL 记录后才允许预占 | 创建接口返回 `503 outcome_unknown` 和商品查询地址；不要盲目重复创建 |
| Redis 预占请求超时或连接断开 | 结果未知，不自动回补 | 按 `Location` 查询异步请求状态；必要时核对参与标记、Stream 和消息积压 |
| Redis 停机 | 无新请求受理 | AOF 持久卷恢复后 relay 继续读取 Stream |
| RabbitMQ 或创建消费者停机 | Stream 或持久队列留存待处理工作 | 恢复服务，核对订单数与队列积压 |
| MySQL 暂时停机或订单写库连接失败 | Redis 仍可受理，创建消费者退避后重新入队，不确认完成 | 数据库恢复后自动落库；关注 RabbitMQ 积压和重试日志 |
| 创建消息格式错误或与已落库业务键冲突 | 消息进死信队列，归档到 MySQL 后才 ACK | 核对原因后按归档 ID 受控重放；永久失败可在严格前提下人工补偿 |
| 取消事件发布失败 | `outbox_events` 保留并退避重试 | 查看 `attempts`、`last_error`、`next_attempt_at` 和最终库存 |
| 取消库存回补时 Redis 暂不可用 | 消费者退避后重新入队 | Redis 恢复后自动回补；`ReturnStockOnce` 按事件 ID 去重 |
| 取消事件数据或库存键异常 | 消息进死信队列 | 核对订单与库存状态后受控重放或人工处理 |
| 对账写库失败或进程崩溃 | 商品仍留在脏集合 | 恢复后再次同步，直到 MySQL 与 Redis 库存样本一致 |

死信重放只重新投递原事件。人工补偿只用于已确认无法修复的创建订单死信：先停 `reservation-relay` 与 `order-create-consumer`，确认主队列无人消费且清空、Redis Stream 没有同一事件、数据库没有对应订单。补偿命令原子地回补一次库存并留下永久补偿标记；之后到达的同一消息会被创建消费者拒绝。具体命令见 [README](README.md)。

## 运行与验证范围

本地 Compose 采用单 Redis 实例、AOF 同步落盘和持久卷；这牺牲峰值吞吐以缩小已受理请求丢失的窗口。它没有实现跨机房容灾、内部 gRPC 的 mTLS、支付接入、管理员 JWT 即时吊销或可证明的历史吞吐数字。多节点 Redis Cluster 也未实现：Lua 使用的库存、参与集合、脏集合和 Stream 不在同一哈希槽。

验证分三层：`go test ./...` 检查权限、发布确认、幂等和 Lua 边界；`SMOKE_FULL=1 python3 scripts/smoke.py` 与 `python3 scripts/concurrency_smoke.py` 检查完整业务不变量；`python3 scripts/fault_smoke.py`、`python3 scripts/compensation_smoke.py` 在专用本地栈检查中断恢复和人工补偿。上述检查是正确性证据，不能替代完整负载基准与灾备演练。
