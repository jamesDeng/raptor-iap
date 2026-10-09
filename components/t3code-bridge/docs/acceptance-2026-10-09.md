# Live acceptance — 2026-10-09

The provided existing operator credential file authenticated successfully. Actual pinned T3 Local ACP registration used a private local installation. No credentials or browser tokens were printed or committed.

Live testing exposed a decoder bug: Raptor responses use a data envelope. The regression test failed with the actual response shape; decoder and fixtures were corrected. Race suite, vet, build and official ACP SDK tests passed.

Two questions against poc-traffic-client/A00003 failed with MissingEvidence. Separate deployment inspection confirmed that application has no current deployment. Both failures reported verified checkpoints, all cleanup checks true and no recovery requirement; T3 showed failures. Acceptance switched to existing raptor-backend/A00001 in rdev.ali with a new state directory for its immutable selector binding.

Successful request `0e8fee21-29ac-493a-87c0-a46df39588cb`, attempt `4902cbf5-ef55-40b1-a3f6-cad7bc8c97cd`: completed/live, selected and actual model gpt-5.6-luna, fresh Raptor and Infra evidence, verified checkpoint, all cleanup checks true, recoveryNeeded false. T3 rendered the answer and request link.

Actual T3 Stop action cancelled request `8b5cbdf8-8805-46a9-81f5-18f72047c0e7`, attempt `c2f21391-3124-421d-b7e7-27753ed68595`: cancelled/live, verified checkpoint, all cleanup checks true, recoveryNeeded false. Reload preserved the answer and Cancelled history. Separate request-list comparison: 17 records before and after, zero new request IDs.

[Sanitized receipt](acceptance-2026-10-09.json). Cleanup was checked through separate authenticated Raptor execution inspection. Direct AX node SSH inspection timed out; no additional independent node-level absence audit is claimed.

No Terraform, IAM, Gateway provider routing, deployment configuration or model credentials were changed. No accounts were created or reactivated. Ordinary request records and runtime checkpoints are expected execution side effects.

Private local installation: primary repository .raptor-local/t3code-live/start.sh, loopback port 3779. It includes pinned T3 runtime, bridge binary, private config and state. One open T3 thread per provider state directory; each prompt is an independent question.
