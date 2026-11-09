# 设备能力切换与客户端同步

日期：2026-11-09。涉及 DraARL-Server（dev）、DraARL-Android（main）和 DraARL-RFBox-1（master）。

## 已确认范围

- 同一设备加装／拆除 SA818 后，沿用设备身份与群组，根据当前上报型号切换配置能力。网络模式仅配置音频和板级 IO，射频模式增加频率、亚音、带宽、功率。
- 配置上报只更新实际报告且当前设备支持的字段，不补写不存在的默认值，不清空另一模式已存参数。重新接入时查询新增能力的本地配置。
- 移除射频保护的服务端 API、TLV、Web、Android、设备设置、BLE 与发射时长／占空比限制实现。旧字段忽略，TLV 编号保留不复用。断网与音频结束释放 PTT 等正常清理继续保留。
- Android 与 RFBox 从 HTTP API 获取本站唯一 UDP 入口，移除多边缘节点的测速择优及归属展示；连接重试时重新发现地址，避免长期使用过期端口。
- 整理 RFBox 320×240 界面布局、控件与状态层级，保留旋钮导航与本地配置。提供真实 LVGL 本地渲染入口检查页面。
- 保留站间互联、幽灵多会话、普通 UDP、设备配置同步及现有登录权限。匿名收听不在本次实现范围内。

## 五项修改的实际结果

- 服务端按型号返回配置能力；型号 1 支持 SA818 与音频，型号 2 支持音频和 SQL/PTT 极性。参数按模式保留，增量上报不擦除另一模式；设备上线和在线型号变化都会同步，过期表单提交返回 `409` 并提示重载。
- 射频保护的服务端 API、运行逻辑、TLV、设备 NVS、屏幕设置与 Android 设置已移除；旧字段忽略，0x0C–0x0F 不复用。正常释放 PTT 继续保留。
- Android 与 RFBox 按 HTTP API 返回的本站唯一入口连接；同一节点的地址／端口变化也会重连。取消启动不再转成错误，API 失败不再猜测默认 UDP 端口。
- RFBox 的首页、网络模式、发射状态、设置、射频设置、关于页已重新布局，使用共享样式。`tools/ui_preview` 直接编译实际 LVGL 页面，旧 SquareLine 工程仅作历史参考。

## 后续动态码与安卓 BLE 修复

上述五项完成后实施以下修复：

- 设备限流的多个桶原子检查，被拒绝的重试不额外消耗其他桶；提高共享 NAT 与轮询抖动容忍度，统一返回 `Retry-After` 和 `data.retry_after`。客户端保留输入、阻止重复提交并倒计时。
- 同一用户可重试已确认的动态码绑定；已就绪配置保留至 10 分钟会话过期，避免尚未领取就被清理。待绑定状态返回快照，续码移除旧映射，提交配置复核归属／有效期。
- RFBox 使用服务端 `expires_in` 控制续码，先确认绑定再决定续码，`bound: true` 后继续等配置；正确区分 `429`、临时网络错误与绑定过期。
- Android 将服务发现、通知描述符、认证、状态读取和 RPC 写入放入一个 GATT 队列；两路通知订阅与初次状态读取完成后才标记就绪。回调统一交给主线程，忽略旧连接回调，超时清理连接与待处理请求。
- Android 的 BLE 配置支持 Wi-Fi 扫描、HTTP API、DMR ID，以及按设备能力显示的射频／音频／IO 参数；账号密码可同时留空使用动态码绑定。验证码文案区分蓝牙认证码与服务器绑定码；断线或回读失败不显示可保存的旧配置。
- 用户已明确：Web BLE 指服务端前端的预配置工具，连接工作正常。本次保留 `www/src/services/bleProvision.ts`、`useBleProvisioning` 及该工具的连接实现；以其 UUID、帧格式和认证交互作为安卓参照。固件目录下弃用的 `web/ble-config.html` 已按用户后续要求删除；配置入口使用服务端前端工具或 Android。

## 本地输入和页面交互优化

