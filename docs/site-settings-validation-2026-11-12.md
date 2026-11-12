# 站点设置与录音阈值验证（2026-11-12）

## 变更范围

- 删除备案图片，保留备案文字链接。
- 亮、暗主题 Logo 独立上传和预览；未配置暗色图片时回退亮色图片。
- 仅 Logo、Favicon 使用 `/logo.png`、`/logo-dark.png`、`/favicon.ico` 服务端读取路由。其他资源继续使用原有存储访问与签名方式。
- APRS 坐标、海拔和定位按钮随卡片宽度排列，长日志换行。
- 录音阈值与记录时长按实际 Opus 音频时长计算，修复配置缓存失效分类，校验最小阈值与最大时长冲突。

## 自动检查

以下检查均通过：

- Windows：`go test ./cmd/... ./internal/... ./pkg/... ./test/... ./tools/...`，同范围 `go vet`。
- 前端：`npm test`（15 项设置测试、47 项 Vitest 测试）、`npm run lint`、`npm run build`。
- OpenAPI：`go run ./tools/openapi -check`。
- Hyper-V Ubuntu：`go test -race -count=1 ./internal/udphub ./internal/handler ./internal/server`。
- Hyper-V Ubuntu：独立 MySQL 数据库和私有 S3 桶运行 `TestSiteBrandingMySQLS3`，使用 `-tags=embed`。覆盖主题独立上传、替换、公开读取、Favicon、删除回退和旧签名 URL 兼容；将预签名入口设为不可访问地址，确认品牌图片由服务端读取，并断言其他资源仍返回签名 URL。

录音回归测试覆盖 2.5–120ms Opus 包、合并帧、最小阈值上下及等于边界、配置热更新、最大时长分段和禁用清理。

## 浏览器验证

Playwright 使用测试 API 和图片响应检查前端行为；真实 MySQL/S3 读取由上述 Hyper-V 集成测试验证。

- APRS 在 320、390、768、900、1024、1280、1920 像素宽度下无控件重叠、横向溢出。
- 三张已配置图片可预览；主题切换选择对应 Logo；保存保留两个 Logo 和 Favicon。
- 模拟图片读取失败后，保存仍保留配置；重试后预览恢复。
- 备案图标已移除，文字链接保留；录音冲突设置阻止保存，合法边界可保存。

本地验证产物位于 `output/site-settings-qa-20261112/`，页面截图位于 `output/playwright/site-aprs-*.png` 和 `output/playwright/site-branding-dark.png`。测试使用的两个隔离容器已清理，未部署生产环境。
