# 前端组件规范

公共组件放 `www/src/components/common/`，功能组件放对应目录。优先复用 MUI 主题、SearchBar、加载状态和公共错误处理。具名 TypeScript props 明确必填项和异步回调；页面负责 API/权限，展示组件接收数据、busy、disabled 和事件。

每个输入有可见 label；图标按钮添加说明动作与对象的 aria-label。对话框 aria-labelledby 指向标题，错误说明用 aria-describedby 关联。支持键盘聚焦、Enter/Space/Escape，不能只靠颜色表达状态。PTT 按钮在失焦、卸载、禁用时必须释放。

异步操作提供进度，busy 时禁止重复提交。批量动作限制并发，成功后移除选中项，失败项保留重试，删除先确认数量。搜索应防抖并丢弃过期响应。Ctrl/Cmd+S 只在当前可编辑表单生效，保存中不能重复触发。

布局用主题间距和断点，手机工具栏可换行，小屏预配置全屏。表格可在自身容器滚动，不能让页面整体溢出。至少检查 320px、768px 和桌面宽度。错误使用 getErrorMessage(error, fallback)，日期使用 apiTime；旧 wall-clock 结合 X-Database-Timezone，ISO 时区保持含义，Logbook UTC/北京时间切换按现有设计。

Vitest + React Testing Library 验证工具、hook 和组件交互。核心工具覆盖率门槛见 vitest.config.ts，不通过扩大忽略范围掩盖缺失测试。BLE ACK/回读可模拟，断电持久化需单独实机记录。主要页面使用真实 API 做浏览器回归、axe-core 和移动布局检查；组件单元测试不能替代浏览器验证。本项目以现有组件和本文作为规范，不额外引入 Storybook 运行时。
