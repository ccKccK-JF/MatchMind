# MatchMind

MatchMind 是一个使用 Go 实现的 5V5 智能匹配与战局质量优化平台。项目采用多个
可独立运行的微服务，外部提供 REST API，内部使用 Protobuf/gRPC 通信，重点展示
匹配算法、并发一致性、分布式状态、战局模拟、历史回放和受控 Agent 策略优化。

## 服务组成

| 服务 | 默认地址 | 职责 |
|---|---:|---|
| `player-service` | gRPC `:50051`、HTTP `:8081` | 玩家资料、封禁和 Elo/Glicko-2 评分 |
| `matchmaking-service` | gRPC `:50052`、HTTP `:8082` | Ticket、队列、分队、预占、游戏服分配和 Match |
| `simulation-service` | gRPC `:50053`、HTTP `:8083` | 固定种子的可复现战局模拟和赛后闭环 |
| `agent-service` | gRPC `:50054`、HTTP `:8084` | 离线策略建议、风险审核、审批审计和灰度发布 |
| `api-service` | HTTP `:8080` | 公共 REST 网关和下游聚合就绪检查 |

四个内部服务都提供标准 gRPC 健康检查和反射；每个进程都提供 HTTP 存活、就绪和
Prometheus 指标端点。

## 已实现能力

- 玩家创建、查询、区域延迟更新、管理员封禁、英雄熟练度和行为稳定分；
- 可配置 Elo 或 Glicko-2，保存评分、不确定度、波动率和幂等评分历史；
- 严格的 Ticket/Match 状态机，以及创建、取消、结果上报等幂等边界；
- 按模式、客户端版本和区域分池，最长等待优先，并支持并发安全的队列操作；
- 随等待时间有界放宽评分窗口、延迟范围和非偏好位置，硬约束始终不可放宽；
- 保持开黑队伍完整，支持 Greedy 与 Beam Search 两种确定性 5V5 分队算法；
- 实力、位置、延迟、组队和等待五维质量评分，并返回主要扣分原因；
- 排位、普通和训练三种受控模式，只有排位模式修改正式评分；
- 10 个英雄、五个位置、自动选择位置兼容且熟练度最高的英雄；
- PostgreSQL 持久化、Redis Lua 原子预占、过期恢复和多 Worker 唯一分配；
- 基于平均/最大延迟、队间差异、方差和容量的跨区域游戏服选择；
- 本地容量分配器和 Agones `GameServerAllocation` 适配器；
- 固定随机种子的战局模拟，涵盖胜负、比分、时长、资源差、AFK、投降和实际质量；
- 历史质量分析、Greedy/Beam 回放、稳定 A/B 分桶及策略版本对比；
- 只读工具白名单、五项风险检查、职责分离、人工审批、灰度发布和回滚审计；
- 端到端 Trace ID、结构化日志、Prometheus 指标、超时和优雅关闭；
- 单元、并发、集成、竞态和一键五服务验收脚本。

## 目录结构

```text
cmd/                    各服务与工具的进程入口
configs/                环境变量示例
deployments/            Prometheus 与 Agones 部署配置
docs/
  product/              原始需求与游戏内容边界
  design/               架构、数据库、英雄和模式设计
  guides/               API、部署、测试、演示和求职指南
  quality/              需求到实现证据的追踪矩阵
gen/go/                 根据 Protobuf 生成的 Go 代码
internal/               领域、应用、适配器和平台实现
migrations/             按顺序执行的 PostgreSQL 迁移
proto/                  内部服务契约的唯一事实来源
scripts/                生成、工具安装、竞态和演示脚本
tests/integration/      跨服务及外部环境集成测试
```

业务规则位于 `internal/*/domain`，用例编排位于 `application`，传输、网关和仓储
只负责适配外部协议与基础设施。领域层不直接依赖 gRPC、数据库或消息中间件。

## Windows 本地开发

安装代码生成工具：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install-tools.ps1
```

需要使用本机 Clash 代理时：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install-tools.ps1 `
  -Proxy http://127.0.0.1:7897
```

检查 Protobuf 并生成 Go 代码：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\generate.ps1
```

执行基础质量门禁：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\check-docs.ps1
go mod tidy
go test -count=1 ./...
go vet ./...
```

安装固定版本的 Windows 竞态检测工具链并运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install-race-tools.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\race.ps1
```

执行完整五服务演示：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1
```

脚本会构建并临时启动五个服务，验证完整闭环，写入
`.cache/demo/acceptance-report.json`，然后无条件关闭所有临时进程。

如需手动启动，按依赖顺序在不同终端运行：

```powershell
go run .\cmd\player-service
go run .\cmd\matchmaking-service
go run .\cmd\simulation-service
go run .\cmd\agent-service
go run .\cmd\api-service
```

公共 API 位于 `http://localhost:8080`，匹配指标位于
`http://localhost:8082/metrics`。

## 常用配置

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `PLAYER_RATING_SYSTEM` | `elo` | `elo` 或 `glicko2` |
| `PLAYER_ELO_K_FACTOR` | `32` | Elo K Factor |
| `PLAYER_GLICKO2_TAU` | `0.5` | Glicko-2 系统常数，范围 `0.3` 到 `1.2` |
| `PLAYER_STORAGE_BACKEND` | `memory` | `memory` 或 `postgres` |
| `POSTGRES_DSN` | 本地 MatchMind DSN | PostgreSQL 连接串 |
| `MATCHMAKING_WORKER_COUNT` | `1` | 匹配 Worker 数量 |
| `MATCHMAKING_POLICY_MODE` | `beam` | `greedy`、`beam` 或 `ab` |
| `MATCHMAKING_BEAM_WIDTH` | `64` | Beam Search 宽度 |
| `MATCHMAKING_AB_TREATMENT_BPS` | `5000` | 实验组基点数 |
| `MATCHMAKING_ALLOCATOR_BACKEND` | `local` | `local` 或 `agones` |
| `MATCHMAKING_LOCAL_REGION_CAPACITIES` | `hongkong=100,singapore=100,tokyo=100` | 本地各区域容量 |
| `MATCHMAKING_TICKET_STORAGE_BACKEND` | `memory` | `memory` 或 `redis` |
| `MATCHMAKING_MATCH_STORAGE_BACKEND` | 跟随 Ticket 后端 | `memory` 或 `postgres` |
| `REDIS_ADDRESS` | `localhost:6379` | Redis 地址 |
| `AGENT_STORAGE_BACKEND` | `memory` | `memory` 或 `postgres` |
| `AGENT_CONTROL_TOKEN` | 本地开发令牌 | Agent 与 Matchmaking 必须一致 |

端口、Agones、Agent 版本和全部配置见 `configs/.env.example`。

## 文档入口

完整文档导航见 [docs/README.md](docs/README.md)。推荐阅读顺序：

1. [产品需求](docs/product/REQUIREMENTS.md)
2. [游戏内容边界](docs/product/GAME_CONTENT.md)
3. [系统架构](docs/design/ARCHITECTURE.md)
4. [HTTP API](docs/guides/API.md)
5. [可重复演示](docs/guides/DEMO.md)
6. [求职展示与学习指南](docs/guides/PORTFOLIO_GUIDE.md)
7. [需求追踪矩阵](docs/quality/REQUIREMENTS_TRACEABILITY.md)
