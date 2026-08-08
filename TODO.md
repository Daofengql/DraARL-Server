# 大文件拆分与模块边界重构 TODO

> 状态快照：2026-08-08。
>
> 固件 OTA 服务端兼容、`auth.go`、`group.go`、UDP Server、Logbook 和 SiteConfig 拆分已经完成并从本文件移除。剩余工作继续只降低单文件复杂度、明确职责边界，不改变 HTTP API、UDP/互联协议、数据库结构、权限规则、路由行为或页面交互。

## P0. 固件 OTA 真机验收

- [ ] 覆盖旧设备已有截断 OTA 缓存的迁移验收：切换代理模式后，设备重新检查能覆盖旧缓存，并验证 `dev_model=2` 从 `0.0.2` 升级到当前发布版本成功。

## 1. `internal/interconnect/gateway.go`

这是最高风险项，必须按职责域逐次移动并在每一步运行对应测试，禁止一次性重写。

### 1.1 CenterGateway

- [ ] 将设备配置请求、缓存、重试和完成处理移动到 `center_gateway_config.go`。
- [ ] 将设备/身份/Ghost 路由更新和域刷新移动到 `center_gateway_routing.go`。
- [ ] 将中心端 Relay 与 SpeakerLease 处理移动到 `center_gateway_relay.go`。

### 1.2 EdgeGateway

- [ ] 将边缘端结构、构造、控制链路和连接生命周期移动到 `edge_gateway.go`。
- [ ] 将认证、Session 续期、确认、过期和撤销移动到 `edge_gateway_session.go`。
- [ ] 将本地语音、SpeakerLease、fallback 和本地 fanout 移动到 `edge_gateway_voice.go`。
- [ ] 将设备配置上行/下行、缓存和结果处理移动到 `edge_gateway_config.go`。
- [ ] 将投影应用、接收计划缓存和路由确认/重同步移动到 `edge_gateway_routing.go`。
- [ ] 将下行写入、屏障队列、排空和地址辅助函数移动到 `edge_gateway_downstream.go`。

### 1.3 共同约束

- [ ] 共享类型只放入 `gateway_types.go`；不要创建同时操纵 Center 和 Edge 内部状态的万能工具文件。
- [ ] 保持所有互斥锁的获取顺序、channel 关闭顺序、goroutine 退出条件和原子变量语义不变。
- [ ] 覆盖控制链路中断恢复、Ghost Session 恢复窗口、Session owner 冲突、路由投影回滚、配置重试、SpeakerLease 和下行屏障。
- [ ] 对 `internal/interconnect` 运行完整测试、`-race`、fuzz smoke test 和 edge receiver benchmark。
- [ ] 执行中心/边缘接口模拟：断链、重连、重复控制包、乱序确认、过期 Session、虚拟互联变更和多收路由更新。

完成标准：Center 与 Edge 的状态机可分别阅读和测试，`gateway.go` 删除或只留下少量真正共享的入口代码。

## 2. 统一验收矩阵

- [ ] `gofmt` 不产生额外差异。
- [ ] `go test ./...` 通过。
- [ ] `go vet ./...` 通过。
- [ ] `go test -race ./internal/interconnect ./internal/udphub ./internal/handler` 通过；依赖 MySQL 的用例按现有环境策略执行或明确记录跳过原因。
- [ ] `go build ./cmd/draarl` 通过。
- [ ] `npm run lint`、`npm run build` 和 `npm test` 通过。
- [ ] `mkdocs build --strict` 通过。
- [ ] HTTP 路由总数、方法、路径和中间件顺序与拆分前基线一致。
- [ ] 数据库迁移结果与拆分前一致，不新增或删除表、列、索引和外键。
- [ ] Web 回归通联日志与系统设置页面，覆盖桌面和移动视口。
- [ ] 接口模拟回归标准设备单端、Ghost 多端、多收单发、中心/边缘切换和在线计数。

## 3. 剩余提交顺序

- [ ] 提交 7-N：按 CenterGateway、EdgeGateway 的职责域逐次拆分 `internal/interconnect/gateway.go`。
- [ ] 最终提交：只做无用 import、重复辅助函数、测试基线和文档路径清理，不再改变模块边界。

## 4. 本轮不做

- 不修改公开或内部协议版本。
- 不调整数据库模型或迁移。
- 不改变 Ghost/标准设备在线策略、PTT 仲裁或频道权限。
- 不引入新的全局状态管理框架、依赖注入框架或 Go package 分层。
- 不借拆分机会重做页面设计、API 命名、错误码或响应结构。
- 不以删除注释、压缩 JSX 或合并语句的方式追求行数指标。
