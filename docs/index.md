---
title: DraARL Server 文档
---

<div class="docs-hero">
  <div>
    <p class="docs-kicker">DRAARL SERVER / DOCUMENTATION</p>
    <h1>从部署到设备接入，所有资料集中在这里。</h1>
    <p class="docs-lead">这套文档面向部署人员、平台管理员、普通用户、设备开发者和客户端开发者。内容随 DraARL Server 源码发布，行为事实以当前代码、配置模板和根目录 <code>VERSION</code> 为准。</p>
    <div class="docs-actions">
      <a class="md-button md-button--primary" href="usage/01-部署与配置/">开始部署</a>
      <a class="md-button" href="api/">查看 API</a>
    </div>
  </div>
  <div class="docs-hero-meta">
    <span class="docs-meta-label">DOCUMENT STATUS</span>
    <strong>持续维护</strong>
    <span>GitHub Pages · MkDocs Material</span>
    <span>支持全文搜索、章节导航、代码复制和移动端阅读</span>
  </div>
</div>

## 按场景进入

<div class="docs-card-grid">
  <a class="docs-card" href="usage/01-部署与配置/">
    <span class="docs-card-index">01</span>
    <strong>新部署或升级</strong>
    <span>环境要求、配置、Docker Compose、手动部署和升级检查。</span>
    <small>部署与配置 →</small>
  </a>
  <a class="docs-card" href="usage/06-管理员后台/">
    <span class="docs-card-index">02</span>
    <strong>平台管理员</strong>
    <span>用户审核、设备、群组、站点配置、资源和运维入口。</span>
    <small>管理与运维 →</small>
  </a>
  <a class="docs-card" href="usage/04-设备与群组/">
    <span class="docs-card-index">03</span>
    <strong>普通用户</strong>
    <span>账号、用户控制台、设备绑定、群组和在线收发。</span>
    <small>使用说明 →</small>
  </a>
  <a class="docs-card" href="usage/07-设备接入与API快速对接/">
    <span class="docs-card-index">04</span>
    <strong>设备或客户端开发者</strong>
    <span>接入流程、认证、配置同步、API 示例和兼容边界。</span>
    <small>设备接入 →</small>
  </a>
  <a class="docs-card" href="Protocol/">
    <span class="docs-card-index">05</span>
    <strong>协议实现者</strong>
    <span>DraARLv1 UDP 报文、字段定义、设备型号和状态码。</span>
    <small>协议文档 →</small>
  </a>
  <a class="docs-card" href="架构设计/">
    <span class="docs-card-index">06</span>
    <strong>架构与性能</strong>
    <span>系统模块、数据流、容量测试、数据库和前端工程约定。</span>
    <small>架构与性能 →</small>
  </a>
</div>

## 推荐阅读顺序

### 新部署或运维人员

1. [架构设计](架构设计.md) 了解模块、数据流和部署形态。
2. [部署与配置](usage/01-部署与配置.md) 完成服务启动和配置检查。
3. [运维与排障](usage/08-运维与排障.md) 处理常见故障和运行检查。
4. [数据库迁移](database-migrations.md) 执行版本升级和回退。

### 平台管理员

1. [管理员后台](usage/06-管理员后台.md) 了解管理功能和权限边界。
2. [数据字典](数据字典.md) 查询数据库表、字段和索引。
3. [资源上传与站点配置](api/07-资源上传与站点配置.md) 配置公开资源、SMTP、APRS 和存储。

### 设备或客户端开发者

1. [设备接入与 API 快速对接](usage/07-设备接入与API快速对接.md) 了解接入流程。
2. [DraARLv1 协议](Protocol.md) 实现 UDP 报文和设备认证。
3. [API 文档总览](api/README.md) 查阅 HTTP API、权限和错误处理。
4. [WebSocket 协议](api/10-WebSocket协议详解.md) 接入浏览器幽灵设备和实时通信。

## 文档约定

- 版本事实以仓库根目录 `VERSION` 为准。
- 配置默认值以 `config.yaml.example` 和 `internal/config/config.go` 为准。
- API 路由以 `internal/server/server.go` 和对应 handler 为准。
- `JWT` 表示需要 `Authorization: Bearer <access_token>`；`Admin` 表示管理员权限。
- 历史升级资料会明确标记；运行行为以当前代码和配置模板为准。

## 本地预览与发布

```bash
pip install -r docs/requirements.txt
mkdocs serve
```

构建静态站点：

```bash
mkdocs build --strict
```

GitHub Actions 工作流位于 `.github/workflows/docs-pages.yml`，会在 `master` 分支文档变更、手动触发或 Release 工作流成功后构建并发布到 [GitHub Pages](https://daofengql.github.io/DraARL-Server/)。
