# ECS AX migration execution amendment

User chose current-session execution and authorizes one ECS; no new approval needed for that direction.

1. Confirm image, capacity and cost. Add isolated Terraform ecs-ax module/root with account/subnet guard, single instance, encrypted disk, restricted SSH and private service access. Mock tests and reviewed live plan before apply.
2. SSH to the new instance only. Install checksum-verified K3s1.37 and inspect native certificate APIs; bootstrap signer and test native projections before expensive rollout.
3. Select and test immutable AX/Substrate pair, build on linux/amd64 using private node-local registry. Adapt development manifests for persistent single-node storage and gVisor. Install Substrate, verify counter actor.
4. Install AX, durable Redis and pinned Pi0.99.2 image. Verify workspace suspend/resume and model/API routes.
5. Complete existing gateway LiveRuntime driver and portable checkpoint bridge, trusted configuration and meaningful lifecycle tests.
6. Run original design's acceptance and rollback checks, then switch dispatch. Record resource/cost receipts and one fresh branch review.

Read exact manifests and install scripts before execution. Unexpected dependency or feature support stops the dependent stage, not unrelated read-only research. Never report deployment/migration completed without observed acceptance.

Task5 integration refinement: a node-local runtime bridge speaks the inspected typed AX/Substrate/guest API. Gateway transport is authenticated private HTTPS with mutual TLS, deterministic request/attempt names and no automatic write retries. Cloud credentials stay at the gateway. Gate tests: mismatched binding/ownership, missing TLS identity, ambiguous creates/starts, expired deadline/lease, no inference replay on reconcile, bounded file/event reads, exact actor/task deletion, checkpoint publication only after encryption/checksum verification. New runtime remains opt-in until these and the original acceptance pass.

## Multi-provider amendment

User requires retaining Aliyun Sandbox. Add a durable provider router (`aliyun` default, `ax` opt-in) before completing AX integration. Provider selection is per attempt and immutable; restart recovery follows the journal and legacy untagged attempts use Aliyun. Test default changes during an active attempt, stale ownership, unknown/conflicting providers, missing configured backend and no fallback on backend errors. Keep existing Aliyun tests green and run the same lifecycle acceptance contract for AX. Switching the rdev.ali default does not remove the Aliyun backend.
