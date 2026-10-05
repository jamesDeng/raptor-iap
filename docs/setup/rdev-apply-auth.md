# Rdev deployment identity

Created 2026-10-05 and independently read back. Role: raptor-iap-rdev-apply. Trust binds issuer token.actions.githubusercontent.com, audience sts.aliyuncs.com and exact immutable subject repo:jamesDeng@4443650/raptor-iap@1397422754:environment:rdev.ali-apply. Maximum session duration is 3600 seconds; check workflow requests 1800.

GitHub environment rdev.ali-apply permits main only and requires jamesDeng approval. Admin bypass is disabled. Self-review remains permitted because the owner is the POC operator. Repository/environment protection is part of the trust boundary; the OIDC subject does not itself contain a branch claim.

Attached policies: existing raptor-iap-rdev-plan-read metadata policy, and raptor-iap-rdev-apply-state. The latter reads/writes only the exact rdev state object and accesses shared terraform_lock rows. Shared lock risk was explicitly accepted for this POC. No cloud creation policy is attached. State contents may contain secrets and must never be printed or uploaded as public artifacts.

The manually dispatched rdev-apply-oidc-check workflow runs only on main. It verifies temporary credentials through account discovery without provisioning infrastructure. Environment approval is required before its job starts. Until it runs successfully, live OIDC exchange for this role is unverified. The check does not verify OSS state access or OTS row locking; those remain a separate live check.

The initial creation policy was accepted by native RAM CreatePolicy and independently read back with zero attachments. Numeric condition values must use string encoding. This is policy acceptance, not live creation authorization or an assurance that all provider follow-up calls will succeed. Cloud creation awaits preflight and saved-plan review.

NAT deletion protection scope was resolved using the official AliyunNATGatewayFullAccess policy and live GetPolicyVersion readback: vpc:DeletionProtection authorizes natgateway resources. Future follow-up policy should use the actual NAT ID. Source: https://help.aliyun.com/zh/ram/developer-reference/aliyunnatgatewayfullaccess .
