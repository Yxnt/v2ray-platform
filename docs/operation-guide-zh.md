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
