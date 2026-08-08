# 测试与模拟工具

本目录保存独立于生产二进制的设备模拟器和 UDP fan-out/中断恢复压测工具。Go 单元、集成与竞态测试仍分布在对应包的 `*_test.go` 中。

## Python 设备模拟器

`simulator/` 提供 Tk GUI，可同时模拟：

- UDP 普通设备：设备密码认证、心跳、文本、语音、动态绑定和 Type 3 配置。
- UDP 幽灵设备：版本化 JWT 认证、稳定安装实例、Session tag、单发多收和来源频道。
- 串口设备：通过串口收发 DraARLv1 数据。

从仓库根目录运行：

```bash
python -m pip install -r test/simulator/requirements.txt
python test/simulator/main.py
```

模拟器界面的服务地址、用户名、设备密码和 JWT 密钥必须按测试环境填写。不要在共享或
生产环境使用示例凭据。UDP 幽灵模拟器用于当前 Session 协议，不兼容旧 raw-JWT 认证。

## UDP fan-out benchmark

`bench/udp_fanout/` 是 Go 编写的真实 UDP socket 基准，可测试单中心、本地 fan-out、
中心/多边缘分布、纯跨边缘转发和带切组/禁收发/配置下发/控制链重连的 churn soak。

该工具会创建并删除数据库测试数据，运行前必须阅读
[bench/udp_fanout/README.md](bench/udp_fanout/README.md) 的安全约束，并显式传入
`-confirm-test-data`。不要指向未经备份的生产数据库。

## 项目测试

```bash
go test ./...
go test -race ./...
go vet ./...

cd www
npm run lint
npm run build
```

依赖真实 MySQL/S3 的契约测试通过环境变量选择性启用；命令和隔离要求见
`../docs/usage/01-部署与配置.md` 及相关测试源码。
