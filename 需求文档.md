# MatchMind 智能匹配与战局质量优化平台需求文档

## 1. 文档信息

- 项目名称：MatchMind
- 项目类型：Go 后端、多人游戏匹配、Agent 辅助决策平台
- 目标用户：玩家、游戏运营人员、算法工程人员、系统管理员
- 文档版本：V1.0
- 开发目标：完成可运行、可测试、可演示的 5V5 智能匹配后端

---

## 2. 项目背景

传统 Elo 系统主要用于计算玩家实力，但无法单独解决多人竞技游戏中的位置偏好、网络延迟、开黑关系、等待时间和游戏服务器容量等问题。

MatchMind 在 Elo/Glicko 实力评分基础上构建多目标匹配系统，综合玩家实力、位置、网络延迟、组队结构、行为稳定性和等待时间生成高质量对局，并通过赛后数据、离线回放和 Agent 分析持续优化匹配策略。

---

## 3. 项目目标

### 3.1 业务目标

1. 为 5V5 多人竞技游戏提供完整匹配流程；
2. 降低双方实力差距；
3. 控制玩家匹配等待时间；
4. 提高位置分配满意度；
5. 降低高延迟和组队不对称造成的负面体验；
6. 避免同一玩家进入多个对局；
7. 支持战局质量评估和策略持续优化；
8. 使用 Agent 辅助数据分析、策略生成和风险审核。

### 3.2 技术目标

1. 使用 Go 构建高并发匹配后端；
2. 使用清晰的领域模型和模块边界；
3. 核心业务不直接依赖具体数据库或 Web 框架；
4. 支持 context 取消、超时和服务退出；
5. 支持单元测试、集成测试、竞态检测和压测；
6. 支持结构化日志、指标监控和链路追踪；
7. 支持后续接入 Redis、PostgreSQL、NATS 和 Agones。

---

## 4. 项目范围

### 4.1 第一阶段范围

第一阶段必须完成：

- 玩家基础信息；
- Elo 评分；
- 匹配 Ticket；
- 内存匹配队列；
- 创建、取消和查询匹配；
- 候选玩家生成；
- 5V5 贪心分队；
- 预测胜率；
- 基础质量评分；
- Ticket 原子预占；
- 比赛创建；
- 对局模拟器；
- 赛后 Elo 更新；
- REST API；
- 单元测试和基础监控。

### 4.2 第二阶段范围

第二阶段计划完成：

- PostgreSQL 持久化；
- Redis 活跃队列；
- 分布式匹配 Worker；
- Beam Search 分队；
- 位置偏好优化；
- 组队关系；
- 区域延迟和游戏服选择；
- 动态条件放宽；
- Glicko 评分；
- 历史 Ticket 回放；
- A/B 实验。

### 4.3 第三阶段范围

第三阶段计划完成：

- Agent 数据分析；
- Agent 策略生成；
- Agent 仿真调用；
- Agent 风险审核；
- 人工审批；
- 策略灰度发布；
- 实验报告；
- Agones 游戏服务器分配。

---

## 5. 用户角色

### 5.1 玩家

玩家可以：

- 查看个人评分；
- 创建匹配请求；
- 选择模式和位置偏好；
- 取消匹配；
- 查询匹配状态；
- 获取比赛服务器信息；
- 查看比赛结果和评分变化。

### 5.2 游戏运营人员

运营人员可以：

- 查看队列人数和等待时间；
- 查看匹配成功率；
- 查看不同区域的战局质量；
- 查看策略版本；
- 发起策略分析；
- 审核 Agent 生成的策略建议；
- 创建或停止 A/B 实验。

### 5.3 系统管理员

系统管理员可以：

- 管理服务状态；
- 管理匹配策略；
- 查看 Agent 审计日志；
- 查看异常和告警；
- 回滚错误策略；
- 管理游戏服务器容量。

---

## 6. 核心业务流程

### 6.1 创建匹配

1. 玩家提交模式、位置偏好和区域延迟；
2. 系统验证玩家登录状态；
3. 系统检查玩家是否已有有效 Ticket；
4. 系统读取玩家评分和组队信息；
5. 系统创建 Ticket；
6. Ticket 状态由 CREATED 转为 QUEUED；
7. Ticket 加入对应匹配池；
8. 系统返回 Ticket 编号。

