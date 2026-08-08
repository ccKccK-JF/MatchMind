# API 契约目录

版本化内部 API 的唯一事实来源位于 `proto/matchmind`，生成的 Go 客户端和服务端位于
`gen/go/matchmind`，公共 REST 映射位于 `internal/api/transport/http`。

本目录作为交付结构中的 API 入口索引，不复制 Protobuf 或 HTTP 文档，避免出现两份
契约。公共接口示例见 [`docs/guides/API.md`](../docs/guides/API.md)。
