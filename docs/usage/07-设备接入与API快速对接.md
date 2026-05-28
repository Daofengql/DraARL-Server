# 07. 设备接入与 API 快速对接

## 1. API 基础约定

| 项目 | 值 |
|---|---|
| HTTP 前缀 | `/api` |
| WebSocket 地址 | `/ws` |
| JSON Content-Type | `application/json` |
| 文件上传 | `multipart/form-data` |
| 常见时间格式 | `2006-01-02 15:04:05` |

详细接口见 [API 文档总览](../api/README.md) 和 [完整路由索引](../api/09-完整路由索引.md)。

## 2. Access Token

受保护接口使用：

```http
Authorization: Bearer <access_token>
```

登录成功会返回 `token`、`refresh_token`、`expires_in`、`refresh_expires_in` 和 `user`。access token 默认有效期为 3 小时。

## 3. Refresh Token

浏览器推荐使用 HttpOnly Cookie：

- Cookie 名：`refresh_token`
- Path：`/api/auth`
- 过期：14 天

刷新接口：

```http
POST /api/auth/refresh
```

非浏览器客户端也可在 body 中传递 `refresh_token`。

## 4. 统一响应

大部分接口返回：

```json
{
  "code": 200,
  "message": "成功",
  "data": {}
}
```

常见错误：

```json
{ "code": 400, "message": "请求参数错误" }
{ "code": 401, "message": "未授权" }
{ "code": 403, "message": "需要管理员权限" }
{ "code": 404, "message": "资源不存在" }
{ "code": 429, "message": "请求过于频繁，请稍后重试", "data": { "retry_after": 9 } }
{ "code": 500, "message": "服务器内部错误" }
```

历史 AT 控制接口可能返回业务码 `20000/20001`。

## 5. 分页

常见请求参数：

- `page`
- `page_size`
- `limit`

常见返回字段：

- `total`
- `items` 或 `list`
- `page`
- `page_size`

## 6. 动态码设备绑定 API

设备端请求动态码：

```http
POST /api/device/request-code
Content-Type: application/json
```

```json
{
  "mac": "AA:BB:CC:DD:EE:FF"
}
```

Web 端绑定：

```http
POST /api/device/bind
Authorization: Bearer <access_token>
Content-Type: application/json
```

```json
{
  "dynamic_code": "735921"
}
```

Web 端提交配置：

```http
POST /api/device/submit-config
Authorization: Bearer <access_token>
Content-Type: application/json
```

```json
{
  "device_mac": "AA:BB:CC:DD:EE:FF",
  "ssid": 11
}
```

## 7. DraARLv1 UDP 协议摘要

协议核心：

- 协议标识：`DraA`
- 固定头长度：90 字节
- 最大包长：800 字节
- 字节序：大端序
- DMR ID 为 3 字节 uint24
- 心跳 GPS 固定区为 24 字节
- 心跳如需上报 MAC，只能追加到 `DATA[24:]`，不能写入 header

包类型：

| Type | 说明 |
|---:|---|
| `0` | 控制指令 |
| `1` | JWT 认证包 |
| `2` | 心跳包 |
| `3` | 设备配置 |
| `4` | 文本消息 |
| `5` | Opus 16K 语音 |
| `6` | 服务器互联语音 |
| `7` | AT 透传 |

更完整协议说明见：[DraARLv1 协议文档](../Protocol.md)。

## 8. WebSocket 接入

WebSocket 入口：

```text
/ws
```

接入要求：

- 必须携带 `ws_token` HttpOnly Cookie。
- 需要先通过 `/api/auth/ws-token/sync` 同步 Cookie。
- 不支持 URL query 传 token。
- 消息体为 DraARLv1 二进制帧。
- 同账号同平台幽灵设备只允许一个在线连接。

示例：

```javascript
const ws = new WebSocket('wss://server.example.com/ws');
ws.binaryType = 'arraybuffer';
```

## 9. 限流

| 路径 | 限流说明 |
|---|---|
| `/api/device/pre-check` | IP 每秒 1 次；MAC 每分钟 5 次。 |
| `/api/device/request-code` | IP 每 10 秒 1 次；MAC 每分钟 1 次。 |
| `/api/device/confirm-bind` | MAC 每 5 秒 1 次。 |
| `/api/device/bind` | 用户每分钟 5 次。 |
| `/api/device/submit-config` | 用户每分钟 10 次。 |
| `/api/public/relays` | IP 每分钟 10 次。 |

## 10. 关键对接接口

| Method | Path | Auth | 说明 |
|---|---|---|---|
| POST | `/api/auth/login` | Public | 账号密码登录。 |
| POST | `/api/auth/refresh` | Public | 刷新 access token。 |
| POST | `/api/auth/ws-token/sync` | JWT | 同步 `ws_token` Cookie。 |
| POST | `/api/device/pre-check` | Public | 设备预检查。 |
| POST | `/api/device/request-code` | Public | 请求动态码。 |
| POST | `/api/device/confirm-bind` | Public | 轮询绑定状态。 |
| GET | `/api/devices/:id/config` | JWT+Approved | 读取设备配置。 |
| POST | `/api/devices/:id/config/sync` | JWT+Approved | 下发设备配置。 |
| GET | `/api/public/firmware/latest` | Public | 查询指定型号最新固件。 |
| GET | `/ws` | Cookie(ws_token) | WebSocket 实时通联。 |

