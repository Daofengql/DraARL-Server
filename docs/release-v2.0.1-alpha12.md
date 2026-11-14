# DraARL v2.0.1-alpha12

本版本拆分 BoxV1 与 Box32 的设备型号，并将正式使用文档统一到 GitHub Pages。

- 原 RFBox 设备统一命名为 BoxV1-RF / BoxV1-EX，保留型号 ID 1 / 2。
- 为 Box32 分配 ANA / DIG / MIX 三种型号，ID 分别为 3 / 4 / 5；当前 Box32 固件默认使用 ANA。
- 同步服务端、网页设备列表、配置能力和 OTA 型号白名单。Box32 模拟型号提供音频与 SQL/PTT 极性配置；DIG 的 MMDVM 参数保留至后续实现。
- 顶部文档入口直接打开 GitHub Pages，旧 `/docs` 地址兼容跳转；下载中心仍保留在当前实例的 `/downloads`。
- 移除内置 Markdown 文档副本及其渲染依赖，更新在线文档导航、首页和样式。

本版本保留 DraARLv1 报文格式。既有 BoxV1 和 Android 客户端无需同步升级即可继续收发；旧 Box32 固件继续按原来上报的型号处理。

已通过 Server Go 测试、网页 lint/类型检查、64 项网页单元测试、前端构建和文档严格构建；Box32 与本地真实 Server 的鉴权、双向语音、配置同步、重连和 OTA 联测通过。
