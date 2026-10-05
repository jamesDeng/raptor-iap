# Foundation execution ledger

2026-10-05: Approved native execution. Branch feat/rdev-foundation based on 5d4d9b4. Native worktree registration unavailable because the calling project root is not a Git repo; reused established ignored manual-worktree location. PR5/6 still open; no merge authorized.

Ruling: prepare foundation from PR6 but block release apply until source integration is resolved. This preserves public GitOps desired state and avoids silently deploying unmerged platform code.

Verification: five offline preflight tests pass; Raptor HTTP adapter and MCP baseline tests pass with loopback enabled. Foundation provider validation passes and mocked contract passes (one run). Provider desired_size is a string; contract corrected after inspecting verbose mock output. Four-core worker in stock in Singapore a/b/c/d at check time.

Ruling: gateway main currently has only simulation worker wiring; do not advertise live Pi agent execution from cloud deployment. The later runtime integration task must wire the real adapter before claiming end-to-end agent acceptance.

Remaining: complete price/permission evidence and remote state; live ACK version check; source integration decision; Helm/Argo CD manifests and image build verification; cloud apply and live acceptance. No cloud resources created.
