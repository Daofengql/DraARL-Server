# DraARL Server

[![Release](https://img.shields.io/github/v/release/Daofengql/DraARL-Server?include_prereleases&label=release)](https://github.com/Daofengql/DraARL-Server/releases)
[![Release Build](https://github.com/Daofengql/DraARL-Server/actions/workflows/release.yml/badge.svg)](https://github.com/Daofengql/DraARL-Server/actions/workflows/release.yml)
[![Docs Deploy](https://github.com/Daofengql/DraARL-Server/actions/workflows/docs-pages.yml/badge.svg)](https://github.com/Daofengql/DraARL-Server/actions/workflows/docs-pages.yml)
[![License: PolyForm Noncommercial 1.0.0](https://img.shields.io/badge/License-PolyForm%20Noncommercial%201.0.0-blue.svg)](LICENSE)

**Digital Radio Advanced Application Real-time Link**<br>
面向业余无线电和自研数字电台设备的实时通信、设备管理、用户审核和运维管理平台。

DraARL Server 使用 Go 提供 HTTP API、WebSocket 在线收发和 UDP DraARLv1 设备接入服务，前端使用 React + TypeScript + Material UI 提供公共页面、用户控制台和管理员后台。项目已经包含完整的 API 文档、协议文档、使用说明、架构图和数据字典。

## 快速链接

| 类型 | 链接 |
|------|------|
| 在线站点 | [https://ptt.4l2.cn](https://ptt.4l2.cn) |
| 在线内置文档 | [https://ptt.4l2.cn/docs](https://ptt.4l2.cn/docs) |
| GitHub 仓库 | [Daofengql/DraARL-Server](https://github.com/Daofengql/DraARL-Server) |
| 最新发布 | [GitHub Releases](https://github.com/Daofengql/DraARL-Server/releases) |
| 最新 tag | [v1.1.4-alpha6](https://github.com/Daofengql/DraARL-Server/tree/v1.1.4-alpha6) |
| 问题反馈 | [Issues](https://github.com/Daofengql/DraARL-Server/issues) |
| 构建发布 | [Release workflow](https://github.com/Daofengql/DraARL-Server/actions/workflows/release.yml) |
| 文档发布 | [Docs Deploy workflow](https://github.com/Daofengql/DraARL-Server/actions/workflows/docs-pages.yml) |

## 文档入口

| 文档 | 说明 |
|------|------|
| [文档站首页](docs/index.md) | 推荐阅读顺序、文档目录、本地预览与自动发布说明 |
| [架构设计](docs/架构设计.md) | 系统模块、数据流、部署形态和核心链路 |
| [数据字典](docs/数据字典.md) | 数据库表、字段、索引和关系说明 |
| [使用与说明文档](docs/usage/README.md) | 部署、账号、设备、群组、在线收发、后台、运维等功能说明 |
| [设备接入指南](docs/usage/07-设备接入与API快速对接.md) | 设备绑定、认证、上报、配置同步和 API 快速对接 |
| [固件与 OTA](docs/usage/09-固件与OTA升级.md) | 固件发布、版本规则、OTA 查询与下载流程 |
| [APRS 与位置服务](docs/usage/10-APRS与位置服务.md) | APRS 配置、位置上报和地图展示说明 |
| [DraARLv1 协议](docs/Protocol.md) | UDP 设备协议、报文结构、状态码和认证流程 |
| [API 文档](docs/api/README.md) | HTTP API、WebSocket、错误码与完整路由索引 |

## 核心能力

- **实时语音与文本通信**：支持 UDP DraARLv1 设备、WebSocket 浏览器幽灵设备、PTT 控制、Opus 语音、半双工群组通信和通信记录。
- **设备接入与管理**：支持设备密码、JWT、动态码绑定、设备 SSID、设备型号上报、远程参数配置、设备禁发/禁收、AT 控制和 SA818 频率配置。
- **群组与互联**：支持公开群组、私有群组、群组成员管理、设备切组、虚拟互联组和跨群组语音转发。
- **账号与审核**：支持账号密码、邮箱验证码、Keycloak SSO、JWT/refresh token、注册审核、操作证上传与审核。
- **管理后台**：提供用户、设备、群组、中继台、服务器、资源中心、固件发布、站点配置、SMTP、APRS、OpenAI、缓存指标和操作日志管理。
- **通联与记录**：支持平台发信记录、个人通联日志、统计趋势、音频存储和管理员侧全局查询。
- **资源与发布**：支持 MinIO 对象存储、前端资源嵌入或 CDN 托管、GitHub Actions 多平台 Release，以及 MkDocs 文档自动发布到 EdgeOne Pages。

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端 | Go 1.25、Gin、GORM、Gorilla WebSocket |
| 数据 | MySQL/MariaDB、Redis、MinIO |
| 前端 | React 19、TypeScript 5.9、Vite 7、Material UI 7、React Router 7 |
| 通信 | UDP DraARLv1、WebSocket、Opus、APRS |
| 文档 | MkDocs、MkDocs Material |
| 自动化 | GitHub Actions、EdgeOne Pages |

## 项目结构

```text
DraARL-Server/
├── cmd/udphub/              # 服务入口，启动 UDP、HTTP、APRS、缓存和日志等模块
├── internal/
│   ├── aprs/                # APRS 连接、配置和日志
│   ├── auth/                # refresh token 存储，支持 Redis 与内存降级
│   ├── captcha/             # 图形验证码
│   ├── config/              # YAML 配置、默认值、Origin 校验
│   ├── db/                  # 兼容旧逻辑的原生 SQL 数据访问
│   ├── gormdb/              # GORM 模型、仓储和自动迁移
│   ├── handler/             # HTTP API 处理器
│   ├── middleware/          # 登录、审核、管理员、群组权限和限流中间件
│   ├── protocol/            # DraARLv1 协议编解码和设备字段校验
│   ├── server/              # Gin 路由、WebSocket、前端静态资源服务
│   └── udphub/              # UDP 设备运行态、语音转发、群组互联和通信记录
├── pkg/                     # JWT、缓存、MinIO、WebSocket、加密、GeoIP 等公共包
├── www/                     # React 前端项目
├── docs/                    # MkDocs 文档站、API 文档、协议文档和图表资源
├── test/                    # Python 设备/协议测试工具
├── .github/workflows/       # Release 与文档发布工作流
├── udphub.yaml.example      # 配置模板
├── Makefile                 # 常用构建、测试和运行命令
└── README.md
```

## 环境要求

- Go 1.25+
- Node.js 20+
- MySQL 5.7+ 或 MariaDB 10.3+
- Redis 6.0+（推荐；不可用时 refresh token 会降级到内存存储）
- MinIO（可选，用于资源、头像、通信录音、固件和前端 CDN）
- Keycloak（可选，用于 SSO）
- Python 3.11+（仅本地预览/构建 MkDocs 文档时需要）

## 快速开始

### 1. 克隆仓库

```bash
git clone https://github.com/Daofengql/DraARL-Server.git
cd DraARL-Server
```

### 2. 准备数据库

```sql
CREATE DATABASE draarl CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

### 3. 准备配置文件

```bash
cp udphub.yaml.example udphub.yaml
```

至少需要检查以下配置：

- `Database`：MySQL/MariaDB 连接信息。
- `Redis`：refresh token 存储配置，生产环境建议启用。
- `Web.Port`：后端 HTTP API 和前端静态资源端口。
- `Web.FrontendURL` 与 `Web.AllowedOrigins`：登录回调、CORS 和 WebSocket Origin 白名单。
- `JWT.Secret`：至少 32 字符；不符合要求时程序会自动生成并写回配置。
- `DeviceAuth.AESKey`：16、24 或 32 字节；留空时程序会自动生成并写回配置。
- `MinIO`：如需资源中心、头像、固件、通信录音或前端 CDN，需要配置。

### 4. 安装依赖并构建前端

```bash
go mod download

cd www
npm ci
npm run build
cd ..
```

### 5. 初始化数据库并启动

首次部署或表结构变更后执行自动迁移：

```bash
go run ./cmd/udphub -c udphub.yaml -auto-migrate
```

后续正常启动：

```bash
go run ./cmd/udphub -c udphub.yaml
```

首次启动会自动创建管理员用户，并在控制台输出初始用户名和密码。登录后请立即修改密码。

## 本地开发

后端：

```bash
go run ./cmd/udphub -c udphub.yaml
```

前端：

```bash
cd www
npm run dev
```

Vite 开发服务器默认监听 `9001`，并把 `/api` 代理到 `http://localhost:9002`。如果本地 `Web.Port` 不是 `9002`，请同步调整 `www/vite.config.ts` 中的代理目标，或把本地后端端口设为 `9002`。

常用开发命令：

```bash
go test ./...
go fmt ./...
go vet ./...

cd www
npm run lint
npm run build
```

## 构建与部署

### API 模式构建

默认构建不嵌入前端资源，适合 API 服务或本地开发：

```bash
go build -o draarl ./cmd/udphub
./draarl -c udphub.yaml
```

### 嵌入前端构建

Release 工作流使用 `embed` 构建标签，将 `www/dist` 放入 `internal/server/web/dist` 后再编译：

```bash
cd www
npm ci
npm run build
cd ..

mkdir -p internal/server/web/dist
cp -r www/dist/* internal/server/web/dist/
go build -tags=embed -o draarl ./cmd/udphub
```

Windows PowerShell 可将复制步骤替换为：

```powershell
New-Item -ItemType Directory -Force internal/server/web/dist
Copy-Item -Recurse -Force www/dist/* internal/server/web/dist/
go build -tags=embed -o draarl.exe ./cmd/udphub
```

### Makefile

```bash
make help
make build
make test
make run
```

注意：当前 `Makefile` 的 `build/run` 目标是普通 Go 构建，不会自动执行前端构建，也不会加 `-tags=embed`。

### 发布流程

当前仓库的发布由 GitHub Actions 驱动：

1. 推送形如 `v*.*.*` 的 tag。
2. [Release workflow](https://github.com/Daofengql/DraARL-Server/actions/workflows/release.yml) 构建 Linux、Windows、macOS 的 amd64/arm64 产物并创建 GitHub Release。
3. Release 成功后，[Docs Deploy workflow](https://github.com/Daofengql/DraARL-Server/actions/workflows/docs-pages.yml) 自动构建 MkDocs 并发布到 EdgeOne Pages。

示例：

```bash
git tag -a v1.1.4-alpha7 -m "release: v1.1.4-alpha7"
git push origin v1.1.4-alpha7
```

## 服务端口与入口

| 类型 | 默认/常用端口 | 说明 |
|------|---------------|------|
| UDP 设备接入 | `60050` | DraARLv1 设备接入和语音转发 |
| HTTP API / Web | `9000` 或本地开发常用 `9002` | Gin API、WebSocket、前端静态资源 |
| Vite 开发服务器 | `9001` | 前端开发预览，代理 `/api` 到后端 |
| WebSocket | `/ws` | 浏览器在线收发连接入口 |
| API 前缀 | `/api` | HTTP API 统一前缀 |

## 管理与运维

常用命令：

```bash
# 查看版本
./draarl -v

# 打印关键配置
./draarl -c udphub.yaml -p json

# 重置管理员密码
./draarl -c udphub.yaml -reset-admin-pass "new-password"

# 执行数据库自动迁移
./draarl -c udphub.yaml -auto-migrate
```

生产环境建议：

- 固定并备份 `udphub.yaml` 中的 `JWT.Secret` 和 `DeviceAuth.AESKey`。
- 配置真实的 `Web.FrontendURL` 和 `Web.AllowedOrigins`，避免 Release 模式下 Origin 校验失败。
- 使用 Redis 保存 refresh token，避免进程重启导致登录态丢失。
- 使用 MinIO 保存头像、操作证、资源文件、通信录音和固件。
- 对外暴露 UDP 服务时，确认防火墙和反向代理的真实 IP 传递策略；如使用 frp 等代理，可按需开启 `System.ProxyProtocol`。
- 首次上线后立即修改默认管理员密码，并检查注册审核、操作证审核和设备绑定策略。

更多排障内容请阅读 [运维与排障](docs/usage/08-运维与排障.md)。

## API 与协议

README 不再维护大段接口清单，避免与实际路由漂移。请以文档站为准：

- [API 总览](docs/api/README.md)
- [完整路由索引](docs/api/09-完整路由索引.md)
- [WebSocket 协议详解](docs/api/10-WebSocket协议详解.md)
- [错误码与状态码](docs/api/11-错误码与状态码.md)
- [DraARLv1 协议文档](docs/Protocol.md)

## 文档预览

```bash
pip install -r docs/requirements.txt
mkdocs serve -f docs/mkdocs.yml
```

访问 `http://127.0.0.1:8000` 预览文档站。

静态构建：

```bash
mkdocs build -f docs/mkdocs.yml --strict
```

## 分支与提交

- `master`：主分支和发布来源。
- `dev`：开发分支。
- `v*.*.*` tag：触发 Release 和后续文档发布。

提交信息建议使用：

```text
feat: 新功能
fix: 修复问题
docs: 文档更新
refactor: 代码重构
test: 测试相关
chore: 构建或工具调整
```

## 许可证

本项目使用 [PolyForm Noncommercial License 1.0.0](LICENSE) 授权，并随附 [NOTICE](NOTICE) 中的 `Required Notice:` 声明。

- 允许非商业目的下的学习、研究、运行、修改、二次开发和再分发。
- 未经作者明确书面授权，不允许商业使用、收费销售、收费 SaaS、商业托管或商业集成。
- 二次开发、fork、复制、再分发或派生作品必须保留原始仓库地址：`https://github.com/Daofengql/DraARL-Server`。
- 中文说明请阅读 [LICENSE.zh-CN.md](LICENSE.zh-CN.md)，正式法律文本以英文 [LICENSE](LICENSE) 为准。
