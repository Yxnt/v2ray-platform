# v2ray-platform 操作流程指南

## 1. 系统架构概览

```text
┌─────────────────────────────────────────────────────────────────┐
│                        Control Plane                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │   Admin UI  │  │   REST API  │  │  PostgreSQL │            │
│  └─────────────┘  └─────────────┘  └─────────────┘            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ 管理节点配置
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Node Agent (每台代理服务器)                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │   V2Ray     │  │   Agent     │  │  Stats API  │            │
│  │   Config    │  │   Daemon    │  │   10085     │            │
│  └─────────────┘  └─────────────┘  └─────────────┘            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ Clash 订阅
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      用户 (Clash 客户端)                         │
└─────────────────────────────────────────────────────────────────┘
```

## 2. 运行 Control Plane

### 2.1 本地开发模式

```bash
# 设置环境变量
export BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export BOOTSTRAP_ADMIN_PASSWORD=change-me-now

# 启动控制面板 (使用内存存储)
go run ./cmd/control-plane
```

访问 http://localhost:8080 打开管理界面。

### 2.2 生产环境部署（默认：SSH 服务器 + GHCR）

#### 第一步：准备可 SSH 连接的 Linux 服务器

1. 确认目标主机可以通过 SSH 访问
2. 确认目标主机已安装 `docker` 和 `docker compose`
3. 决定使用内置 Postgres 容器，还是自己提供外部 `DATABASE_URL`

#### 第二步：加载服务端部署环境变量

```bash
cp deploy/server.env.example /tmp/v2ray-platform-server.env
# edit the copied file with real values
. /tmp/v2ray-platform-server.env
```

关键变量：

- `DEPLOY_HOST`
- `DEPLOY_PATH`
- `CONTROL_PLANE_PUBLIC_URL`
- `CONTROL_PLANE_IMAGE`，默认值：`ghcr.io/yxnt/v2ray-platform-control-plane:latest`
- `POSTGRES_RESTORE_DUMP`，可选，用于首次部署时恢复数据库备份

SSH 服务器部署脚本会在缺省时自动生成 bootstrap 密钥，包括用于加密
CloudFront AWS 凭证的 `CLOUDFRONT_MASTER_KEY`。

#### 第三步：执行预检和部署

```bash
bash deploy/preflight-auto.sh
bash deploy/deploy-auto.sh
```

脚本会自动上传仓库、生成 `.env.server`、按需恢复 Postgres 备份、从
GHCR 拉取镜像，并启动 control-plane 容器。

#### 可选：Cloud Run 部署

```bash
bash deploy/preflight-cloudrun.sh
bash deploy/deploy-cloudrun.sh
```

### 2.3 Docker 本地构建

```bash
docker build -t v2ray-platform-control-plane .
docker run -p 8080:8080 \
  -e BOOTSTRAP_ADMIN_EMAIL=admin@example.com \
  -e BOOTSTRAP_ADMIN_PASSWORD=changeme \
  v2ray-platform-control-plane
```

### 2.4 已发布镜像

推送到 `main` 后，`.github/workflows/deploy.yml` 会把 control-plane 镜像
发布到：

```text
ghcr.io/<your-github-owner>/v2ray-platform-control-plane
```

## 3. 首次登录与基础配置

### 3.1 登录管理界面

1. 打开部署后的 URL（例如 `https://control-plane.example.com`）
2. 使用 `BOOTSTRAP_ADMIN_EMAIL` 和 `BOOTSTRAP_ADMIN_PASSWORD` 登录
3. **重要**：首次登录后立即修改密码

### 3.2 创建节点

1. 进入 **Nodes** 标签页
2. 点击 **＋ Add Node**
3. 填写信息：
   - **Node Name**：节点名称 (如 `sg-1`)
   - **Region**：区域 (如 `ap-southeast-1`)
   - **Public Host**：服务器公网 IP 或域名
   - **Tags**：标签 (可选，逗号分隔)
4. 点击 **Generate install command**
5. 复制生成的命令到服务器执行

### 3.3 安装 Node Agent

在代理服务器上执行生成的命令：

```bash
curl -fsSL "https://<your-cp>/install.sh?token=<TOKEN>&name=<NAME>&region=<REGION>" | bash
```

脚本会自动：
- 下载 node-agent 二进制文件
- 创建 systemd 服务
- 启动服务并注册到控制面板

### 3.4 验证节点状态

1. 在管理界面 **Nodes** 标签页查看节点状态
2. 正常节点显示 **online** 状态
3. 如果显示 **offline**，检查服务器防火墙和网络连接

