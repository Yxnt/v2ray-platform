# Subscription & Tiers

## Clash Subscription URL

Every member has a unique, permanent **subscription token**. The Clash subscription URL is:

```
https://<control-plane-host>/sub/<subscription_token>/clash.yaml
```

Users paste this URL into Clash (or any compatible client) under **Remote Profile**. The client fetches the latest proxy list automatically — no manual updates needed when nodes are added or removed.

The subscription response includes a `Subscription-Userinfo` header so Clash displays current usage and quota:

```
upload=N; download=N; total=N; expire=N
```

- `total` is omitted when the member has no quota (unlimited)
- Values are in bytes; Clash auto-converts to GB for display

## Tiers

Tiers define bandwidth quotas assignable to members.

### Create a tier

From the admin UI: **Tiers → Add**. Or via API:

```sh
curl -sS \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <session token>' \
  -d '{"name":"Standard","description":"50 GB/month","quota_bytes":53687091200,"quota_type":"monthly"}' \
  http://127.0.0.1:8080/api/admin/tiers
```

`quota_type` options:
- `monthly` — resets on the 1st of each calendar month
- `fixed` — total lifetime quota, no reset

### Assign a tier to a member

From the **Members** panel → **Edit** → select a tier.

Members without a tier have **unlimited** bandwidth.

### Quota override

Individual members can have a `quota_bytes_limit` that overrides their tier quota. Set it in the **Edit** modal (displayed in GB).

## Proxy Chain (dialer-proxy)

Some nodes may be unreachable directly from users in certain regions. You can route them through a relay node:

1. In the admin UI, select the target node and use **Set proxy** to choose a relay node.
2. The relay node's name will appear as `dialer-proxy` in the generated Clash YAML.
3. The relay node is automatically included in the proxy list even if the member has no direct grant to it.

Node display name in Clash: `<Region> - <Name>` if a region is set, otherwise just `<Name>`.
