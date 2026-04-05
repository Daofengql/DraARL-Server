# DraARL 文档站

这是 DraARL Server 的文档入口，包含：

- DraARLv1 协议文档
- API 对接文档（按模块拆分）

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