### 6.2 取消匹配

1. 玩家提交 Ticket 编号；
2. 系统检查 Ticket 所属玩家；
3. 如果状态为 QUEUED，则转为 CANCELLED；
4. 如果状态为 RESERVED，需要根据业务规则拒绝或尝试释放；
5. 如果状态为 ASSIGNED，则不能直接取消；
6. 重复取消请求必须保持幂等。

### 6.3 匹配调度

1. 调度器从队列中选择等待时间最长的 Ticket；
2. 根据评分范围生成候选玩家；
3. 检查模式、版本、区域和组队等硬约束；
4. 候选人数不足时结束本轮；
5. 从候选人中选择 10 名玩家；
6. 组成两支 5 人队伍；
7. 为玩家分配位置；
8. 计算预测胜率和质量评分；
9. 低于最低质量阈值时不创建比赛；
10. 原子预占全部 Ticket；
11. 预占成功后创建 Match；
12. 分配游戏服务器；
13. Ticket 状态变为 ASSIGNED。

### 6.4 对局结束

1. 游戏服务器上报比赛结果；
2. 系统验证结果幂等性；
3. 保存双方比分、时长、挂机和投降信息；
4. 计算实际战局质量；
5. 更新玩家 Elo/Glicko 评分；
6. 保存评分变化；
7. 释放游戏服务器；
8. 将比赛状态更新为 FINISHED。

---

## 7. 功能需求

## 7.1 玩家管理

### FR-PLAYER-001 玩家创建

系统应支持创建模拟玩家。

必要字段：

- 玩家编号；
- 昵称；
- 初始评分；
- 偏好位置；
- 所属区域；
- 行为稳定分。

### FR-PLAYER-002 查询玩家

系统应支持根据玩家编号查询：

- 当前评分；
- 评分不确定度；
- 最近比赛；
- 当前匹配状态。

### FR-PLAYER-003 更新网络延迟

系统应支持更新玩家到不同服务器区域的延迟。

---

## 7.2 评分系统

### FR-RATING-001 预测胜率

系统应根据双方评分计算预期胜率。

### FR-RATING-002 更新 Elo

比赛结束后，系统应根据比赛结果更新双方评分。

### FR-RATING-003 参数配置

K Factor 不得写死，应通过配置或策略传入。

### FR-RATING-004 评分历史

系统应记录每次评分变化前后的数值和原因。

---

## 7.3 Ticket 管理

### FR-TICKET-001 创建 Ticket

同一玩家同一时间只能存在一个有效 Ticket。

### FR-TICKET-002 Ticket 状态机

状态至少包括：

- CREATED；
- QUEUED；
- RESERVED；
- ASSIGNED；
- CANCELLED；
- EXPIRED；
- FAILED。

### FR-TICKET-003 非法状态转换

系统必须拒绝非法状态转换并返回明确错误。

### FR-TICKET-004 Ticket 超时

等待时间超过配置上限的 Ticket 应转为 EXPIRED。

### FR-TICKET-005 幂等

创建和取消接口必须支持幂等键。

---

## 7.4 匹配队列

### FR-QUEUE-001 添加和移除

系统应支持添加、移除和查询 Ticket。

### FR-QUEUE-002 队列分区

队列至少按以下字段分区：

- 游戏模式；
- 客户端版本；
- 区域。

### FR-QUEUE-003 排序

同一匹配池内应优先处理等待时间更长的玩家。

### FR-QUEUE-004 并发安全

队列必须支持并发添加、取消和候选查询。

### FR-QUEUE-005 有效性过滤

候选查询不能返回 CANCELLED、EXPIRED 或已分配 Ticket。

---

## 7.5 候选生成

### FR-CANDIDATE-001 锚点选择

系统应优先选择等待时间最长的 Ticket 作为锚点。

### FR-CANDIDATE-002 评分窗口

系统应根据锚点评分和当前等待时间计算评分搜索范围。

### FR-CANDIDATE-003 候选上限

单次候选集应设置最大数量，防止组合搜索失控。

