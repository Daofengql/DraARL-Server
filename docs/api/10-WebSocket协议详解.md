# WebSocket 协议详解

## 1. 端点与认证

浏览器幽灵设备通过 `/ws` 建立 WebSocket，生产环境使用 `wss://`。JWT 不放在 URL，也不再由客户端发送 Type 1 帧；客户端先调用 `POST /api/auth/ws-token/sync`，服务端从 HttpOnly `ws_token` Cookie 完成握手认证。

服务端执行 Origin 校验。认证失败在协议升级前返回 HTTP `401` 和稳定错误文本。

### 1.1 客户端参数

```text
wss://server.example/ws
  ?client_instance_id=41a065d7-f9e1-4785-bbcb-22c3ca8784ad
  &protocol_version=1
  &capabilities=multi_receive_v1,source_group_v1
```

| 参数 | 说明 |
|---|---|
| `client_instance_id` | 安装范围持久化的随机 UUID，不是硬件指纹 |
| `protocol_version` | 当前为 `1` |
| `multi_receive_v1` | 支持一个 Session 订阅多个接收频道 |
| `source_group_v1` | 能解析下行 DraARLv1 `Reserved` 中的来源频道 |

浏览器 API 无法设置握手自定义 Header，因此使用 query。非浏览器实现也可以提交 `X-DraARL-Client-Instance-ID`、`X-DraARL-Protocol-Version` 和 `X-DraARL-Capabilities`。

`client_instance_id`、协议版本和两项能力均为必需参数。缺少或无效时握手失败；不同安装实例在同一账号下可以多端共存，同一个安装实例重连时只替换自己的旧 Session。

## 2. 帧与控制事件

连接中有两类消息：

- 服务端文本帧：Session 和路由控制事件。
- 双向二进制帧：90 字节头部的 DraARLv1 心跳、文本和 Opus 媒体。

客户端不能把所有文本帧当成错误，也不能把控制 JSON 交给二进制解码器。

### 2.1 `auth_success`

Session 升级成功后，服务端首先发送：

```json
{
  "type": "auth_success",
  "data": {
    "session_id": "7fbbbd37-9207-4df2-b79d-e400e0a09fd0",
    "client_instance_id": "41a065d7-f9e1-4785-bbcb-22c3ca8784ad",
    "protocol_version": 1,
    "tx_group_id": 1001,
    "rx_group_ids": [1001, 1002]
  }
}
```

客户端收到并校验该事件后才进入 Online 状态，保存本连接的 `session_id`，并开始心跳。

### 2.2 `routing_updated`

API 更新、权限清理或服务端路由规范化后发送：

```json
{
  "type": "routing_updated",
  "data": {
    "session_id": "7fbbbd37-9207-4df2-b79d-e400e0a09fd0",
    "tx_group_id": 1002,
    "rx_group_ids": [1001, 1002, 1003]
  }
}
```

事件只对对应 Session 生效。客户端必须以服务端返回的完整数组替换本地路由，不能仅追加请求值。

## 3. DraARLv1 二进制消息

固定头部 90 字节，整数使用大端序。WebSocket 上行只处理：

| Type | 名称 | 方向 |
|---:|---|---|
| `2` | Heartbeat | 客户端到服务端，服务端返回心跳响应 |
| `4` | TextMessage | 双向 |
| `5` | Opus16K | 双向 |

上行目标频道不从帧中读取，始终取当前 Session 的 `tx_group_id`。下行 Type 4/5 的偏移 `86..89`（Reserved）为大端 `uint32 source_group_id`。

```javascript
const sourceGroupId = new DataView(buffer).getUint32(86, false)
```

同一消息可能从直接订阅和互联路径同时命中，但服务端按 Session 去重，只写一个 WebSocket 帧。发送源排除也按当前 Session，而不是用户名或 SSID，因此同账号的其他浏览器或手机仍可收到。

## 4. 单发与多收

切换路由使用 HTTP API，而不是在媒体帧内声明群组：

```http
PUT /api/radio/sessions/{session_id}/routing
Authorization: Bearer <access-token>
Content-Type: application/json

{
  "tx_group_id": 1002,
  "rx_group_ids": [1001, 1002, 1003]
}
```

`tx_group_id` 必须包含在最终 `rx_group_ids` 中。服务端会验证所有群组和权限，并以 `routing_updated` 同步最终状态。旧 `/api/radio/group` 已移除。

## 5. 语音与混音

Opus 参数为 16 kHz、单声道、60 ms 帧。Web 发送端可以把两个帧按 `uint16 length + frame` 格式合并，每约 120 ms 发送一次。

Web 接收端按以下 key 隔离解码状态：

```text
source_group_id : username/callsign : ssid
```

每个并发来源拥有独立 Opus 解码器和播放时间线，`MultiChannelAudioMixer` 在 AudioContext 中混合输出。全局音量、静音和每频道音量分别生效；取消订阅时清理该频道的解码流。延迟过大的帧会被丢弃以保持实时播放，不能拿一个全局 Opus 解码器交替解码不同频道。

通信域仍是半双工：同一群组或互联域只允许一个有效说话者；两个无关通信域可以同时出现语音并在 Web 端混音。

## 6. 心跳、断线与重连

- 客户端上线后每 25 秒发送 Type 2 心跳。
- 服务端每 30 秒发送 WebSocket Ping，客户端库自动回复 Pong。
- 断线只清理当前 `session_id`，不影响同账号其他 Session。
- 重连使用相同 `client_instance_id`，服务端签发新的临时 `session_id`，并加载该实例最后一次持久化路由。
- 页面主动退出可调用 `DELETE /api/radio/sessions/:session_id`；普通 WebSocket close 也只移除本连接。

## 7. 消息历史

实时二进制消息按 `source_group_id` 放入对应频道；历史同步使用：

```text
GET /api/groups/{group_id}/messages?limit=50&cursor=...
```

不要继续使用 `/api/comm-records?group_id=...` 作为聊天历史，也不要从 `device_name` 字符串拆分发送者。消息 API 返回结构化 sender、稳定游标和授权后的音频地址。

## 8. 安全约束

- `client_instance_id` 只用于区分安装实例，不能代替 JWT 或群组权限。
- `session_id` 只能操作当前账号自己的 Session。
- 每次 routing 更新都会重新校验群组 ACL；成员撤销后服务端会清理路由或断开 Session。
- 写队列有界，慢客户端不会阻塞其他 Session；队列溢出会记录丢弃指标。
- 不在日志中记录 `ws_token`、完整 JWT 或其他会话凭据。
