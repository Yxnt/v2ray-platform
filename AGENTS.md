<!-- pensieve:instructions:start -->
## How To Use Pensieve

Use `.pensieve/` as the first source of architectural intent.

- `maxims/` are active engineering rules.
- `decisions/` are active project decisions.
- `knowledge/` explains boundary maps and debugging paths.
- `pipelines/` gives executable workflows.

Use these project pipelines directly when trigger words match; do not rediscover them through skills first.

- Commit requests (`commit`, `提交`, `git commit`): use `.pensieve/pipelines/run-when-committing.md`. Check staged diff, decide whether reusable insight should be captured, then make atomic commits.
- Refactor requests (`重构`, `refactor`, `大改`, `拆代码`): use `.pensieve/pipelines/run-when-refactoring.md`. Confirm the real problem, fix upstream data authority first, split large work into 2-3 user-visible steps, delete old paths when new paths work, and avoid compatibility/fallback branches.
- Review requests (`review`, `代码审查`, `检查代码`): use `.pensieve/pipelines/run-when-reviewing-code.md`. Start from git history and changed hot spots, verify candidate issues, and report only high-signal findings with evidence and file locations.
<!-- pensieve:instructions:end -->

## CloudFront Workstream

- Before implementing CloudFront support, read the spec at `docs/superpowers/specs/2026-06-05-cloudfront-subscription-design.md` and the plan at `docs/superpowers/plans/2026-06-05-cloudfront-subscription-implementation.md`.
- If starting from a clean or mostly untouched branch, execute `docs/superpowers/plans/2026-06-05-cloudfront-subscription-implementation.md` task-by-task.
- If continuing an already-started CloudFront branch, read `docs/superpowers/plans/2026-06-09-cloudfront-support-closure.md` first; it records current branch status, hardening checks, and the local closure gates. Real AWS verification is a deferred production validation item unless the task explicitly asks for it.
- Treat the current branch as a partial implementation, not a greenfield build, whenever CloudFront diffs already exist. Inspect `git status --short` and the diff under `internal/cloudfront`, `internal/api/controlplane.go`, `internal/api/web/index.html`, and `internal/store` before making new changes.
- Do not restart completed plan tasks just because they are listed in the original implementation plan. Verify the current code against the plan, then continue from the first failing, missing, or unverified task.
- Preserve direct subscription behavior as a hard rule. `GET /sub/{token}/clash.yaml` and `GET /api/admin/members/{memberID}/clash.yaml` must remain backward compatible while CloudFront support is added.
- CloudFront phase one is a control-plane feature. Do not change node-agent registration, heartbeat, usage reporting, or runtime user add/remove flows unless a task explicitly calls for it.
- CloudFront create-new distribution support is part of phase one, alongside scan-and-bind of existing distributions.
- CloudFront routing must be modeled with origins plus cache behaviors. Do not use an origin-only mental model when planning or syncing routes.
- Use stable node `route_key` for CloudFront paths. Never derive CloudFront paths from `node.name`.
- Build AWS CloudFront clients from decrypted database config at request time. Do not rely on boot-time singletons for CloudFront access.
- CloudFront AWS REST requests must sign with `us-east-1`/`cloudfront` even if stored UI/config region differs.
- Prefer distribution auto-discovery over manual `distribution_id` entry. The normal admin UX should be scan, select, bind, plan, then sync.
- Distribution discovery must follow paginated CloudFront list responses, not just the first page.
- Credential updates must not allow partial secret replacement. AK/SK must be supplied together, and `sessionToken` must not be updated by itself.
- Treat live AWS distribution state as authoritative for CloudFront plan/sync. Stored `origins_json` is a scan cache, not proof that remote state is unchanged.
- Changing the bound distribution must invalidate previous successful sync markers so `clash-cf.yaml` cannot become available on stale success state.
- Changing CloudFront bindings or the desired route set must also invalidate previous successful sync markers. Do not expose routes that have not been synced to CloudFront yet.
- CloudFront path behaviors select the origin by stable `route_key`, then the platform-managed CloudFront Function `v2ray-platform-route-rewrite` rewrites the request URI to the node's actual direct WebSocket path (`/{node.name}`) before origin fetch. Do not remove this rewrite unless node runtime path handling is redesigned.
- Keep CloudFront ownership boundaries strict. Only mutate origins and behaviors that the platform can positively identify as platform-managed.
- Do not expose CloudFront subscription export to users until a distribution is bound and at least one successful sync has completed.
- Do not expose CloudFront subscription export when the stored drift status is `drifted` or `conflict`; require sync to resolve it first.
- Every CloudFront endpoint or sync flow should ship with focused tests in the same task that introduces it.
- Verification for CloudFront work should at minimum run `GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront ./internal/api ./internal/store` before claiming completion, then `GOCACHE=/private/tmp/v2ray-platform-go-build go test ./...` if the focused suite passes.
- For local CloudFront logic closure, real AWS credentials/network are not required unless the task explicitly asks for them. Do not claim production AWS API acceptance until real AWS scan/bind/create/sync has been verified; otherwise record it as deferred production validation.
- Custom entry host alias/DNS validation is deferred production validation unless a task explicitly requests it; local subscription rendering may trust the configured custom entry host after CloudFront sync gates pass.
