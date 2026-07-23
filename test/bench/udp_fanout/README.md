<!-- markdownlint-disable MD013 MD060 -->

# UDP fan-out benchmark

`udp_fanout` 使用真实 DraARLv1 心跳认证和 UDP socket，测量单机或中心/多边缘拓扑在大量在线设备下的语音 fan-out 能力。工具支持一个或多个独立群组，并为每个群组安排一个同时发言的设备。

历史实测结果和瓶颈分析见[UDP 单机转发压力测试](../../../docs/UDP单机转发压力测试.md)。

## 安全约束

> [!WARNING]
> 工具会连接 `config.yaml` 指向的 MySQL，创建并删除带 `__draarl_udp_bench_`
> 前缀的用户、设备、群组和通信记录。不要对未经备份的生产数据库运行。

- 必须显式传入 `-confirm-test-data`。
- 同一数据库一次只能运行一个实例，工具通过 MySQL advisory lock 阻止并发执行。
- 启动时先清理同前缀的遗留测试数据，结束时再次清理。
- 工具固定使用 Type 5 普通语音路径；仅允许在本地存储驱动下使用，以便准确删除可能生成的测试录音。只有站点通信录制设置 `comm.enabled=true` 时才会实际落盘，关闭时录音清理数为 0 属正常结果。
- 中断进程可能来不及执行清理；重新运行时使用 `-cleanup-only`。
- 每个模拟设备绑定不同的 `127.x.x.x` 回环地址，使服务端的同 IP DDoS 限速不会掩盖转发表性能。

## 前置条件

1. MySQL 和 DraARL Server 已启动。
2. 当前目录存在与服务端一致的 `config.yaml`。
3. 已取得 DraARL Server 进程 PID。
4. Windows 或 Linux 环境；进程 CPU/RSS 指标分别通过系统 API 和 `/proc` 读取。

## 运行

单群组纯转发测试：

```bash
go run ./test/bench/udp_fanout -confirm-test-data -server-pid 11396 -levels 1000,2000,4000,5000 -duration 10s -interval 120ms
```

5 个独立群组，每组 1000 台设备、5 个发言者同时发送普通语音：

```bash
go run ./test/bench/udp_fanout -confirm-test-data -server-pid 11396 -groups 5 -levels 5000 -duration 15s -interval 120ms
```

真实中心和三个边缘的混合本地/跨节点转发：

```bash
./udp-fanout-bench -confirm-test-data -config center.yaml \
  -servers 127.0.0.1:62051,127.0.0.1:62052,127.0.0.1:62053 \
  -placement distributed -groups 5 -levels 5000 -duration 15s \
  -process-pids center=11094,edge-a=11208,edge-b=11219,edge-c=11230
```

纯跨节点转发（每组发言者在第一个边缘，所有接收者在其他边缘）：

```bash
./udp-fanout-bench -confirm-test-data -config center.yaml \
  -servers 127.0.0.1:62051,127.0.0.1:62052,127.0.0.1:62053 \
  -placement cross -groups 5 -levels 5000 -duration 15s \
  -process-pids center=11094,edge-a=11208,edge-b=11219,edge-c=11230
```

30 分钟混合 churn soak（持续 Type 2/4/5，并循环验证切组、禁收发、Type 3
配置下发、设备跨边缘漫游和 TLS 控制连接重连）：

```bash
./udp-fanout-bench -confirm-test-data -churn -config center.yaml \
  -api-base http://127.0.0.1:60051/api \
  -servers 127.0.0.1:62051,127.0.0.1:62052,127.0.0.1:62053 \
  -placement distributed -groups 5 -levels 1000 -duration 30m \
  -interval 120ms -churn-interval 5s -edge-reset-interval 2m \
  -process-pids center=11094,edge-a=11208,edge-b=11219,edge-c=11230
```

