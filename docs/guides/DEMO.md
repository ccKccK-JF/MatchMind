# 可重复演示

在仓库根目录运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1
```

脚本会把带强断言的机器可读报告写到
`.cache/demo/acceptance-report.json`。CI 需要上传到其他位置时可指定：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1 `
  -ReportPath .cache\portfolio\demo-report.json
```

## 演示流程

1. 构建并临时启动 Player、Matchmaking、Simulation、Agent 和 API 五个服务；
2. 等待 `GET /ready` 确认全部 gRPC 依赖就绪；
3. 创建十名位置均衡的玩家；
4. 创建十个幂等排位 Ticket；
5. 等待 Worker 创建一个 READY Match；
6. 查询预测胜率和五项质量分；
7. 使用固定种子 `42` 模拟比赛；
8. 查询玩家新评分和唯一一条评分历史；
9. 比较赛前预测质量和赛后实际质量；
10. 使用 Greedy 与 Beam 两种策略回放历史 Ticket；
11. 使用分析员身份让 Agent 生成结构化候选策略；
12. 验证五项风险、API/Matchmaking 指标，写入 JSON 报告并打印摘要；
13. 无论成功或失败，都强制关闭并等待全部临时进程退出。

相同输入和种子会产生相同赛果。API 响应包含 `X-Trace-ID`，运行期间可在 `8080`、
`8082`、`8084` 查看指标。单场演示会故意触发 `sample_size` 阻断风险，用于证明样本
不足的提案不能被批准或激活。

缺少状态、种子、评分历史、质量分析、两个回放结果、五项风险或指标时，脚本会失败。
失败报告包含错误和每个服务最后 20 行 stderr。日志位于 `.cache/demo`，该目录与构建
产物不会进入 Git。
