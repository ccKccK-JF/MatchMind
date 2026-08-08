# MatchMind 文档中心

本目录按“产品边界 → 技术设计 → 使用指南 → 质量证据”组织。文件名和目录名保持
英文，正文统一使用中文；代码标识、协议字段、命令和标准技术名称保留原文。

## 产品文档

- [需求文档](product/REQUIREMENTS.md)：范围、功能需求、非功能需求和验收标准。
- [游戏内容文档](product/GAME_CONTENT.md)：玩法抽象、位置、英雄、模式和模拟边界。

## 设计文档

- [系统架构](design/ARCHITECTURE.md)：服务边界、完整链路、并发与安全设计。
- [数据库设计](design/DATABASE.md)：PostgreSQL/Redis 数据所有权和一致性策略。
- [游戏模式](design/GAME_MODES.md)：排位、普通、训练的差异化规则。
- [英雄目录](design/HEROES.md)：英雄属性、位置和熟练度选择规则。

## 使用指南

- [HTTP API](guides/API.md)：公共接口、身份头、示例和运维端点。
- [部署说明](guides/DEPLOYMENT.md)：本地进程、Docker Compose 和 Agones。
- [测试方案](guides/TESTING.md)：快速测试、竞态、集成与压测方法。
- [可重复演示](guides/DEMO.md)：五服务端到端验收流程和 JSON 报告。
- [求职展示与学习指南](guides/PORTFOLIO_GUIDE.md)：项目讲解、面试重点和简历示例。

## 质量证据

- [需求追踪矩阵](quality/REQUIREMENTS_TRACEABILITY.md)：每项需求对应的生产代码与自动化验证证据。
