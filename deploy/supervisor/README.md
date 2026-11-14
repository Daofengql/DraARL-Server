# Supervisor 自动部署

Release 工作流完成五个平台构建、校验文件上传和发布后，调用 `Deploy production` 工作流。GitHub runner 下载**本次 tag** 的 Linux AMD64 安装包，经受限 SSH 传到生产机；两端都校验 SHA256，不依赖 latest 指针，生产机无需直连 GitHub 下载大文件。alpha 预发布同样会自动部署。

部署仅提取程序文件，保留现有配置。每次更新按以下顺序执行：

1. 校验安装包、程序版本和 `-migrate-only` 能力，备份旧程序。
2. 停止指定的 Supervisor 程序，使用 `mysqldump` 备份该应用数据库，校验压缩包完整性。
3. 新程序读取原配置，通过 `-migrate-only` 执行现有版本化迁移及 GORM schema 同步，完成后退出，不启动 HTTP/UDP 服务。
4. 在同一文件系统内原子替换程序，由 Supervisor 启动，然后检查 `/readyz` 和实际运行版本。

下载、校验或备份失败时，数据库尚未迁移，保留或恢复原服务。**迁移开始后的失败**会停止部署、保持服务停止并写入 `deployment-failed` 标记，保留数据库备份、旧程序和迁移日志；后续自动部署也会暂停。管理员核对数据库状态及程序版本、完成恢复后再清除标记。MySQL DDL 可能已经提交，因此这里不自动恢复数据库或启动旧程序。

备份在 `/var/lib/draarl-deploy/backups/`，权限仅 root 可读，成功升级后保留最近三份。部署凭据不会输出到 Actions 日志。重复部署相同且健康的程序会直接返回，不重复备份、迁移或重启。

## 生产机准备

由管理员安装下列文件，全部由 root 持有：

| 仓库文件 | 服务器路径 | 权限 |
|---|---|---|
| `deploy.sh` | `/usr/local/sbin/draarl-deploy` | `0755` |
| `ssh-entrypoint.sh` | `/usr/local/libexec/draarl-deploy-ssh` | `0755` |
| `production.conf.example` | `/etc/draarl-deploy.conf` | `0600` |

按实际情况填写配置文件中的程序目录、Supervisor 路径和程序名、HTTP 地址。运行用户 `draarl-deploy` 使用独立 SSH 密钥，其 `authorized_keys` 必须限制为：

```text
restrict,command="/usr/local/libexec/draarl-deploy-ssh" ssh-ed25519 PUBLIC_KEY draarl-production
```

在 `/etc/sudoers.d/draarl-deploy` 配置并用 `visudo -cf` 校验：

```sudoers
draarl-deploy ALL=(root) NOPASSWD: /usr/local/sbin/draarl-deploy *
```

该密钥仅能向固定的部署程序传入版本号，不能开启终端、执行其他 SSH 命令或转发端口。原有应用的 Supervisor 配置和运行用户不变。修改部署脚本时，需要由管理员同步更新生产机上的 root 文件。

`MYSQL_DEFAULTS_FILE` 指向管理员维护的 MySQL 客户端配置文件，权限为 root `0600`，包含应用数据库的连接凭据；`DATABASE_NAME` 必须对应原应用配置。此文件只留在生产机，不提交到仓库或 GitHub。部署账户不得修改上述配置、脚本及 sudoers 文件。

## GitHub 配置

创建 `production` Environment，设置以下变量和 Secrets：

| 类型 | 名称 | 内容 |
|---|---|---|
| Variable | `DRAARL_DEPLOY_HOST` | SSH 主机地址 |
| Variable | `DRAARL_DEPLOY_PORT` | SSH 端口，默认 22 |
| Variable | `DRAARL_DEPLOY_USER` | `draarl-deploy` |
| Secret | `DRAARL_DEPLOY_PRIVATE_KEY` | 专用 SSH 私钥 |
| Secret | `DRAARL_DEPLOY_KNOWN_HOSTS` | 经核对的主机公钥记录 |

无须将 root 密码或数据库密码存入 GitHub。生产部署串行执行。

首次启用可手动运行 `Deploy production`，输入支持 `-migrate-only` 的 Release tag（从 `v2.0.1-alpha13` 起），验证完整部署链路。之后推送新版本 tag 即会在 Release 发布成功后自动部署。若需要暂停自动更新，移除 Release 工作流中的 `deploy-production` 调用即可。

## 验证

Linux 上运行：

```sh
bash -n deploy/supervisor/deploy.sh deploy/supervisor/ssh-entrypoint.sh
python3 deploy/supervisor/test_deploy.py
```

测试通过本地 HTTP、隔离的 Supervisor/数据库边界执行实际部署脚本，覆盖成功更新、重复部署、备份失败恢复原服务、部分迁移失败停服、迁移后就绪失败停服、校验和错误、版本不符、缺少迁移入口和非法 SSH 命令，不会接触真实服务。
