# PgCat IAM bootstrap state

This temporary entrypoint calls the IAM submodule inside terraform-module/pgcat-ess. It declares no independent role or policy resources. PgCat's runtime scaling configuration uses the same module's role output.

Existing resources remain in separate OSS state rdev.ali/pgcat-iam/terraform.tfstate until the test fleet is ready. Moved blocks migrate the original top-level resource addresses into module.config_iam with no cloud changes.

Before enabling the full test stack, transfer these three state entries to module.proxy[0].module.config_iam in its dedicated state and remove ownership from this bootstrap state. Never apply both states against the same role. bootstrap_reviewed remains false while runtime qualification and this state handoff are pending. Do not delete/recreate the role or grant wider permissions.
