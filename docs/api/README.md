# DraARL Server API 文档（客户端对接版）

> 随 DraARL Server 源码发布，版本以仓库根目录 `VERSION` 为准，基于当前代码实现整理
> （`internal/server/server.go` + `internal/handler/*` + `pkg/websocket/*`）。

## 文档目录

1. [00-约定与鉴权](./00-约定与鉴权.md)
2. [01-认证与SSO](./01-认证与SSO.md)
3. [02-用户与资料](./02-用户与资料.md)
4. [03-设备与配置](./03-设备与配置.md)
5. [04-群组与互联](./04-群组与互联.md)
6. [05-无线电与实时通信](./05-无线电与实时通信.md)
7. [06-通联记录与日志](./06-通联记录与日志.md)
8. [07-资源上传与站点配置](./07-资源上传与站点配置.md)
9. [08-运维与管理接口](./08-运维与管理接口.md)
10. [09-完整路由索引](./09-完整路由索引.md)
11. [10-WebSocket协议详解](./10-WebSocket协议详解.md)
12. [11-错误码与状态码](./11-错误码与状态码.md)
13. [12-客户端资源分发](./12-客户端资源分发.md)
14. [13-群组自动播报](./13-群组自动播报.md)

## 说明

- 本文档优先使用“推荐新路径”，同时标注兼容旧路径。
- 接口统一返回 `code/message/data`，但少量历史接口（AT 控制）返回业务码 `20000/20001`。
- WebSocket 鉴权使用 `HttpOnly Cookie(ws_token)`，不支持 URL 透传 token。
- 收发控制（`disable_send`/`disable_recv`）是设备级状态，由 `devices` 表统一维护；设备所有者/管理员使用 `PUT /api/devices/:id`，当前群主/管理员可使用群组设备收发控制接口。
- 群组不提供设备准入白名单或永久封禁；群主的临时收发控制在设备切换群组后仍然生效，设备所有者可自行恢复。
- 频道历史使用 `/api/groups/:id/messages` 的不透明游标；`/api/comm-records` 是本人发信/审计视图，不能替代频道历史。
- 完整路由索引按当前 `setupRoutes()` 登记，公开、JWT、Approved、Admin 和群主权限不会因路径前缀相似而互相替代。

## 自动生成的 OpenAPI

执行 `go run ./tools/openapi` 更新 [openapi.json](openapi.json)，`go run ./tools/openapi -check` 检查生成结果。CI 会检查过期文件。生成器分析实际 Gin 路由/分组/鉴权中间件、无线电路由注册、handler 注释、ShouldBindJSON 请求类型和 json/binding 标签，输出 OpenAPI 3.1。

目前没有完整响应类型推导，通用响应说明需结合本目录的人工接口文档与 handler 阅读；上传、业务条件校验、WS 二进制和 UDP 协议仍参阅专题文档。新增路由注册模式时补生成器测试，避免遗漏。handler 错误响应兼容 code/message 并补充稳定 error_code，5xx 不返回内部诊断。
