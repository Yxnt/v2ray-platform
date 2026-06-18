# v2ray-platform Operation Guide

## 1. System Architecture Overview

```text
┌─────────────────────────────────────────────────────────────────┐
│                        Control Plane                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │   Admin UI  │  │   REST API  │  │  PostgreSQL │            │
│  └─────────────┘  └─────────────┘  └─────────────┘            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ Node Configuration
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Node Agent (per proxy server)              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │   V2Ray     │  │   Agent     │  │  Stats API  │            │
│  │   Config    │  │   Daemon    │  │   10085     │            │
│  └─────────────┘  └─────────────┘  └─────────────┘            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ Clash Subscription
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Users (Clash Client)                       │
└─────────────────────────────────────────────────────────────────┘
```

## 2. Running Control Plane

### 2.1 Local Development Mode

```bash
# Set environment variables
export BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export BOOTSTRAP_ADMIN_PASSWORD=change-me-now

# Start control plane (using in-memory store)
go run ./cmd/control-plane
```

Open http://localhost:8080 to access the admin UI.

### 2.2 Production Deployment (Default: SSH Server + GHCR)

#### Step 1: Prepare a Linux server with Docker Compose

1. Ensure the target host is reachable over SSH
2. Ensure `docker` and `docker compose` are installed on the target host
3. Decide whether to use the bundled Postgres container or an external `DATABASE_URL`

#### Step 2: Load the server deploy environment

```bash
cp deploy/server.env.example /tmp/v2ray-platform-server.env
# edit the copied file with real values
. /tmp/v2ray-platform-server.env
```

Important variables:

- `DEPLOY_HOST`
- `DEPLOY_PATH`
- `CONTROL_PLANE_PUBLIC_URL`
- `CONTROL_PLANE_IMAGE` default: `ghcr.io/yxnt/v2ray-platform-control-plane:latest`
- `POSTGRES_RESTORE_DUMP` optional local dump path to restore on first deploy

#### Step 3: Run preflight and deploy

```bash
bash deploy/preflight-auto.sh
bash deploy/deploy-auto.sh
```

The deploy script uploads the repo, writes `.env.server`, restores the optional
Postgres dump, pulls the GHCR image, and starts the control-plane container.

#### Optional: Cloud Run deployment

```bash
bash deploy/preflight-cloudrun.sh
bash deploy/deploy-cloudrun.sh
```

### 2.3 Docker Local Build

```bash
docker build -t v2ray-platform-control-plane .
docker run -p 8080:8080 \
  -e BOOTSTRAP_ADMIN_EMAIL=admin@example.com \
  -e BOOTSTRAP_ADMIN_PASSWORD=changeme \
  v2ray-platform-control-plane
```

### 2.4 Published image

Push to `main` and `.github/workflows/deploy.yml` publishes the control-plane
image to:

```text
ghcr.io/<your-github-owner>/v2ray-platform-control-plane
```

## 3. First Login & Basic Configuration

### 3.1 Login to Admin UI

1. Open the deployed URL (for example `https://control-plane.example.com`)
2. Login with `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD`
3. **Important**: Change password immediately after first login

### 3.2 Create Node

1. Go to **Nodes** tab
2. Click **＋ Add Node**
3. Fill in the information:
   - **Node Name**: Node identifier (e.g., `sg-1`)
   - **Region**: Region (e.g., `ap-southeast-1`)
   - **Public Host**: Server public IP or domain
   - **Tags**: Tags (optional, comma-separated)
4. Click **Generate install command**
5. Copy the generated command and run on the server

### 3.3 Install Node Agent

Run the generated command on the proxy server:

```bash
curl -fsSL "https://<your-cp>/install.sh?token=<TOKEN>&name=<NAME>&region=<REGION>" | bash
```

The script will automatically:
- Download the node-agent binary
- Create systemd service
- Start the service and register with control plane

### 3.4 Verify Node Status

1. Check node status in the admin UI **Nodes** tab
2. Normal nodes show **online** status
3. If showing **offline**, check server firewall and network connection

## 4. User Management

### 4.1 Create Member

1. Go to **Members** tab
2. Click **＋ Add member**
3. Fill in member information:
   - **Name**: Member name
   - **Email**: Email address
   - **Note**: Notes (optional)
4. Click **Create**

### 4.2 Grant Member Access to Nodes

1. Go to **Grants** tab
2. Click **＋ Grant access**
3. Select member and node
4. Click **Grant**

Or use batch authorization:
1. Select nodes in **Nodes** tab
2. Select members in **Members** tab
3. Use **Grant** function for batch authorization

### 4.3 Configure Bandwidth Quota (Tier)

1. Go to **Tiers** tab
2. Click **＋ Add tier**
3. Configure:
   - **Name**: Plan name (e.g., `Standard`)
   - **Description**: Description (e.g., `50 GB/month`)
   - **Quota (GB)**: Bandwidth limit
   - **Type**:
     - `Monthly`: Resets monthly
     - `Fixed`: One-time total
4. Click **Create**

### 4.4 Assign Quota to Members

1. Find the target member in **Members** tab
2. Click **Edit**
3. Select **Tier**
4. Optional: Set **Quota Override (GB)** to override plan default
5. Click **Save**

## 5. Clash Subscription Configuration

### 5.1 Get Subscription Link

1. Find the member in **Members** tab
2. Click **Sub🔗** to copy subscription link
3. Format: `https://<your-cp>/sub/<subscription_token>/clash.yaml`

### 5.2 Configure Clash Client

1. Open Clash client
2. Go to **Profiles** or **Remote Profile**
3. Paste the subscription link
4. Click **Download** or **Update**
5. Enable proxy

### 5.3 View Traffic Usage

Clash client automatically displays:
- **Upload**: Uplink traffic
- **Download**: Downlink traffic
- **Total**: Total quota
- **Expire**: Expiration time

## 6. Node Management

### 6.1 Node Status Explanation
