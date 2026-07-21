<!-- markdownlint-disable MD013 MD060 -->

# UDP fan-out benchmark

`udp_fanout` 使用真实 DraARLv1 心跳认证和 UDP socket，测量单机服务在大量在线设备下的语音 fan-out 能力。工具支持一个或多个独立群组，并为每个群组安排一个同时发言的设备。

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
| `-server-pid` | 无 | DraARL Server PID，正式测试必填 |
| `-levels` | `100,500,1000,2000,4000` | 递增的总客户端数，最大 20000 |
| `-groups` | `1` | 独立群组数，也是同时发言者数量 |
| `-duration` | `10s` | 每档测量时间 |
| `-interval` | `120ms` | 每个发言者的语音包间隔 |
| `-payload` | `320` | 语音 DATA 长度，完整包长另加 90 字节 |
| `-settle` | `3s` | 新增客户端后的稳定等待时间 |
| `-cleanup-only` | `false` | 仅清理遗留数据，不运行测试 |

## 输出字段

- `expected/received/loss_pct`：理论下行包数、实收包数和丢包率。
- `output_pps/output_mbps`：服务端实际下行包速和 DraARLv1 应用层带宽。
- `latency_*`：数据包发送时间到本地接收时间，不含真实网络 RTT。
- `server_cpu_cores`：服务进程全部 OS 线程消耗的 CPU 核数，例如 `1.20` 表示约 1.2 个逻辑核。
- `server_rss_mb`：服务进程 RSS。

工具在丢包率超过 5% 后停止更高档位，避免继续制造不具代表性的过载流量。