### FR-CANDIDATE-004 硬约束

候选玩家必须满足：

- 模式一致；
- 客户端版本一致；
- Ticket 状态有效；
- 玩家不重复；
- 开黑队伍不被拆散。

### FR-CANDIDATE-005 决策解释

系统应记录候选玩家通过或被拒绝的原因。

---

## 7.6 分队与位置分配

### FR-TEAM-001 队伍人数

每场比赛必须由两支各 5 人的队伍组成。

### FR-TEAM-002 玩家唯一性

玩家不能同时出现在两支队伍中。

### FR-TEAM-003 确定性

相同输入和相同策略版本应产生相同分队结果。

### FR-TEAM-004 贪心算法

第一版应实现基于评分平衡的贪心分队算法。

### FR-TEAM-005 Beam Search

第二版应支持 Beam Search，并可以比较算法质量和耗时。

### FR-TEAM-006 位置分配

系统应尽量满足玩家第一或第二位置偏好。

---

## 7.7 战局质量评分

### FR-QUALITY-001 子分数

质量评分至少包含：

- 实力公平分；
- 位置完整分；
- 延迟分；
- 组队对称分；
- 等待时间分。

### FR-QUALITY-002 分数范围

每个子分数范围为 0～100。

### FR-QUALITY-003 权重配置

各子分数权重由 MatchPolicy 配置，权重之和必须等于 1。

### FR-QUALITY-004 最低阈值

总质量分低于策略阈值时，不得创建比赛。

### FR-QUALITY-005 解释信息

质量结果必须包含分项得分和主要扣分原因。

---

## 7.8 动态条件放宽

### FR-EXPAND-001 评分范围放宽

随着等待时间增加，评分范围可以逐步扩大。

### FR-EXPAND-002 延迟范围放宽

随着等待时间增加，可接受延迟可以在上限内逐步扩大。

### FR-EXPAND-003 位置要求放宽

等待较久时，可以允许玩家进入非第一偏好位置。

### FR-EXPAND-004 硬约束不可放宽

以下条件不得放宽：

- 客户端版本；
- 玩家封禁状态；
- Ticket 唯一性；
- 开黑队伍完整性；
- 无法游戏的最大延迟上限。

---

## 7.9 Ticket 预占

### FR-RESERVE-001 原子预占

一场比赛所需的全部 Ticket 必须全部预占成功，才能继续创建比赛。

### FR-RESERVE-002 预占失败

任意 Ticket 预占失败时，本次预占整体失败。

### FR-RESERVE-003 预占标识

RESERVED Ticket 必须记录 reservation_id。

### FR-RESERVE-004 预占过期

预占必须设置 TTL。Worker 崩溃后，过期 Ticket 应恢复为 QUEUED。

### FR-RESERVE-005 重复分配保护

任何玩家都不能被分配到两场未结束比赛。

---

## 7.10 比赛与游戏服务器

### FR-MATCH-001 创建比赛

预占成功后，系统应创建唯一 Match。

### FR-MATCH-002 服务器选择

系统应根据玩家区域延迟和服务器容量选择游戏服务器区域。

### FR-MATCH-003 分配失败回滚

游戏服务器分配失败时，应释放 Ticket 预占。

### FR-MATCH-004 连接信息

分配成功后，玩家应能够获取服务器地址和连接令牌。

### FR-MATCH-005 状态管理

比赛状态至少包括：

- CREATED；
- ALLOCATING；
- READY；
- RUNNING；
- FINISHED；
- FAILED。

---

## 7.11 对局模拟器

### FR-SIM-001 可复现

相同比赛数据和随机种子必须得到相同结果。

### FR-SIM-002 实力影响

高评分队伍长期统计胜率应高于低评分队伍。

### FR-SIM-003 结果字段

模拟结果至少包含：

- 获胜队伍；
- 对局时长；
- 双方比分；
- 最大差距；
- 挂机；
- 提前投降。

### FR-SIM-004 批量模拟

系统应支持批量模拟，用于离线策略比较。

---

## 7.12 Agent 系统

### FR-AGENT-001 数据查询工具

Agent 只能通过白名单工具读取队列、比赛和策略指标。