- 删除弃用 `web/ble-config.html`，清理 README 的旧入口、保护参数和旧按键说明；服务端 Web BLE 连接代码保持不变。
- 统一为旋转选择、按下进入／确认、KEY0 返回／取消。进入设置立即有焦点，射频功率按一次切换，关于页按下直接返回，不再经过无用选择状态。长按 KEY0 在所有运行页面打开电源菜单。
- 主页参数先选择再编辑。频率旋转选位，按下调值，再按回到选位，末尾 Save 或长按旋钮保存；亚音、静噪和带宽按下保存。KEY0 撤销当前字段，返回字段选择，再按回待机。
- 字段修改采用快照草稿，只有确认后才应用模块、写入 NVS、安排同步；失败保留草稿并提示重试。配置查询／自动上报不包含未确认草稿；服务器或 BLE 下发新配置会替换旧草稿，取消不再恢复过期快照。外部配置应用或保存失败恢复原运行参数。
- 亮度旋转实时预览，仅确认时写入一次 NVS，KEY0 恢复原亮度。不可用的固件更新／射频入口不参与旋钮导航；子页返回保留设置焦点。
- 25 ms 消抖与 1 秒长按由两个按钮共用，长按到时仅触发一次，松开不再短按。清理未使用的显示驱动输入注册路径，由应用统一分发输入；弹窗和锁屏先消费手势。
- PTT 输入暂停与释放后 500 ms 等待继续保留；恢复时丢弃残留的旋转／按压，持续按住的按钮须先释放。仅浏览字段不会阻止网络来话，开始网络转射频时退出浏览，显示实际 TX 状态。
- 青色选择、橙色编辑，描边不再改变控件布局；新增逐状态操作提示、完整地址换行与滚动，修复低功率缺字。真实控制器离屏渲染 29 个状态，提供 `tools/ui_preview` CMake／CTest 入口及相册导出脚本。

## Android release 产物

- 使用现有 release 签名和版本 `2.0.0-alpha4`（versionCode 11），运行 `assembleReleaseAll verifyReleaseArtifact -Pabi=all` 成功；验证 native ABI、JNI 保留规则、R8 产物和 Baseline Profile。三个 APK 均通过 SDK `apksigner verify`，签名为既有 RSA 证书、APK v2。
- 产物目录：`DraARL-Android/app/build/outputs/apk/release/`。

| 文件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `app-universal-release.apk` | 53,213,009 | `c609da42f088345fcd408c9885065e146637a10af3d0da454073965c8f01cf20` |
| `app-arm64-v8a-release.apk` | 33,887,472 | `2430a9953a9d1a05943a9c6ccf30b2b4db0e443bc54928aa690fea154b6e8adf` |
| `app-armeabi-v7a-release.apk` | 27,178,578 | `d51cd5ccfc2e8c283357efa727bd55851722c5d6c7f0e96a809c9d35ecc4b91f` |

- 验证任务原有 Gradle 配置缓存序列化提示导致本次缓存丢弃，构建和产物检查成功，不影响 APK。未改版本号、签名或发布配置。

## 验证与限制

- 服务端本地和 Ubuntu 虚拟机：`go test -count=1 ./cmd/... ./internal/... ./pkg/... ./tools/... ./test/...`；待绑定管理器、限流、handler 的 race 检查通过。
- 虚拟机真实 MySQL 临时库：RF→网络→RF 参数保留、写库失败重试、实际 UDP 配置下发／配置查询／时间同步、HTTP 能力元数据与 `409`；动态码重复申请／绑定／领取、跨用户拒绝、限流等待后恢复，均通过。临时库在测试后删除。
- 服务端前端：lint、构建、13 项 legacy 测试和 44 项 Vitest 测试通过；配置覆盖率要求通过。
- Android：434 项单元测试、debug APK、lintDebug、Spotless 通过；lint 为 0 个错误、39 个警告。Detekt 对本次代码已无新增问题，仍有原有的 3 项：`AprsLiveStream.InjectDispatcher`、`SecureSessionStore.UseOrEmpty`、`AprsPacketCodecScreen.LongParameterList`。
- RFBox：PlatformIO 固件构建通过，RAM 23.4%、Flash 61.2%。实际 LVGL／控制器的 29 个 320×240 状态已渲染并目视检查；原图、3 倍图、5 页合图和相册位于 RFBox `build/ui-preview/details/`。58 项输入与导航检查通过。
- 未进行设备烧写、真实手机 BLE 连接、旋钮或 GPIO 测试；安卓 BLE 的实际连接效果仍需手机与 RFBox 联调确认。服务端 Web BLE 连接没有改动。
- 服务端按用户要求合并至 `master`／`dev` 并准备发布 `v2.0.1-alpha7`，保留 `edge` 分支。Android `main` 和 RFBox `master` 的配套修改不随服务端仓库推送；服务端发布内容见 [版本说明](release-v2.0.1-alpha7.md)。