## 4. 用户管理

### 4.1 创建会员

1. 进入 **Members** 标签页
2. 点击 **＋ Add member**
3. 填写会员信息：
   - **Name**：会员名称
   - **Email**：邮箱地址
   - **Note**：备注 (可选)
4. 点击 **Create**

### 4.2 授权会员访问节点

1. 在 **Grants** 标签页
2. 点击 **＋ Grant access**
3. 选择会员和节点
4. 点击 **Grant**

或使用批量授权：
1. 在 **Nodes** 标签页选择节点
2. 在 **Members** 标签页选择会员
3. 使用 **Grant** 功能批量授权

### 4.3 配置带宽配额 (Tier)

1. 进入 **Tiers** 标签页
2. 点击 **＋ Add tier**
3. 配置：
   - **Name**：套餐名称 (如 `Standard`)
   - **Description**：描述 (如 `50 GB/month`)
   - **Quota (GB)**：带宽限额
   - **Type**：
     - `Monthly`：每月重置
     - `Fixed`：一次性总量
4. 点击 **Create**

### 4.4 分配套额给会员

1. 在 **Members** 标签页找到目标会员
2. 点击 **Edit**
3. 选择 **Tier**
4. 可选：设置 **Quota Override (GB)** 覆盖套餐默认值
5. 点击 **Save**

## 5. Clash 订阅配置

### 5.1 获取订阅链接

1. 在 **Members** 标签页找到会员
2. 点击 **Sub🔗** 复制订阅链接
3. 格式：`https://<your-cp>/sub/<subscription_token>/clash.yaml`

### 5.2 配置 Clash 客户端

1. 打开 Clash 客户端
2. 进入 **Profiles** 或 **Remote Profile**
3. 粘贴订阅链接
4. 点击 **Download** 或 **Update**
5. 启用代理

### 5.3 查看流量使用

Clash 客户端会自动显示：
- **Upload**：上行流量
- **Download**：下行流量
- **Total**：总限额
- **Expire**：到期时间

## 6. 节点管理

### 6.1 节点状态说明

| 状态 | 含义 |
|-----|------|
| **online** | 节点正常运行，心跳正常 |
| **offline** | 节点离线，心跳超时 (默认 15 分钟) |
| **provisioning** | 节点正在配置中 |
| **degraded** | 节点性能下降 |

### 6.2 重建节点配置

当修改节点配置后，需要重建：

1. 在 **Nodes** 标签页选择节点
2. 点击 **↺ Rebuild** 按钮
3. 等待节点拉取新配置

### 6.3 节点配置回滚

1. 点击节点的 **Config** 按钮
2. 查看配置历史 (保留最近 3 个版本)
3. 选择要回滚的版本
4. 点击 **Rollback**

### 6.4 删除节点

1. 在 **Nodes** 标签页选择节点
2. 点击 **✕** 按钮
3. 确认删除

## 7. 告警与监控

### 7.1 查看告警

1. 进入 **Logs & Alerts** 标签页
2. 查看 **Alerts** 区域：
   - **Node heartbeat overdue**：节点心跳超时
   - **Latest config sync failed**：配置同步失败
   - **Member quota exceeded**：会员配额超限
   - **Member expired**：会员已过期

### 7.2 配置 Webhook 告警

设置环境变量：
```bash
export CONTROL_PLANE_ALERT_WEBHOOK_URL=https://your-webhook-url
```

### 7.3 审计日志

在 **Logs & Alerts** 标签页查看：
- **Audit logs**：记录所有管理操作
- 支持导出 CSV

## 8. CloudFront 集成 (可选)

### 8.1 配置 AWS 凭证

CloudFront 凭证存储要求 control-plane 配置 `CLOUDFRONT_MASTER_KEY`。默认
SSH 服务器部署会把它生成到 `/opt/v2ray-platform/.env.server`；Cloud Run
部署需要把它作为稳定 secret 提供。

1. 进入 **CloudFront** 标签页
2. 填写：
   - **Access Key ID**
   - **Secret Access Key**
   - **Region**
3. 点击 **Save**

### 8.2 绑定现有 CloudFront 分发

1. 点击 **Scan distributions** 扫描可用分发
2. 选择目标分发
3. 点击 **Bind**

### 8.3 同步路由

1. 查看 **Sync Preview** 预览变更
2. 点击 **Sync** 执行同步
3. 等待 CloudFront 部署完成

### 8.4 CloudFront 订阅

