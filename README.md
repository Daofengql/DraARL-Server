# DraARL-Server

**Digital Radio Advanced Application Real-time Link** - 数字无线电高级应用实时链路平台

DraARL 是一个专业的数字无线电实时通信平台，支持业余电台设备的语音通信、群组管理和用户认证。项目采用现代化的前后端分离架构，实现了完整的无线电通信管理平台。

## 功能特性

### 核心通信功能
- **实时语音转发** - 基于UDP的低延迟语音通信，支持Opus 16K编码
- **多设备群组通联** - 支持基础半双工语音模式
- **服务器互联** - 跨服务器语音转发
- **DraARLv1协议** - 自研设备通信协议

### 设备管理
- **多设备类型支持** - ESP32、Android、iOS、Windows、Web等
- **双轨制认证** - 设备密码 + JWT Token灵活认证
- **动态码绑定** - 新设备快速绑定流程
- **远程配置同步** - 设备参数远程配置

### 用户管理
- **多种登录方式** - 邮箱/密码登录、邮箱验证码登录、SSO单点登录
- **用户审核机制** - 支持注册审核和操作证审核
- **角色权限系统** - 管理员/普通用户分级管理

### 群组管理
- **公开群组** - 所有人可见、无需密码
- **私有群组** - 需验证密码后加入
- **虚拟互联组** - 跨群组互联

### 数据统计
- **通信记录** - 完整的通信历史记录和统计
- **趋势分析** - 通信趋势图展示
- **通联日志** - 个人通联记录管理

## 技术栈

### 后端
| 技术 | 版本 | 说明 |
|------|------|------|
| Go | 1.25+ | 主要开发语言 |
| Gin | 1.12 | HTTP Web框架 |
| GORM | 1.31 | ORM数据库框架 |
| MySQL | 5.7+ | 主数据库 |
| Redis | 6.0+ | 缓存（可选） |
| MinIO | - | 对象存储 |
| JWT | v5 | 用户认证 |
| WebSocket | Gorilla | 实时通信 |

### 前端
| 技术 | 版本 | 说明 |
|------|------|------|
| React | 19.2 | UI框架 |
| TypeScript | 5.9 | 类型安全 |
| Vite | 7.3 | 构建工具 |
| Material-UI | 7.3 | UI组件库 |
| React Router | 7.13 | 路由管理 |
| Recharts | 3.8 | 图表组件 |
| Axios | - | HTTP客户端 |

## 项目结构

```
DraARL-Server/
├── cmd/                        # 应用入口
│   └── udphub/                # UDP服务器主程序
├── internal/                   # 内部包（私有代码）
│   ├── aprs/                  # APRS协议支持
│   ├── captcha/               # 验证码功能
│   ├── common/                # 通用工具函数
│   ├── config/                # 配置管理
│   ├── db/                    # 数据库操作（原生SQL）
│   ├── gormdb/                # GORM数据库操作
│   ├── handler/               # HTTP处理器
│   ├── middleware/            # 中间件
│   ├── models/                # 数据模型
│   ├── protocol/              # 协议实现
│   ├── server/                # HTTP/WebSocket服务器
│   ├── service/               # 业务逻辑层
│   └── udphub/                # UDP服务器核心
├── pkg/                        # 公共包
│   ├── cache/                 # 缓存管理
│   ├── crypto/                # 加密工具
│   ├── jwt/                   # JWT认证
│   ├── minio/                 # MinIO对象存储
│   └── websocket/             # WebSocket支持
├── www/                        # 前端项目
│   ├── src/
│   │   ├── components/        # 组件
│   │   ├── pages/             # 页面
│   │   ├── services/          # API服务
│   │   └── types/             # 类型定义
│   └── dist/                  # 构建输出
├── test/                       # 测试代码
├── docs/                       # 文档
├── udphub.yaml                 # 配置文件
├── Makefile                    # 构建脚本
└── go.mod                      # Go模块定义
```

## 快速开始

### 环境要求

