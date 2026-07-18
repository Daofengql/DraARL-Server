# 01. 认证与 SSO

## 1. 账号密码登录

### `POST /api/auth/login`

功能：账号/邮箱 + 密码登录，签发 access/refresh token。

请求示例：

```json
{
  "username": "alice",
  "password": "P@ssw0rd"
}
```

返回示例：

```json
{
  "code": 200,
  "message": "登录成功",
  "data": {
    "token": "<access_token>",
    "refresh_token": "<refresh_token>",
    "expires_in": 10800,
    "refresh_expires_in": 1209600,
    "user": {
      "id": 12,
      "username": "alice",
      "nickname": "Alice",
      "callsign": "BG7XXX",
      "roles": ["user"],
      "approval_status": 1,
      "last_group_id": 999
    }
  }
}
```

## 2. 登出

### `POST /api/auth/logout`

功能：注销 refresh token，并清理 `refresh_token`/`ws_token` Cookie。

返回示例：

```json
{ "code": 200, "message": "登出成功" }
```

## 3. 刷新登录态

### `POST /api/auth/refresh`

功能：刷新 access token（并轮换 refresh token）。

请求示例（非 Cookie 客户端）：

```json
{ "refresh_token": "<refresh_token>" }
```

返回示例：

```json
{
  "code": 200,
  "message": "成功",
  "data": {
    "token": "<new_access_token>",
    "expires_in": 10800,
    "refresh_token": "<new_refresh_token>",
    "refresh_expires_in": 1209600
  }
}
```

## 4. 注册

### `POST /api/auth/register`

功能：邮箱验证码注册（注册后进入待审核状态）。

请求示例：

```json
{
  "username": "alice",
  "password": "P@ssw0rd",
  "callsign": "BG7XXX",
  "email": "alice@example.com",
  "nickname": "Alice",
  "session_id": "email_session_xxx",
  "email_code": "123456"
}
```

返回示例：

```json
{
  "code": 201,
  "message": "注册成功，请等待管理员审核",
  "data": {
    "id": 12,
    "username": "alice",
    "approval_status": 0,
    "device_password": "Ab12Cd34"
  }
}
```

## 5. 邮箱验证码

### `POST /api/auth/send-code`

功能：发送邮箱验证码。用途 `purpose` 支持：

- `register`
- `login`
- `reset_password`
- `change_email`

请求示例：

```json
{
  "email": "alice@example.com",
  "purpose": "register",
  "captcha_id": "cpt_abc",
  "captcha_code": "7k3p"
}
```

返回示例：

```json
{
  "code": 200,
  "message": "验证码已发送",
  "data": {
    "session_id": "email_session_xxx",
    "expires_in": 600
  }
}
```

### `POST /api/auth/verify-email`

功能：注册流程中验证邮箱验证码。

请求：

```json
{ "session_id": "email_session_xxx", "code": "123456" }
```

返回：

```json
{ "code": 200, "message": "邮箱验证成功", "data": { "email": "alice@example.com", "session_id": "email_session_xxx" } }
```

## 6. 邮箱登录与重置密码

### `POST /api/auth/email-login`

请求：

```json
{ "session_id": "email_session_xxx", "code": "123456" }
```

返回：同 `login`，含 `token/refresh_token/user`。

### `POST /api/auth/reset-password`

请求：

```json
{
  "session_id": "email_session_xxx",
  "code": "123456",
  "new_password": "NewPass123"
}
```

返回：

```json
{ "code": 200, "message": "密码重置成功" }
```

## 7. WebSocket Token Cookie

### `POST /api/auth/ws-token/sync`（JWT）

功能：从 `Authorization` 同步 `ws_token` Cookie。

返回：

```json
{ "code": 200, "message": "成功" }
```

### `POST /api/auth/ws-token/clear`

功能：清理 `ws_token`（并清理 `refresh_token`）。

返回：

```json
{ "code": 200, "message": "成功" }
```

## 8. SSO（Keycloak）

### `GET /api/sso/login`

功能：获取 SSO 登录 URL。

返回：

```json
{ "code": 200, "message": "成功", "data": { "url": "https://.../auth?..." } }
```

### `GET /api/sso/callback`

功能：Keycloak 回调入口（服务端处理并重定向前端）。

### `POST /api/sso/exchange`

功能：用一次性交换码换取 token（避免 URL 透传 token）。

请求：

```json
{ "code": "one_time_sso_code" }
```

返回：同 `login`。

### `GET /api/sso/status`（JWT）

返回示例：

```json
{ "code": 200, "message": "成功", "data": { "bound": true, "keycloak_id": "xxxx-xxxx" } }
```

### `POST /api/sso/bind`（JWT）

功能：发起当前用户绑定 SSO。

### `DELETE /api/sso/unbind`（JWT）

功能：解绑当前用户 SSO。

## 9. 管理员切换登录用户

### `POST /api/auth/switch-login/:id`（Admin）

管理员在后台直接切换为路径指定的目标用户登录。请求不需要目标用户密码；后端按目标
用户本人的用户名和角色签发一套普通 access/refresh token，并覆盖浏览器的
`refresh_token` 与 `ws_token` Cookie。

这不是模拟登录：响应和普通登录相同，前端会直接用返回的 `token` 和 `user`
覆盖当前管理员会话。切换成功后不再保留管理员权限，也没有一键返回入口；如需返回管理员
账号，必须正常退出并重新登录。

约束：

- 只有当前登录管理员可以调用，普通用户返回 403。
- 目标用户必须存在、启用，且不能是当前账号。
- 主管理员（ID 1）不能作为切换目标，避免借身份切换取得主管理员特权。
- 成功时吊销当前浏览器携带的管理员 refresh token，不影响管理员在其他设备上的会话。
- 操作写入 `admin_switch_login` 审计日志，记录管理员和目标用户。
- 响应使用 `Cache-Control: no-store`。

返回：同 `login`，含目标用户的 `token/refresh_token/user`。