同步完成后，会员可使用 CloudFront 订阅：
- 点击 **CF↓** 下载 Clash 配置
- 或点击 **Sub CF🔗** 复制订阅链接

## 9. 常用 API

### 9.1 管理员 API

```bash
# 登录获取 Session Token
curl -X POST http://localhost:8080/api/admin/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme"}'

# 列出节点
curl http://localhost:8080/api/admin/nodes \
  -H 'Authorization: Bearer <session-token>'

# 列出会员
curl http://localhost:8080/api/admin/members \
  -H 'Authorization: Bearer <session-token>'

# 创建授权
curl -X POST http://localhost:8080/api/admin/grants \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <session-token>' \
  -d '{"member_id":"<member-id>","node_id":"<node-id>"}'
```

### 9.2 节点 Agent API

```bash
# 心跳上报
curl -X POST http://localhost:8080/api/agent/heartbeat \
  -H 'Authorization: Bearer <node-token>' \
  -H 'Content-Type: application/json' \
  -d '{"applied_config_version":1,"public_host":"1.2.3.4"}'

# 上报流量使用
curl -X POST http://localhost:8080/api/agent/usage \
  -H 'Authorization: Bearer <node-token>' \
  -H 'Content-Type: application/json' \
  -d '{"snapshots":[{"credential_uuid":"xxx","uplink":1234,"downlink":5678}]}'
```

## 10. 故障排查

### 10.1 节点无法连接

1. 检查节点状态是否为 **online**
2. 检查服务器防火墙是否开放端口
3. 查看 node-agent 日志：
   ```bash
   journalctl -u v2ray-platform-node-agent -f
   ```

### 10.2 流量统计不显示

1. 确认 `NODE_USAGE_SOURCE=runtime`
2. 确认 V2Ray 统计 API 已启用 (端口 10085)
3. 手动测试统计命令：
   ```bash
   /usr/local/v2ray/v2ray api stats --server=127.0.0.1:10085 -json
   ```

### 10.3 配置同步失败

1. 在 **Logs & Alerts** 查看具体错误信息
2. 检查节点网络连接
3. 尝试手动重建配置

### 10.4 数据库连接问题

```bash
# 测试数据库连接
psql $DATABASE_URL -c "SELECT 1"

# 检查连接池状态
curl http://localhost:8080/api/admin/debug/health
```

## 11. 维护操作

### 11.1 备份数据库

```bash
# Neon 用户
# 在 Neon 控制台使用 Backup 功能

# 自建 PostgreSQL
pg_dump $DATABASE_URL > backup_$(date +%Y%m%d).sql
```

### 11.2 更新 Node Agent

Node Agent 支持自动更新：
1. 推送新版本到 GitHub Releases
2. Agent 在下次心跳时自动检测并更新
3. 更新后自动重启

### 11.3 更新 Control Plane

```bash
# 发布新的 control-plane 镜像
git push origin main

# 或者在你自己的运行环境里更新代码/镜像后重启 control-plane
```

## 12. 环境变量参考

### Control Plane

| 变量 | 默认值 | 说明 |
|-----|-------|------|
| `DATABASE_URL` | - | PostgreSQL 连接字符串 |
| `BOOTSTRAP_ADMIN_EMAIL` | - | 首次启动创建的管理员邮箱 |
| `BOOTSTRAP_ADMIN_PASSWORD` | - | 首次启动创建的管理员密码 |
| `CONTROL_PLANE_SESSION_SECRET` | 启动时自动生成 | 会话签名密钥。生产环境请设置稳定随机值；未设置时重启会使现有会话失效。 |
| `CLOUDFRONT_MASTER_KEY` | SSH 服务器部署自动生成 | 用于加密已保存 CloudFront AWS 凭证的 32 字符密钥 |
| `PORT` | 8080 | 监听端口 |
| `CONTROL_PLANE_NODE_OFFLINE_SECONDS` | 900 | 节点离线判定时间 (秒) |

### Node Agent

| 变量 | 默认值 | 说明 |
|-----|-------|------|
| `CONTROL_PLANE_URL` | - | 控制面板地址 |
| `BOOTSTRAP_TOKEN` | - | 首次启动的引导令牌 |
| `NODE_NAME` | - | 节点名称 |
| `NODE_REGION` | - | 节点区域 |
| `NODE_PUBLIC_HOST` | - | 节点公网地址 |
| `NODE_USAGE_SOURCE` | disabled | 流量统计来源 (runtime/file) |
| `NODE_USAGE_QUERY_SERVER` | 127.0.0.1:10085 | V2Ray 统计 API 地址 |
