# DraARL 文档站

这是 DraARL Server 的文档入口，包含：

- 使用与说明文档（按功能拆分）
- DraARLv1 协议文档
- API 对接文档（按模块拆分）

## 推荐阅读顺序

1. 新部署或运维人员：先读 [使用与说明文档总览](usage/README.md)，再读 [部署与配置](usage/01-部署与配置.md)。
2. 平台管理员：阅读 [管理员后台](usage/06-管理员后台.md) 和 [运维与排障](usage/08-运维与排障.md)。
3. 普通用户说明：阅读 [账号、登录与权限](usage/02-账号登录与权限.md)、[用户控制台](usage/03-用户控制台.md)、[设备与群组](usage/04-设备与群组.md)。
4. 设备或客户端对接：阅读 [设备接入与 API 快速对接](usage/07-设备接入与API快速对接.md)，再查阅 [API 文档](api/README.md) 和 [协议文档](Protocol.md)。

## 本地预览

```bash
pip install -r docs/requirements.txt
mkdocs serve -f docs/mkdocs.yml
```

启动后访问：`http://127.0.0.1:8000`

## 构建静态站点

```bash
mkdocs build -f docs/mkdocs.yml
```

构建输出目录：`site/`

## 自动发布

- GitHub Actions 工作流：`.github/workflows/docs-pages.yml`
- 触发条件：`Release` 工作流成功完成后自动触发
- 结果：自动构建并发布到 EdgeOne Pages（production 环境）

需要在 GitHub 仓库配置：

- `Secrets`:
  - `EDGEONE_API_TOKEN`
- `Variables`:
  - `EDGEONE_PAGES_PROJECT`（EdgeOne Pages 项目名，若不存在会自动创建）
