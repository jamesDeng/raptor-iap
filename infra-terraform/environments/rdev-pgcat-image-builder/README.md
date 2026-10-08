# Temporary rdev PgCat image builder

Terraform entrypoint for the image-builder submodule owned by PgCat. Separate state rdev.ali/pgcat-image-builder/terraform.tfstate. One private PostPaid ECS and empty-ingress security group; no RAM role, passwords, userdata or public bandwidth. Reuse existing VPC/subnet without owning or changing them.

Verified compute plus 20 GB disk quote CNY0.28114/hour, max two-hour builder work window. Do not leave running when blocked. No snapshots or images are created by this configuration. Capture/retention costs and permissions are a separate gate. Destroy only this temporary module after image capture; preserve the resulting image/snapshot and other rdev resources.

The module's running input defaults false. The prepared builder is economically stopped after installation and qualification; its disk remains billed. Set running=true only for a bounded image-preparation window, then return false. This keeps Terraform from silently restarting a CLI-stopped builder. Workbench can automatically add SSH rules; use the Cloud Assistant management channel instead. Terraform removed blocks in workbench-cleanup.tf record restoration of this builder's empty-ingress boundary.
