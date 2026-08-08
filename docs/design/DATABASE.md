# 数据库设计

本地五服务演示默认使用并发安全的内存仓储。通过配置可以让 Player、Ticket、Match、
评分历史和 Agent 审计使用 PostgreSQL，并让 Redis 管理活跃匹配队列与预占。

## PostgreSQL 数据所有权

### `players`

保存玩家资料、当前评分、Glicko-2 评分偏差与波动率、行为分、英雄熟练度 JSON，
以及封禁标志、原因、操作人和时间。数据库约束要求封禁状态必须具有完整元数据，
正常状态不能残留封禁信息。

### `rating_changes`

仅追加保存评分、偏差和波动率的前后值、评分算法和原因。`sequence` 保留同一 Match
中十名玩家的响应顺序，`(match_id, player_id)` 唯一键防止重复赛果二次更新评分。
评分更新使用可串行化事务和 Match 级 advisory lock。

### `tickets`

保存 Ticket 完整状态、行为分和英雄熟练度快照、创建/取消幂等键、预占信息、Match ID
和时间。活跃状态上的 `player_id` 部分唯一索引防止多个 API 实例并发创建重复 Ticket。
行锁保证十个 Ticket 的预占、分配、释放和过期恢复原子执行。

`active` 区分不可变历史记录和实时资格：Match 处于 READY/RUNNING 时，其已分配
Ticket 仍然活跃；比赛完成后在同一事务中清除十个活跃标志，玩家才能再次排队。

### `matches`

保存模式、策略版本、五项质量分、队伍快照、英雄与行为数据、服务器分配、状态、
预测胜率、赛果、实际质量和单调版本号。更新拒绝旧版本写入。READY Match 与十个
Ticket 的 ASSIGNED 转换共享一个事务。

历史质量分析使用策略/创建时间索引；历史回放把 Match 中的 Ticket ID 连接回持久
快照。二者均不修改 Match、Ticket 或评分。

### `agent_runs` 与 `policy_proposals`

`agent_runs` 是追加式审计记录，包含 Agent/模型/提示版本、请求、结构化输出、工具调用
记录、状态和耗时。启动恢复会把遗留 `RUNNING` 标为失败，不会静默重跑。

`policy_proposals` 保存候选策略、理由、五项风险、申请/审核身份、状态、灰度基点、
分桶盐、实验 ID、激活/回滚信息和时间。状态更新使用比较并交换条件；运行完成与提案
创建在同一事务中提交。

## Redis 数据所有权

Redis 只保存可重建的活跃协调数据：

- 按 `mode:version:region` 分池的 Sorted Set，分数为创建时间；
- 用于快速候选读取的 Ticket 快照；
- 带 TTL 的预占键；
- 防止重复玩家的活跃键；
- 短期 Worker 租约和过期预占索引。

Lua 脚本先校验十个 Ticket 的全部状态，再一次性写入预占；任一条件失败则零写入。
服务启动时会从 PostgreSQL 活跃 Ticket 重建 Redis，并协调过期预占。

## 一致性原则

- PostgreSQL 是 Player、Ticket、Match、评分和 Agent 审计的持久化事实来源；
- Redis 是可以丢弃并重建的实时协调层；
- 相同 reservation 的重试保持幂等；
- PostgreSQL 拒绝提交时回滚 Redis 预占；
- 不宣称跨系统“恰好一次”，而是使用事务、本地唯一约束、幂等和补偿实现可验证结果。

Outbox/NATS 仍属于可选扩展，不是第一版匹配闭环的必要条件。