### FR-AGENT-002 策略建议

Agent 可以生成结构化策略变更建议，但不能直接修改生产策略。

### FR-AGENT-003 离线仿真

Agent 可以调用历史回放或批量模拟工具验证策略。

### FR-AGENT-004 风险审核

策略发布前必须检查：

- 公平性恶化；
- 延迟突破上限；
- 补位率异常；
- 样本不足；
- 高分段体验恶化。

### FR-AGENT-005 人工审批

策略进入线上实验前必须经过人工审批。

### FR-AGENT-006 审计

每次 Agent 运行必须记录：

- Agent 名称；
- 模型；
- Prompt 版本；
- 工具调用；
- 输入与输出；
- 策略版本；
- 执行状态；
- 开始和结束时间。

---

## 8. 接口需求

### 8.1 创建匹配

`POST /api/v1/tickets`

请求示例：

```json
{
  "player_id": "player-1001",
  "mode": "ranked_5v5",
  "client_version": "1.0.0",
  "preferred_roles": ["core", "support"],
  "region_latency": {
    "singapore": 32,
    "tokyo": 76,
    "hongkong": 48
  },
  "idempotency_key": "create-player-1001-001"
}
```

### 8.2 取消匹配

`DELETE /api/v1/tickets/{ticket_id}`

### 8.3 查询 Ticket

`GET /api/v1/tickets/{ticket_id}`

### 8.4 查询比赛

`GET /api/v1/matches/{match_id}`

### 8.5 模拟比赛

`POST /api/v1/matches/{match_id}/simulate`

### 8.6 查询玩家评分

`GET /api/v1/players/{player_id}/rating`

### 8.7 健康检查

- `GET /health`
- `GET /ready`
- `GET /metrics`

---

## 9. 核心数据模型

### 9.1 Player

```go
type Player struct {
    ID              string
    Name            string
    Rating          float64
    RatingDeviation float64
    PreferredRoles  []Role
    RegionLatency   map[string]int
    BehaviorScore   float64
}
```

### 9.2 MatchTicket

```go
type MatchTicket struct {
    ID          string
    PlayerID    string
    PartyID     string
    Mode        string
    Region      string
    Rating      float64
    State       TicketState
    CreatedAt   time.Time

    ReservationID       string
    ReservationExpireAt time.Time
}
```

### 9.3 MatchPolicy

```go
type MatchPolicy struct {
    Version string

    SkillWeight   float64
    RoleWeight    float64
    LatencyWeight float64
    PartyWeight   float64
    WaitWeight    float64

    InitialRatingRange float64
    MaxRatingRange     float64
    MinQualityScore    float64

    ReservationTTL time.Duration
}
```

### 9.4 MatchQuality

```go
type MatchQuality struct {
    TotalScore  float64
    SkillScore  float64
    RoleScore   float64
    LatencyScore float64
    PartyScore  float64
    WaitScore   float64

    PredictedWinRateA float64
    PredictedWinRateB float64
    Reasons           []string
}
```

---

## 10. 非功能需求

### 10.1 性能

第一阶段目标：

- 单实例每秒接收不少于 500 个 Ticket 创建请求；
- 10 万活跃 Ticket 条件下，候选查询 P95 小于 100ms；
- 单次匹配计算 P95 小于 200ms；
- 支持不少于 10 个并发匹配 Worker。

以上属于开发目标，应通过压测验证，不作为未验证的项目成果宣传。

### 10.2 并发安全

- 所有共享内存结构必须通过竞态检测；
- 必须执行 `go test -race ./...`；
- 不允许玩家重复进入多个比赛；
- 所有后台 goroutine 必须支持退出。

### 10.3 可靠性

- 所有外部调用必须设置超时；
- 预占必须支持过期恢复；
- 重复消息必须幂等；
- 数据库写入失败不能造成半完成比赛；
- 服务重启后重要状态应可恢复。

### 10.4 安全性

- 玩家只能访问自己的 Ticket；
- 运营接口需要权限控制；
- Agent 工具必须使用白名单；
- Agent 不得执行任意 Shell 或 SQL；
- 敏感数据不得直接写入日志；
- 策略修改必须可审计和回滚。

