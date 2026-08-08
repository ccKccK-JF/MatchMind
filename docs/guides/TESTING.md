# 测试方案

## 快速质量门禁

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\check-docs.ps1
go test -count=1 ./...
go vet ./...
```

文档门禁要求 Markdown 文件和目录使用英文 ASCII 名称、每份文档包含中文正文、相对
链接全部存在，并保持 `product/design/guides/quality` 四层信息架构。

测试覆盖：

- Player、Ticket、Match、Agent 聚合不变量和状态转换；
- Elo 数学与 Glicko-2 官方示例、评分持久化和结果幂等；
- 动态候选窗口、位置分配、Greedy/Beam、质量分和稳定 A/B 分桶；
- 排位/普通/训练模式校验、模式策略派生和仅排位更新评分；
- 玩家封禁、实时资格复核、关闭式失败和取消后重新排队；
- 英雄目录、熟练度不可变性、自动选择及对胜率的统计影响；
- 行为分从 Player 到 Ticket/Match 的链路和低稳定性 AFK 统计影响；
- 预占冲突、失败回滚、TTL 恢复、服务器容量和跨区域选择；
- 固定种子复现、万级批量边界、并发 1/16 的字节级一致报告；
- HTTP/gRPC 映射、状态码、身份权限、Trace ID 和 Prometheus 输出；
- 质量分析、Brier 分数、历史回放只读性和 Agent 五项风险工作流；
- 从十名玩家排队到 Match、评分、分析、回放和 Agent 的完整 gRPC 集成流程。

## PostgreSQL 集成

使用真实 PostgreSQL 时启用独立 Schema 测试：

```powershell
$env:MATCHMIND_POSTGRES_TEST_DSN = "postgres://matchmind:matchmind@localhost:5432/matchmind?sslmode=disable"
go test -count=1 .\tests\integration -run PostgreSQL
```

测试会应用嵌入迁移，验证 Player、幂等 Ticket、十行原子预占、Match 恢复、乐观版本
冲突、Match/Ticket 事务分配、比赛期间禁止重新排队、赛后释放、历史质量查询，以及
Agent 运行/提案事务和审核并发控制。

## Redis 集成

```powershell
$env:MATCHMIND_REDIS_TEST_ADDRESS = "localhost:6379"
go test -count=1 .\tests\integration -run Redis
```

快速测试使用进程内 Redis 实现执行同一组 Lua 脚本，并注入元数据缺失、竞争预占、
过期集合、持久层拒绝和 Redis 全量丢失。真实 Redis 测试用于验证协议与服务行为。

## 并发与竞态

并发场景包括：

- 100 个 goroutine 为不同玩家创建 Ticket；
- 100 个 goroutine 竞争同一玩家的活跃 Ticket；
- Ticket 创建与取消并发；
- 预占过期与分配确认竞争；
- 十个 Worker 竞争同一个十人批次；
- Match 完成前后玩家唯一性和重新排队。

安装固定、校验和验证过的 MinGW-w64 GCC 并运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install-race-tools.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\race.ps1
```

竞态命令是发布门禁，普通 `go test` 不能替代。

## 五服务验收

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1
```

报告位于 `.cache/demo/acceptance-report.json`。它验证 readiness、Match、固定种子、
评分历史、质量分析、两种回放、五项 Agent 风险和指标，并在退出后清理全部服务。

## 压力测试

启动完整环境后运行：

```powershell
go run .\cmd\loadtest -rate 500 -duration 10m -max-tickets 100000 -concurrency 256
```

JSON 输出包含成功 Ticket 吞吐、失败数、P95/P99 延迟以及压测进程的堆和 goroutine
数量。运行期间同时记录 `docker stats`。测试 10 万纯队列时设置
`MATCHMAKING_WORKER_COUNT=0`；多 Worker 测试使用 Compose 默认值 `10`。

任何性能结论都必须同时给出机器配置、环境变量、Git Commit 和原始输出，不能只报告
一个脱离环境的数字。
