# MatchMind HTTP API

公共 API 默认监听 `http://localhost:8080`。JSON 字段统一使用 `snake_case`，内部
服务间通信使用 gRPC。

每个响应都包含 `X-Trace-ID`。调用方可以提供不超过 128 个字符的 Trace ID，只允许
ASCII 字母、数字、`-`、`_`、`.`、`:`、`/`；非法值会被替换。Trace ID 会继续写入
下游 gRPC 元数据和结构化日志。

错误响应格式：

```json
{
  "error": {
    "code": "not_found",
    "message": "ticket not found"
  }
}
```

## 玩家接口

### 创建玩家

```http
POST /api/v1/players
Content-Type: application/json

{
  "id": "player-1001",
  "name": "Nova",
  "initial_rating": 1500,
  "preferred_roles": ["core", "support"],
  "home_region": "hongkong",
  "region_latency": {
    "hongkong": 32,
    "singapore": 48
  },
  "behavior_score": 95,
  "hero_proficiency": {
    "starblade": 92,
    "lifebloom": 81
  }
}
```

`hero_proficiency` 的键是英雄 ID，值为 0 到 100。只要提交了任意英雄，未提交英雄
按 0 处理；整个表为空时，自动选择使用中性熟练度 50。英雄 ID 见
[英雄目录](../design/HEROES.md)。`behavior_score` 同样限制在 0 到 100，并进入
每个 Ticket 的不可变快照。

### 查询评分与玩家状态

```http
GET /api/v1/players/player-1001/rating
```

响应包含当前评分、评分偏差、Glicko-2 波动率和完整评分历史。每条历史记录包含不确定
状态的前后值和 `rating_system`：REST JSON 中 `1` 表示 Elo，`2` 表示 Glicko-2。
响应还包含最近十个 Match ID、当前匹配状态（`IDLE`、`QUEUED`、`RESERVED`、
`ASSIGNED`），以及存在时的活跃 Ticket。

### 更新区域延迟

身份头必须与路径玩家一致：

```http
PATCH /api/v1/players/player-1001/latency
X-Player-ID: player-1001
Content-Type: application/json

{
  "region_latency": {
    "hongkong": 28,
    "singapore": 46,
    "tokyo": 72
  }
}
```

### 封禁或解封玩家

仅管理员可以操作。封禁需要非空原因，服务会记录管理员和 UTC 时间。封禁玩家不能
创建 Ticket；已在排队的玩家若等待期间被封禁，其 Ticket 会在匹配前取消。

```http
PATCH /api/v1/players/player-1001/ban
X-Operator-ID: admin-1
X-Operator-Role: admin
Content-Type: application/json

{
  "banned": true,
  "reason": "confirmed cheating"
}
```

解封发送 `{"banned":false}`。`analyst` 和 `reviewer` 角色无权调用。

## Ticket 接口

`mode` 只接受 `ranked_5v5`、`normal_5v5`、`training_5v5`。排位使用严格策略并在
赛后更新评分；普通更快放宽且不改排位分；训练使用沙盒规则且不改分。详细规则见
[游戏模式](../design/GAME_MODES.md)。

### 创建幂等 Ticket

```http
POST /api/v1/tickets
Content-Type: application/json
Idempotency-Key: create-player-1001-001
X-Player-ID: player-1001

{
  "player_id": "player-1001",
  "mode": "ranked_5v5",
  "client_version": "1.0.0",
  "preferred_roles": ["core", "support"],
  "region_latency": {
    "hongkong": 32,
    "singapore": 48
  }
}
```

幂等键也可以放在请求体的 `idempotency_key`。`X-Player-ID` 必须与 `player_id` 一致。

### 查询 Ticket

```http
GET /api/v1/tickets/{ticket_id}
X-Player-ID: player-1001
```

只有 Ticket 所属玩家可以查看详情。

### 取消 Ticket

```http
DELETE /api/v1/tickets/{ticket_id}
X-Player-ID: player-1001
Idempotency-Key: cancel-player-1001-001
```

取消幂等键也可以使用查询参数 `idempotency_key`。创建、查询和取消都会拒绝缺失或
不匹配的玩家身份。

## Match 与模拟接口

### 查询 Match

```http
GET /api/v1/matches/{match_id}
```

### 模拟线上 Match

```http
POST /api/v1/matches/{match_id}/simulate
Content-Type: application/json

{
  "random_seed": 42
}
```

省略种子时网关生成并返回种子。每个 Match 的模拟结果幂等，重复请求返回已保存结果，
不会再次更新评分。

### 离线批量模拟

```http
POST /api/v1/simulations/batch
Content-Type: application/json

{
  "concurrency": 8,
  "inputs": [
    {
      "case_id": "balanced-001",
      "random_seed": 42,
      "rating_a": 1500,
      "rating_b": 1500,
      "predicted_win_rate_a": 0.5,
      "role_score": 95,
      "latency_score": 90,
      "party_score": 100,
      "hero_proficiency_a": 85,
      "hero_proficiency_b": 80,
      "behavior_score_a": 98,
      "behavior_score_b": 92
    }
  ]
}
```

