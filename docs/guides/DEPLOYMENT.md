# 部署说明

## Windows 本地进程

安装 Go 1.25 或更高版本后运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\generate.ps1
go test ./...
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1
```

演示脚本构建五个可执行文件，以隐藏进程启动，等待就绪，完成一次 5V5 闭环，并始终
关闭和等待全部进程。日志及强断言 JSON 报告位于 `.cache/demo`。

手动运行时按依赖顺序分别启动：

```powershell
go run .\cmd\player-service
go run .\cmd\matchmaking-service
go run .\cmd\simulation-service
go run .\cmd\agent-service
go run .\cmd\api-service
```

## Docker Compose

```powershell
docker compose config
docker compose up --build -d
docker compose ps
Invoke-RestMethod http://localhost:8080/ready
```

Compose 会运行 PostgreSQL 迁移、使用 PostgreSQL 的 Player/Match/Agent、使用 Redis
的活跃队列、十个匹配 Worker 和 Prometheus。默认开启 50/50 Greedy/Beam A/B 实验；
通过 `MATCHMAKING_POLICY_MODE=greedy` 或 `beam` 可以固定策略。

公共端点：

- API：`http://localhost:8080`
- Matchmaking 指标：`http://localhost:8082/metrics`
- Agent 指标：`http://localhost:8084/metrics`
- Prometheus：`http://localhost:9090`

停止但保留数据：

```powershell
docker compose down
```

同时删除开发数据库卷：

```powershell
docker compose down --volumes
```

容器以非 root 用户运行，包含健康检查，并通过私有 Compose 网络通信。完整配置见
`configs/.env.example`。非本地环境必须随机生成 `AGENT_CONTROL_TOKEN`，并只提供给
Matchmaking 和 Agent。REST 角色头只是参考身份边界，生产环境应在 API 前放置可信
身份网关或认证中间件。

## Agones 分配

默认本地分配器从 `MATCHMAKING_LOCAL_REGION_CAPACITIES` 读取容量。使用 Kubernetes/
Agones 时先应用最小权限配置：

```powershell
kubectl apply -f .\deployments\agones\matchmaking-rbac.yaml
```

Matchmaking Pod 使用 `serviceAccountName: matchmind-matchmaking`，并配置：

```text
MATCHMAKING_ALLOCATOR_BACKEND=agones
AGONES_NAMESPACE=matchmind
AGONES_API_URL=https://kubernetes.default.svc
```

每个 Fleet 必须带有 `matchmind.dev/game=matchmind` 和一个区域标签，例如
`matchmind.dev/region=hongkong`、`tokyo`、`singapore`。MatchMind 汇总各区域
`status.readyReplicas`，拒绝零容量或延迟不合格区域，然后按标签创建
`allocation.agones.dev/v1 GameServerAllocation`。

默认读取集群内 CA 和 ServiceAccount 令牌。除一次性测试集群外，不得启用
`AGONES_INSECURE_SKIP_TLS_VERIFY`。
