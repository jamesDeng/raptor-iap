# ECS AX migration execution amendment

User chose current-session execution and authorizes one ECS; no new approval needed for that direction.

1. Confirm image, capacity and cost. Add isolated Terraform ecs-ax module/root with account/subnet guard, single instance, encrypted disk, restricted SSH and private service access. Mock tests and reviewed live plan before apply.
2. SSH to the new instance only. Install checksum-verified K3s1.37 and inspect native certificate APIs; bootstrap signer and test native projections before expensive rollout.
3. Select and test immutable AX/Substrate pair, build on linux/amd64 using private node-local registry. Adapt development manifests for persistent single-node storage and gVisor. Install Substrate, verify counter actor.
4. Install AX, durable Redis and pinned Pi0.99.2 image. Verify workspace suspend/resume and model/API routes.
5. Complete existing gateway LiveRuntime driver and portable checkpoint bridge, trusted configuration and meaningful lifecycle tests.
6. Run original design's acceptance and rollback checks, then switch dispatch. Record resource/cost receipts and one fresh branch review.

Read exact manifests and install scripts before execution. Unexpected dependency or feature support stops the dependent stage, not unrelated read-only research. Never report deployment/migration completed without observed acceptance.