- Go 1.25+
- Node.js 20+
- MySQL 5.7+ / MariaDB 10.3+
- Redis 6.0+（可选）
- MinIO（可选，用于对象存储）

### 安装步骤

1. **克隆项目**
```bash
git clone https://github.com/your-repo/DraARL-Server.git
cd DraARL-Server
```

2. **安装后端依赖**
```bash
make deps
```

3. **安装前端依赖**
```bash
cd www
npm install
```

4. **配置数据库**

创建MySQL数据库：
```sql
CREATE DATABASE draarl CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

5. **修改配置文件**

复制并编辑 `udphub.yaml`：
```yaml
Database:
    Host: "127.0.0.1"
    Port: 3306
    User: "draarl"
    Password: "your_password"
    DBName: "draarl"

JWT:
    Secret: "your-jwt-secret-key"

DeviceAuth:
    AESKey: "32-byte-aes-key-here"
```

6. **构建前端**
```bash
cd www
npm run build
```

7. **运行服务**
```bash
# 开发模式
make run

# 或者构建后运行
make build
./draarl -c udphub.yaml
```

### 开发模式

**启动前端开发服务器：**
```bash
cd www
npm run dev
```

**启动后端服务：**
```bash
make run
```

访问 http://localhost:5173 进入前端开发页面。

## 配置说明

### 主配置文件 (udphub.yaml)

```yaml
# 系统配置
System:
    Host: "0.0.0.0"           # 监听地址
    Port: "60050"             # UDP服务端口
    LogPath: ""               # 日志文件路径
    IPfile: ./udphub.ipdb     # IP数据库文件
    ProxyProtocol: "v2"       # PROXY Protocol支持

# 数据库配置
Database:
    Host: "127.0.0.1"
    Port: 3306
    User: "draarl"
    Password: "your_password"
    DBName: "draarl"
    Charset: "utf8mb4"
    Collate: "utf8mb4_unicode_ci"
    MaxOpenConns: 25
    MaxIdleConns: 5
    MaxLifetime: 120

# Web服务配置
Web:
    Host: "localhost"
    Port: "9002"
    FrontendURL: "http://localhost:5173"

# Keycloak SSO配置（可选）
Keycloak:
    Enabled: false
    Name: "SSO名称"
    BaseURL: "https://sso.example.com"
    Realm: "draarl"
    ClientID: "draarl-frontend"
    ClientSecret: "your-client-secret"
    RedirectURI: "http://localhost:9002/api/sso/callback"

# MinIO对象存储配置
MinIO:
    Endpoint: "localhost:9000"
    AccessKey: "minioadmin"
    SecretKey: "minioadmin"
    UseSSL: false
    Bucket: "draarl"
    BasePath: "https://oss.example.com/draarl"

# JWT认证配置
JWT:
    Secret: "your-jwt-secret"

# 设备认证配置
DeviceAuth:
    AESKey: "32-byte-hex-string"  # AES-256加密密钥
```

### 前端环境变量

创建 `www/.env.local` 文件：
```env
VITE_API_URL=http://localhost:9002
VITE_WS_URL=ws://localhost:9002/ws
```

## API 文档

### 认证相关

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/auth/login` | 用户登录 |
| POST | `/api/auth/logout` | 用户登出 |
| POST | `/api/auth/register` | 用户注册 |
| POST | `/api/auth/email-login` | 邮箱验证码登录 |
| POST | `/api/auth/send-code` | 发送邮箱验证码 |
| POST | `/api/auth/reset-password` | 重置密码 |

### 设备管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/devices` | 获取设备列表 |
| GET | `/api/devices/:id` | 获取设备详情 |
| PUT | `/api/devices/:id` | 更新设备信息 |
| DELETE | `/api/devices/:id` | 删除设备 |
| GET | `/api/devices/:id/config` | 获取设备配置 |
| PUT | `/api/devices/:id/config` | 更新设备配置 |
| POST | `/api/devices/:id/config/sync` | 同步设备配置 |

