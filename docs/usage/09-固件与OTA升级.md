# 09. 固件与 OTA 升级

## 1. 系统边界

设备固件在后台归入“客户端资源分发 → 设备固件”，但仍是独立子系统：

- 使用独立 `firmware_releases` 表、管理 API 和 `/api/public/firmware/latest` 设备查询接口。
- 按 `dev_model` 和固件版本选择，不创建 `ClientResource`、artifact 或通用平台 target，也不出现在客户端资源 manifest。
- 与通用客户端资源共享私有 S3、预签名 PUT/GET、用户隔离 staging、不可变 final 提升和流式 SHA-256。
- 每条发布记录可选择 `presigned` 或 `proxy` 下载模式；`proxy` 为旧版 RFBox 提供固定短地址，服务端隐藏 S3 签名并流式转发。

当前支持的设备型号由服务端白名单决定：

| DevModel | 名称 |
|---|---|
| 1 | ESP32 链路盒子（1W 射频版） |
| 2 | ESP32 链路盒子（无射频版） |
| 107 | ESP32 链路台/手咪（历史型号） |

## 2. 管理端上传

推荐使用直传流程：

1. `POST /api/storage/presign-put`，`file_type=firmware`。
2. 浏览器按返回的 method/headers 将文件 PUT 到 `upload_url`。
3. `POST /api/firmware/complete` 落库。

申请示例：

```json
{
  "file_type": "firmware",
  "file_name": "firmware.bin",
  "size": 1048576,
  "content_type": "application/octet-stream"
}
```

complete 示例：

```http
POST /api/firmware/complete
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "dev_model": 1,
  "version": "1.2.3",
  "changelog": "修复音频卡顿并优化功耗",
  "file_name": "firmware.bin",
  "download_mode": "proxy",
  "object_key": "staging/firmware/7/2026/07/uuid.bin",
  "upload_token": "signed-upload-grant"
}
```

服务端验证管理员、上传授权、staging 所有者、实际大小和型号/版本唯一性，从对象流计算 SHA-256，然后提升到不可变 key 并再次核对大小与哈希：

```text
firmware/<dev-model>/<version>/<sha256>/<file-name>
```

数据库写入失败时会清理刚提升的对象。对象已存在时返回 409，不会覆盖已有内容。旧 `firmware_releases` 数据及其历史对象路径保持可读。

仍保留 `POST /api/firmware` multipart 代理上传接口；它也使用同一 staging、流式哈希、不可变 final key 和完整性复核，不要求 S3 bucket 公共读。

固件最大 16 MiB，同一 `dev_model` 与 `version` 只能有一条记录。版本格式保持现有设备协议兼容，支持 `1.2.3` 和 `1.2.3-beta.1`。

## 3. 管理接口

| Method | Path | Auth | 说明 |
|---|---|---|---|
| GET | `/api/firmware?dev_model=1&page=1&page_size=20` | Admin | 按型号分页列出固件，并返回短时下载地址。 |
| POST | `/api/firmware` | Admin | multipart 代理上传。 |
| POST | `/api/firmware/complete` | Admin | 完成预签名直传。 |
| DELETE | `/api/firmware/:id` | Admin | 删除记录和对象，并重新计算该型号的 latest。 |
| GET | `/api/public/firmware/:id/download` | Public | 代理模式下载，固定返回 `200`、准确 `Content-Length` 和 `application/octet-stream`。 |

列表响应的 `data` 包含 `items`、`total`、`page` 和 `page_size`。每个 item 包含型号、版本、更新日志、文件名、字节数、纯十六进制 SHA-256、`is_latest`、创建时间和一小时有效的 `download_url`。

## 4. 设备查询

```http
GET /api/public/firmware/latest?dev_model=1&current_version=1.2.2
```

`dev_model` 必填；`current_version` 可选。存在更新时：

```json
{
  "code": 200,
  "message": "成功",
  "data": {
    "id": 2,
    "dev_model": 1,
    "version": "1.2.3",
    "changelog": "修复音频卡顿并优化功耗",
    "file_name": "firmware.bin",
    "file_size": 1048576,
    "file_hash": "0123456789abcdef...",
    "hash_algo": "sha256",
    "has_update": true,
    "download_url": "https://storage.example.com/...X-Amz-Signature=...",
    "create_time": "2026-07-30T12:00:00+08:00"
  }
}
```

当前版本已不低于服务端 latest 时返回 200 且 `data: null`；型号没有固件时返回 404。`download_mode=presigned` 返回有效期一小时的签名地址，`download_mode=proxy` 返回 `/api/public/firmware/:id/download`。当 `Firmware.AutoProxy=true` 且预签名地址超过 `Firmware.MaxPresignedURLLength`（默认 255）时自动使用代理地址；设置为 `false` 可紧急回滚为仅预签名模式。代理会在响应前核对对象大小和 SHA-256，S3 错误转换为 502。

## 5. 设备端 OTA 约定

设备应按以下顺序处理：

1. 查询 latest，保存响应中的版本、大小、SHA-256 和短时 URL。
2. 通过 HTTPS 下载；弱网环境可使用 HTTP Range 续传，URL 过期后重新查询再从已有位置继续。
3. 写入 Flash 前核对实际字节数和完整 SHA-256，不能把 S3 ETag 当作文件哈希。
4. 使用平台提供的 OTA/分区校验机制写入非活动分区，确认可启动后再切换。
5. 升级失败时保留可工作的旧分区；设备侧是否允许降级、签名验证和灰度规则由固件协议独立演进。

常见版本比较：

```text
1.2.2 < 1.2.3
1.2.3-beta.1 < 1.2.3
1.9.9 < 2.0.0
```

## 6. 安全与排障

- bucket 保持私有；设备只使用 API 返回的短时预签名 GET。
- `/files/*` 和通用 `/api/storage/get` 不能通过永久 URL绕过固件访问策略。
- 下载失败或 URL 过期：重新查询 latest 获取新 URL。
- 哈希不一致：丢弃临时文件并重新下载，不写入 Flash。
- 查询 404：核对 `dev_model` 是否在支持列表且该型号已有固件。
- 写入或启动失败：保留/回滚旧分区，并通过设备日志检查空间、分区表和硬件兼容条件。

通用安装包、模型、字库等资源协议见 [客户端资源分发](../api/12-客户端资源分发.md)。