批量接口接受 1 到 10,000 个案例，保持输入顺序，并返回每场结果以及胜率、时长、
实际质量、一边倒、AFK 和投降汇总。`concurrency` 默认使用可用 CPU 数，最大为 64。
所有分数字段范围为 0 到 100。英雄熟练度影响有效实力和实际质量；行为稳定分影响
有效实力和 AFK 概率。离线批量不会修改线上 Match 或玩家评分。

## 历史分析与回放

### 分析完成的 Match

可按策略、模式、服务器区域和 RFC3339 半开时间范围过滤：

```http
GET /api/v1/analytics/match-quality?policy_version=v2-beam&mode=ranked_5v5&server_region=hongkong&from=2026-08-01T00%3A00%3A00Z&to=2026-08-08T00%3A00%3A00Z&limit=100
```

每场观测包含预测/实际质量、带符号误差、绝对误差、A 队预测胜率、实际结果和 Brier
分数。策略汇总包含平均质量误差、胜率 Brier 分数、胜率、时长、一边倒、AFK 和投降
比例。默认返回 100 场，最大 1,000 场。

### 历史回放

```http
POST /api/v1/matches/{match_id}/replay
Content-Type: application/json

{
  "policy_versions": ["v1-greedy", "v2-beam"]
}
```

未指定 `ticket_ids` 时，系统重建源 Match 的十个 Ticket 快照。也可在所选策略的候选
上限内提交更多历史 Ticket。每个结果返回接受/拒绝数量、队伍和位置、预测质量、相对
源 Match 的差值、搜索诊断，以及队伍/位置是否与历史一致。无法成局的反事实结果使用
`matched:false` 返回。回放不会预占 Ticket、创建 Match 或更新评分。

## Agent 策略工作流

Agent 接口要求 `X-Operator-ID` 和 `X-Operator-Role`。参考角色为 `analyst`、
`reviewer`、`admin`；审计身份只从请求头获取，不接受请求体伪造。

### 创建离线分析

分析员或管理员可以调用：

```http
POST /api/v1/agent/runs
X-Operator-ID: analyst-1
X-Operator-Role: analyst
Content-Type: application/json

{
  "base_policy_version": "v2-beam",
  "mode": "ranked_5v5",
  "server_region": "hongkong",
  "historical_limit": 20
}
```

响应包含运行审计、全部白名单工具调用、候选策略、理由，以及公平性、延迟上限、位置
填充、样本量、高分玩家体验五项风险。本操作只读快照并执行历史回放，不能修改实时匹配。

### 查询运行和提案

```http
GET /api/v1/agent/runs?limit=20
GET /api/v1/agent/runs/{run_id}
GET /api/v1/agent/proposals?limit=20
GET /api/v1/agent/proposals/{proposal_id}
```

三种运营角色都可以查询。

### 审核提案

审核人必须与申请人不同。不存在阻断风险时可以批准，也可以拒绝：

```http
POST /api/v1/agent/proposals/{proposal_id}/review
X-Operator-ID: reviewer-1
X-Operator-Role: reviewer
Content-Type: application/json

{
  "decision": "approve",
  "reason": "offline checks passed"
}
```

### 激活与回滚

只有管理员可以激活已批准提案或回滚。灰度基点范围为 1 到 10,000，与分桶盐一同
持久化，重试不能改变实验语义。

```http
POST /api/v1/agent/proposals/{proposal_id}/activate
X-Operator-ID: admin-1
X-Operator-Role: admin
Content-Type: application/json

{
  "treatment_basis_points": 1000,
  "assignment_salt": "guarded-rollout-2026-08"
}
```

```http
POST /api/v1/agent/proposals/{proposal_id}/rollback
X-Operator-ID: admin-1
X-Operator-Role: admin
```

## 运维端点

公共网关提供：

- `GET /health`：进程存活；
- `GET /ready`：Player、Matchmaking、Simulation、Agent 的聚合就绪状态；
- `GET /metrics`：API Prometheus 指标。

Matchmaking 在 `:8082` 提供相同端点，核心指标包括：

- `match_queue_size`
- `match_wait_seconds`
- `match_attempt_total`
- `match_success_total`
- `match_failure_total`
- `match_quality_score`
- `ticket_reservation_conflict_total`
- `match_worker_duration_seconds`
- `match_team_formation_greedy_duration_seconds`
- `match_team_formation_beam_duration_seconds`
- `match_team_formation_greedy_quality_score`
- `match_team_formation_beam_quality_score`

Player、Simulation、Agent 分别在 `:8081`、`:8083`、`:8084` 提供运维端点。
Agent 指标包含运行结果/耗时以及提案批准、拒绝、激活和回滚计数；内部 gRPC 健康服务
用于依赖就绪检查。
