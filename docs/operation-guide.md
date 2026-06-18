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

| Status | Meaning |
|--------|---------|
| **online** | Node running normally, heartbeat OK |
| **offline** | Node offline, heartbeat timeout (default 15 minutes) |
| **provisioning** | Node being configured |
| **degraded** | Node performance degraded |

### 6.2 Rebuild Node Configuration

After modifying node configuration, rebuild is required:

1. Select node in **Nodes** tab
2. Click **↺ Rebuild** button
3. Wait for node to pull new configuration

### 6.3 Node Configuration Rollback

1. Click **Config** button on the node
2. View configuration history (keeps last 3 versions)
3. Select version to rollback
4. Click **Rollback**

### 6.4 Delete Node

1. Select node in **Nodes** tab
2. Click **✕** button
3. Confirm deletion

## 7. Alerts & Monitoring

### 7.1 View Alerts

1. Go to **Logs & Alerts** tab
2. View **Alerts** section:
   - **Node heartbeat overdue**: Node heartbeat timeout
   - **Latest config sync failed**: Configuration sync failed
   - **Member quota exceeded**: Member quota exceeded
   - **Member expired**: Member expired

### 7.2 Configure Webhook Alerts

Set environment variable:
```bash
export CONTROL_PLANE_ALERT_WEBHOOK_URL=https://your-webhook-url
```

### 7.3 Audit Logs

View in **Logs & Alerts** tab:
- **Audit logs**: Records all management operations
- Supports CSV export

## 8. CloudFront Integration (Optional)

### 8.1 Configure AWS Credentials

1. Go to **CloudFront** tab
2. Fill in:
   - **Access Key ID**
   - **Secret Access Key**
   - **Region**
3. Click **Save**

### 8.2 Bind Existing CloudFront Distribution

1. Click **Scan distributions** to scan available distributions
2. Select target distribution
3. Click **Bind**

### 8.3 Sync Routes

1. View **Sync Preview** to preview changes
2. Click **Sync** to execute sync
3. Wait for CloudFront deployment to complete

### 8.4 CloudFront Subscription

After sync completes, members can use CloudFront subscription:
- Click **CF↓** to download Clash configuration
- Or click **Sub CF🔗** to copy subscription link

## 9. Common APIs

### 9.1 Admin API

```bash
# Login to get Session Token
curl -X POST http://localhost:8080/api/admin/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme"}'

# List nodes
curl http://localhost:8080/api/admin/nodes \
  -H 'Authorization: Bearer <session-token>'

# List members
curl http://localhost:8080/api/admin/members \
  -H 'Authorization: Bearer <session-token>'

# Create grant
curl -X POST http://localhost:8080/api/admin/grants \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <session-token>' \
  -d '{"member_id":"<member-id>","node_id":"<node-id>"}'
```

### 9.2 Node Agent API

```bash
# Heartbeat
curl -X POST http://localhost:8080/api/agent/heartbeat \
  -H 'Authorization: Bearer <node-token>' \
  -H 'Content-Type: application/json' \
  -d '{"applied_config_version":1,"public_host":"1.2.3.4"}'

# Report traffic usage
curl -X POST http://localhost:8080/api/agent/usage \
  -H 'Authorization: Bearer <node-token>' \
  -H 'Content-Type: application/json' \
  -d '{"snapshots":[{"credential_uuid":"xxx","uplink":1234,"downlink":5678}]}'
```

## 10. Troubleshooting

### 10.1 Node Cannot Connect

1. Check if node status is **online**
2. Check server firewall ports
3. View node-agent logs:
   ```bash
   journalctl -u v2ray-platform-node-agent -f
   ```

### 10.2 Traffic Statistics Not Showing

1. Confirm `NODE_USAGE_SOURCE=runtime`
2. Confirm V2Ray stats API is enabled (port 10085)
3. Manually test stats command:
   ```bash
   /usr/local/v2ray/v2ray api stats --server=127.0.0.1:10085 -json
   ```

### 10.3 Configuration Sync Failed

1. Check specific error in **Logs & Alerts**
2. Check node network connection
3. Try manual configuration rebuild

### 10.4 Database Connection Issues

```bash
# Test database connection
psql $DATABASE_URL -c "SELECT 1"

# Check connection pool status
curl http://localhost:8080/api/admin/debug/health
```

## 11. Maintenance Operations

### 11.1 Backup Database

```bash
# Neon users
# Use Backup feature in Neon console

# Self-hosted PostgreSQL
pg_dump $DATABASE_URL > backup_$(date +%Y%m%d).sql
```

### 11.2 Update Node Agent

Node Agent supports auto-update:
1. Push new version to GitHub Releases
2. Agent automatically detects and updates on next heartbeat
3. Automatically restarts after update

### 11.3 Update Control Plane

```bash
# Publish a new control-plane image
git push origin main

# Or restart your own control-plane runtime after updating the code/image
```

## 12. Environment Variables Reference

### Control Plane

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | - | PostgreSQL connection string |
| `BOOTSTRAP_ADMIN_EMAIL` | - | Admin email created on first startup |
| `BOOTSTRAP_ADMIN_PASSWORD` | - | Admin password created on first startup |
| `CONTROL_PLANE_SESSION_SECRET` | Auto-generated | Session signing secret |
| `PORT` | 8080 | Listen port |
| `CONTROL_PLANE_NODE_OFFLINE_SECONDS` | 900 | Node offline threshold (seconds) |

### Node Agent

| Variable | Default | Description |
|----------|---------|-------------|
| `CONTROL_PLANE_URL` | - | Control plane URL |
| `BOOTSTRAP_TOKEN` | - | Bootstrap token for first run |
| `NODE_NAME` | - | Node name |
| `NODE_REGION` | - | Node region |
| `NODE_PUBLIC_HOST` | - | Node public address |
| `NODE_USAGE_SOURCE` | disabled | Traffic stats source (runtime/file) |
| `NODE_USAGE_QUERY_SERVER` | 127.0.0.1:10085 | V2Ray stats API address |
