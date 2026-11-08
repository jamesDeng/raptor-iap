# The timer is owned by the Infra API component. It invokes the existing
# custom-runtime function through FC, with no public API Gateway route.
resource "alicloud_fcv3_trigger" "pgcat_monitoring" {
  count          = var.enable_pgcat_monitoring ? 1 : 0
  function_name  = var.function_name
  trigger_name   = "raptor-rdev-pgcat-monitoring"
  trigger_type   = "timer"
  qualifier      = "LATEST"
  description    = "Reconcile the bound PgCat Prometheus target inventory"
  trigger_config = jsonencode({ cronExpression = "@every 1m", enable = true, payload = "pgcat-monitoring-v1" })
  depends_on     = [terraform_data.account_guard]
}