churn 模式要求至少两个群组、两个 UDP 边缘和两个在线边缘控制会话。工具用
`config.yaml` 的 JWT 密钥为临时管理员测试用户签发仅在本进程内使用的令牌，
所有状态变更均调用正式 HTTP API；令牌和密码不会写入输出。退出时会先恢复测试
设备的群组与收发状态，再执行统一的数据及录音清理。

同一批 20000 个会话逐档提高发送频率，寻找丢包拐点：

```bash
./udp-fanout-bench -confirm-test-data -config center.yaml \
  -servers 127.0.0.1:62051,127.0.0.1:62052,127.0.0.1:62053 \
  -placement cross -groups 5 -levels 20000 -duration 10s \
  -intervals 120ms,60ms,30ms,20ms,10ms -process-pids center=11094,edge-a=11208,edge-b=11219,edge-c=11230
```

仅清理遗留测试数据：

```bash
go run ./test/bench/udp_fanout -confirm-test-data -cleanup-only
```

也可以构建独立命令：

```bash
go build -o ./bin/udp-fanout-bench ./test/bench/udp_fanout
```

## 主要参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-confirm-test-data` | `false` | 确认允许创建和删除临时 MySQL 数据，必填 |
| `-config` | `config.yaml` | 服务端配置文件 |
| `-server` | `127.0.0.1:60050` | UDP 服务地址 |
| `-servers` | 空 | 多个 UDP 边缘入口，逗号分隔；设置后覆盖 `-server` |
| `-api-base` | 空 | HTTP API 根地址（含 `/api`），`-churn` 时必填 |
| `-server-pid` | 无 | DraARL Server PID，正式测试必填 |
| `-process-pids` | 空 | 多进程测量目标，例如 `center=100,edge-a=101`；设置后替代 `-server-pid` |
| `-levels` | `100,500,1000,2000,4000` | 递增的总客户端数，最大 20000 |
| `-groups` | `1` | 独立群组数，也是同时发言者数量，最大 1024 |
| `-placement` | `local` | `local` 全部设备在首入口；`distributed` 同组设备轮转各入口；`cross` 发言者在首入口且接收者只在其他入口 |
| `-churn` | `false` | 运行混合状态变化 soak，替代静态 fan-out 档位 |
| `-churn-interval` | `5s` | 切组、禁收发、配置下发和漫游操作间隔 |
| `-edge-reset-interval` | `2m` | 强制重置边缘 TLS 控制连接的间隔；`0` 可仅在诊断时禁用 |
| `-duration` | `10s` | 每档测量时间 |
| `-interval` | `120ms` | 每个发言者的语音包间隔 |
| `-intervals` | 空 | 由慢到快扫描多个包间隔，复用同一批已认证会话；设置后覆盖 `-interval` |
| `-payload` | `320` | 语音 DATA 长度，完整包长另加 90 字节 |
| `-settle` | `3s` | 新增客户端后的稳定等待时间 |
| `-cleanup-only` | `false` | 仅清理遗留数据，不运行测试 |

## 输出字段

- `expected/received/loss_pct`：理论下行包数、实收包数和丢包率。
- `output_pps/output_mbps`：服务端实际下行包速和 DraARLv1 应用层带宽。
- `latency_*`：数据包发送时间到本地接收时间，不含真实网络 RTT。
- `server_cpu_cores`：服务进程全部 OS 线程消耗的 CPU 核数，例如 `1.20` 表示约 1.2 个逻辑核。
- `server_rss_mb`：服务进程 RSS。
- `proc_<name>_cpu_cores/proc_<name>_rss_mb`：使用 `-process-pids` 时各中心/边缘进程的独立资源占用；`server_*` 为它们的合计。
- `CHURN_RESULT`：混合 soak 的操作覆盖数、Type 3/4/5 实收数、边缘保护队列增量、
  goroutine 起止值、接收者缓存命中/重建增量和各进程 CPU/RSS。若必要操作未覆盖、
  控制队列未排空、边缘未重连或出现队列保护丢弃，命令以非零状态退出。

工具在丢包率超过 5% 后停止更高档位，避免继续制造不具代表性的过载流量。