### 10.5 可观测性

必须提供：

- 结构化日志；
- Prometheus 指标；
- Trace ID；
- Ticket ID；
- Match ID；
- Reservation ID；
- Policy Version。

核心指标包括：

- `match_queue_size`
- `match_wait_seconds`
- `match_attempt_total`
- `match_success_total`
- `match_failure_total`
- `match_quality_score`
- `ticket_reservation_conflict_total`
- `match_worker_duration_seconds`

---

## 11. 测试需求

### 11.1 单元测试

必须覆盖：

- Elo 胜率和评分更新；
- Ticket 合法和非法状态转换；
- 候选评分窗口；
- 贪心分队；
- 质量评分；
- 动态条件放宽；
- 预占成功、失败和过期；
- 模拟器可复现性。

### 11.2 并发测试

至少测试：

- 100 个 goroutine 同时创建 Ticket；
- 多个 Worker 竞争同一批玩家；
- 创建和取消同时发生；
- 预占超时与确认同时发生；
- 玩家不能重复分配。

### 11.3 集成测试

使用真实 PostgreSQL 和 Redis 测试：

- Ticket 持久化；
- Redis 原子预占；
- 比赛事务；
- 消息重复消费；
- 服务重启恢复。

### 11.4 压力测试

压测场景：

- 每秒创建 500 个 Ticket；
- 队列总量 10 万；
- 10 个匹配 Worker；
- 持续运行 10 分钟；
- 记录吞吐量、P95、P99、CPU、内存和 goroutine。

---

## 12. 开发阶段与验收标准

### 阶段一：领域模型与 Elo

验收：

- 完成 Player、Ticket、Team、Match；
- 完成 Ticket 状态机；
- 完成 Elo；
- `go test ./...` 通过。

### 阶段二：内存匹配闭环

验收：

- 创建和取消 Ticket；
- 候选生成；
- 贪心分队；
- 质量评分；
- 生成 Match；
- `go test -race ./...` 通过。

### 阶段三：对局模拟与评分更新

验收：

- 模拟结果可复现；
- 批量模拟有效；
- 比赛结束更新 Elo；
- 能展示完整演示流程。

### 阶段四：持久化与分布式

验收：

- PostgreSQL 保存玩家、Ticket 和比赛；
- Redis 管理活跃队列和预占；
- 多 Worker 下无重复匹配；
- 支持异常恢复。

### 阶段五：战局质量闭环

验收：

- 保存赛后过程指标；
- 比较预测质量与实际质量；
- 支持历史流量回放；
- 支持策略版本和 A/B 实验。

### 阶段六：Agent

验收：

- Agent 能通过工具查询指标；
- 能生成结构化策略建议；
- 能调用离线仿真；
- 能输出风险审核结果；
- 未经审批不能发布策略；
- 所有操作有审计日志。

---

## 13. 暂不实现内容

第一版不包含：

- 完整游戏客户端；
- 复杂战斗逻辑；
- 商城和支付；
- 公会和聊天；
- 语音系统；
- 真实机器学习训练平台；
- 全自动线上策略发布；
- 大规模生产级 Kubernetes 集群。

---

## 14. 最终交付物

项目最终至少包含：

```text
MatchMind/
├── cmd/
├── internal/
├── pkg/
├── api/
├── configs/
├── migrations/
├── deployments/
├── docs/
├── scripts/
├── tests/
├── docker-compose.yml
├── Makefile
├── go.mod
└── README.md
```

文档至少包含：

- 游戏内容文档；
- 需求文档；
- 系统架构文档；
- API 文档；
- 数据库设计文档；
- 测试方案；
- 部署说明；
- 项目演示说明。

---

## 15. 项目成功标准

项目达到以下条件，可以作为校招主项目：

1. 有完整可运行的匹配闭环；
2. 有不少于两种分队算法；
3. 能解释公平性与等待时间的权衡；
4. 能证明并发环境下玩家不会重复匹配；
5. 有模拟数据、历史回放和策略对比；
6. 有 Agent，但 Agent 不直接控制实时匹配；
7. 有单元测试、竞态检测、集成测试和压测；
8. 有清晰的架构、文档和可复现演示。
