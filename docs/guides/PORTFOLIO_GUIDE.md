# MatchMind 求职展示与学习指南

## 项目定位

MatchMind 是一个 Go 实现的 5V5 智能匹配与战局质量优化后端。项目重点是
匹配算法、并发一致性、分布式状态、可复现模拟和安全的策略优化流程，不包含
游戏客户端、逐帧战斗、商城或社交系统。

适合投递的方向：Go 后端、微服务、游戏服务器、分布式系统和平台工程。

## 60 秒项目介绍

> 我实现了一个由 Player、Matchmaking、Simulation、Agent 和 API 五个服务组成
> 的 5V5 匹配平台。系统通过 Protobuf/gRPC 通信，支持 Greedy 和 Beam Search
> 分队、位置和组队约束、等待驱动的评分/延迟/位置放宽，并用 PostgreSQL 与
> Redis 保证 Ticket 原子预占和多 Worker 下的玩家唯一性。比赛由固定种子的
> 模拟器完成，赛后使用 Elo 或 Glicko-2 更新评分，并能进行质量分析、历史
> 回放和受人工审批约束的 Agent 策略实验。

## 三分钟演示

在仓库根目录运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1
```

演示应依次看到：

1. 五个服务构建并通过聚合 readiness；
2. 十名不同位置玩家进入排位队列；
3. Worker 创建 READY Match，并返回预测胜率和质量分；
4. 固定种子 `42` 生成可复现赛果；
5. 玩家评分变化且历史只写入一次；
6. 赛前预测与赛后实际质量得到比较；
7. 同一历史流量通过 Greedy 和 Beam 两种策略回放；
8. Agent 返回结构化提案和五项风险检查；
9. `.cache/demo/acceptance-report.json` 的 `status` 为 `passed`；
10. 脚本退出后所有服务端口关闭。

## 面试重点

### 1. 为什么需要 Ticket 状态机

说明 `CREATED → QUEUED → RESERVED → ASSIGNED`，以及取消、过期、失败路径。
重点解释非法转换、幂等键、reservation TTL 和崩溃恢复。

### 2. 如何避免同一玩家被重复匹配

内存路径用复合锁和 active-player 索引；PostgreSQL 使用部分唯一索引和事务；
Redis Lua 在写入前验证十个 Ticket，并以 PostgreSQL 作为持久化事实来源。

### 3. Greedy 与 Beam Search 的取舍

Greedy 延迟低但容易陷入局部最优；Beam Search 保留有限数量候选状态，提高
位置覆盖和整体质量，同时通过 Beam Width 与候选上限控制计算量。两者都有
稳定排序，保证相同输入产生相同结果。

### 4. 等待时间如何影响匹配

评分窗口、可接受延迟和非偏好位置分数随等待时间有界放宽；客户端版本、玩家
封禁、组队完整性和硬延迟上限永不放宽。普通模式比排位更快放宽，训练模式
使用沙盒规则，只有排位更新正式评分。

### 5. 为什么仍然需要对局模拟

没有真实客户端时，固定种子的模拟器让胜率、英雄熟练度、行为稳定性、AFK、
投降和实际质量形成闭环。它既支持在线 Match 完成，也支持不会修改线上状态的
万级离线批量仿真。

### 6. Agent 为什么不能直接修改实时策略

Agent 只有固定工具白名单。候选策略必须经过五项风险检查、不同人员审批和
管理员灰度激活；运行、工具调用、审批、发布和回滚都进入审计记录。

## 可验证命令

```powershell
go test -count=1 ./...
go vet ./...
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\race.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1
```

外部 PostgreSQL、Redis、Agones 和性能验收需要对应运行环境，不能用单元测试
结果代替。当前未安装这些环境时，应明确说明代码和环境隔离测试已经存在，但
真实部署与压测数字仍需单独提供。

## 简历描述示例

- 使用 Go、gRPC/Protobuf 构建五服务 5V5 智能匹配平台，实现 Ticket/Match
  状态机、Greedy/Beam Search 分队和多维质量评分。
- 设计 PostgreSQL 持久化与 Redis Lua 原子预占，在多 Worker 并发下保证玩家
  唯一分配、请求幂等及崩溃后预占恢复。
- 实现 Elo/Glicko-2、可复现战局模拟、历史回放、A/B 实验，以及包含风险审核、
  职责分离和审计回滚的 Agent 策略工作流。
- 建立 Trace ID、Prometheus、健康检查、全量测试、Race Detector 和一键五服务
  验收报告，形成可重复展示的完整后端闭环。
