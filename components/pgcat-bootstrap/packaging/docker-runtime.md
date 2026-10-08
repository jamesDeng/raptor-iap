# Docker on standard ECS

The owner selected standard ECS plus first-boot initialization instead of a
custom PgCat machine image. Terraform ESS user_data installs Ubuntu docker.io,
writes only nonsecret identity/version metadata and pulls the runtime by digest.
PgCat and the existing OSS bootstrap run in one container, as UID 65532 with a
read-only root filesystem, no capabilities, and private tmpfs for configuration.
Host networking supplies IMDSv2 access and private SQL/metrics ports. This is the
private POC's plaintext SQL mode; OSS reads remain HTTPS, version-pinned and SSE.

The Linux qualification workflow builds the existing pinned bootstrap/PgCat
binaries, packages them with this Dockerfile, tests missing metadata fails closed,
and exercises actual PostgreSQL operations plus idle/active/multiple-pool metrics
inside the container. Live ECS role retrieval remains a separate acceptance check.
After merge, dispatch on main with publish_container=true to publish the qualified
runtime to GHCR. The GHCR package must be public: first-created packages can
default private. Set package visibility to public once, then rerun publication.
The publisher verifies an anonymous digest pull before reporting a usable image.
No registry credential is delivered to ECS. Copy that sha256 reference into proxy_container_image;
bootstrap_revision must equal that image's baked source revision. No latest tag.

User data runs once on first launch. Docker's unless-stopped restart policy handles
reboots and process failures, re-fetching the same versioned configuration each
start. Cloud-init/package pull failures leave an unhealthy node; inspect cloud-init
and Docker status before counting it as ready. New nodes require working outbound
package/registry access and private OSS/RDS connectivity.

The old builder is stopped; its custom image and snapshot are unused retained
resources. Their Terraform states require separate explicit cleanup, not fleet
configuration changes. Do not delete image-state files and orphan their resources.
