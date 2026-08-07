# DraARL Web

DraARL Server 的 React 前端，提供公共页面、用户控制台、浏览器在线收发和管理员后台。当前依赖 React 19、TypeScript 5.9、Vite 7、Material UI 7 和 React Router 7。

## 开发环境

- Node.js 20+
- npm（使用仓库中的 `package-lock.json`）
- 已在 `http://localhost:9002` 运行的 DraARL 后端

```bash
npm ci
npm run dev
```

开发服务器默认监听 `http://localhost:9001`，并将 `/api` 代理到
`http://localhost:9002`。WebSocket `/ws` 由前端按当前页面地址连接；联调时应确保
后端 `Web.FrontendURL` / `Web.AllowedOrigins` 包含 Vite 地址。

## 常用命令

```bash
npm run lint
npm run build
npm run preview
npm run audit
```

`npm run build` 会先执行 TypeScript project build，再由 Vite 输出到 `dist/`。
`VITE_APP_VERSION` 可在发布构建时注入；根目录 `build.sh`、`build.bat` 和 Release
workflow 会设置它并用 `embed` build tag 将产物嵌入 Go 二进制。

## 页面结构

| 区域 | 主要路径 | 说明 |
|---|---|---|
| 公共页面 | `/`、`/docs`、`/forum`、`/relays`、`/tools`、`/about` | 无需登录 |
| 认证 | `/login`、`/register`、`/forgot-password`、`/sso/callback` | 密码、邮箱验证码和 SSO |
| 用户控制台 | `/dashboard`、`/profile`、`/devices`、`/groups` | 登录后使用，设备/群组要求审核通过 |
| 实时通信 | `/radio` | WebSocket 幽灵 Session、单发多收、PTT、文本与频道历史 |
| 用户记录 | `/comm-records/platform`、`/comm-records/logbook` | 本人发信记录和 HAM 通联日志 |
| 管理后台 | `/admin/*` | 用户、设备、幽灵会话、群组、节点、资源、固件和站点配置 |

路由入口在 `src/App.tsx`，用户导航在 `src/components/layout/Sidebar.tsx`，管理员导航在
`src/components/layout/AdminLayout.tsx`。HTTP 服务封装位于 `src/services/`，实时通信实现位于
`src/services/radio/`。

## 实时通信约束

- `/ws` 使用 `HttpOnly ws_token` Cookie，不把 token 放入 URL。
- 浏览器持久化随机 `client_instance_id`，握手声明 `protocol_version=1`、
  `multi_receive_v1` 和 `source_group_v1`。
- 每个在线 Session 有一个发送频道和多个接收频道；路由更新使用
  `PUT /api/radio/sessions/:session_id/routing`。
- 下行 Type 4/5 的 DraARLv1 `Reserved` 是大端 `source_group_id`；不同来源流必须使用
  独立 Opus 解码状态。

完整协议见 `../docs/api/10-WebSocket协议详解.md`，前后端 API 契约见
`../docs/api/README.md`。
