# Supervisor 自动部署

Release 工作流完成五个平台构建、校验文件上传和发布后，调用 `Deploy production` 工作流。生产机下载**本次 tag** 的 Linux AMD64 安装包，不依赖 GitHub 的 latest 指针；alpha 预发布同样会自动部署。

部署仅提取程序文件，保留现有配置和数据。替换前备份旧程序，同一文件系统内原子替换，重启指定的 Supervisor 程序，然后检查 `/readyz` 和实际运行版本。失败则恢复旧程序并重启，工作流返回失败。自动回滚针对程序文件；数据库迁移需要保持对上一版程序的兼容。

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

## GitHub 配置

创建 `production` Environment，设置以下变量和 Secrets：

| 类型 | 名称 | 内容 |
|---|---|---|
| Variable | `DRAARL_DEPLOY_HOST` | SSH 主机地址 |
| Variable | `DRAARL_DEPLOY_PORT` | SSH 端口，默认 22 |
| Variable | `DRAARL_DEPLOY_USER` | `draarl-deploy` |
| Secret | `DRAARL_DEPLOY_PRIVATE_KEY` | 专用 SSH 私钥 |
| Secret | `DRAARL_DEPLOY_KNOWN_HOSTS` | 经核对的主机公钥记录 |

无须将 root 密码存入 GitHub。生产部署串行执行；重复部署相同且健康的程序不会重启或覆盖旧版备份。

首次启用可手动运行 `Deploy production`，输入现有的 Release tag，验证完整部署链路。之后推送新版本 tag 即会在 Release 发布成功后自动部署。若需要暂停自动更新，移除 Release 工作流中的 `deploy-production` 调用即可。

## 验证

Linux 上运行：

```sh
bash -n deploy/supervisor/deploy.sh deploy/supervisor/ssh-entrypoint.sh
python3 deploy/supervisor/test_deploy.py
```

测试通过本地 HTTP 和隔离的 Supervisor 边界执行实际部署脚本，覆盖成功更新、重复部署、就绪检查失败回滚、校验和错误、版本不符和非法 SSH 命令，不会接触真实服务。