### 设备绑定

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/device/pre-check` | 设备预检查 |
| POST | `/api/device/request-code` | 请求动态码 |
| POST | `/api/device/confirm-bind` | 确认绑定 |
| POST | `/api/device/bind` | 绑定设备 |

### 群组管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/groups` | 获取群组列表 |
| POST | `/api/groups` | 创建群组 |
| GET | `/api/groups/:id` | 获取群组详情 |
| PUT | `/api/groups/:id` | 更新群组 |
| DELETE | `/api/groups/:id` | 删除群组 |
| GET | `/api/groups/:id/members` | 获取群组成员 |

### 用户管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/users` | 获取用户列表（管理员） |
| GET | `/api/users/:id` | 获取用户详情 |
| PUT | `/api/users/:id` | 更新用户信息 |
| DELETE | `/api/users/:id` | 删除用户 |
| GET | `/api/me` | 获取当前用户信息 |
| PUT | `/api/me` | 更新个人资料 |
| PUT | `/api/me/password` | 修改密码 |

### 通信记录

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/comm-records` | 获取通信记录 |
| GET | `/api/comm-records/stats` | 通信统计 |
| GET | `/api/comm-records/user-stats` | 用户统计 |
| DELETE | `/api/comm-records/:id` | 删除记录 |

### WebSocket

| 路径 | 说明 |
|------|------|
| `/ws` | WebSocket连接端点 |

## 构建部署

### 使用 Makefile

```bash
# 显示所有可用命令
make help

# 构建当前平台
make build

# 构建Linux版本
make build-linux

# 构建ARM版本（树莓派）
make build-arm

# 构建ARM64版本
make build-arm64

# 构建Windows版本
make build-windows

# 运行测试
make test

# 代码格式化
make fmt

# 代码检查
make vet
```

### 手动构建

```bash
# 构建后端
go build -o draarl ./cmd/udphub

# 构建前端
cd www
npm run build
```

### Docker 部署

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o draarl ./cmd/udphub

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/draarl .
COPY --from=builder /app/udphub.yaml .
CMD ["./draarl", "-c", "udphub.yaml"]
```

### Systemd 服务

创建 `/etc/systemd/system/draarl.service`：
```ini
[Unit]
Description=DraARL Server
After=network.target mysql.service

[Service]
Type=simple
User=draarl
WorkingDirectory=/opt/draarl
ExecStart=/opt/draarl/draarl -c /opt/draarl/udphub.yaml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

启动服务：
```bash
sudo systemctl daemon-reload
sudo systemctl enable draarl
sudo systemctl start draarl
```

## 数据库模型

### 核心表结构

**users - 用户表**
- id, name, email, password, device_password
- callsign, dmrid, avatar, roles, status
- approval_status, created_at, updated_at

**devices - 设备表**
- id, name, dmrid, ssid, owner_id
- dev_model, group_id, status, is_certed
- disable_send, disable_recv, is_online

**groups - 群组表**
- id, name, type, callsign, password
- owner_id, status, is_virtual

**group_members - 群组成员表**
- id, group_id, device_id
- disable_send, disable_recv, joined_at

**comm_records - 通信记录表**
- id, device_id, group_id
- start_time, end_time, duration
- voice_packets

## 开发指南

### 代码规范

- Go代码遵循 [Effective Go](https://golang.org/doc/effective_go) 规范
- 使用 `gofmt` 格式化代码
- 使用 `go vet` 进行静态检查
- TypeScript代码使用 ESLint 检查

### 分支管理

- `master` - 生产分支
- `dev` - 开发分支
- `feature/*` - 功能分支
- `hotfix/*` - 热修复分支

### 提交规范

```
feat: 新功能
fix: 修复bug
docs: 文档更新
style: 代码格式调整
refactor: 代码重构
test: 测试相关
chore: 构建/工具相关
```

## 许可证

[MIT License](LICENSE)

## 贡献

欢迎提交 Issue 和 Pull Request。

## 联系方式

- 项目主页: https://github.com/your-repo/DraARL-Server
- 问题反馈: https://github.com/your-repo/DraARL-Server/issues
