locals {
  proxy_routes = var.enable_proxy_commands ? {
    scale          = "/v1/db-proxy-scale"
    protection     = "/v1/db-proxy-node-protection"
    deregistration = "/v1/db-proxy-node-deregistration"
  } : {}
  # Enumeration must detect duplicate tag matches, so this one read spans SG
  # groups in the selected account. Membership and every mutation stay pinned.
  proxy_statements = !var.enable_proxy_commands ? [] : [
    { Effect = "Allow", Action = ["ess:DescribeScalingGroups"], Resource = ["acs:ess:${var.region}:${var.account_id}:scalinggroup/*"] },
    { Effect = "Allow", Action = ["ess:DescribeScalingInstances", "ess:ModifyScalingGroup", "ess:SetInstancesProtection"], Resource = sort(distinct([for p in values(var.proxy_targets) : "acs:ess:${var.region}:${var.account_id}:scalinggroup/${p.group_id}"])) },
    { Effect = "Allow", Action = ["nlb:ListServerGroupServers"], Resource = sort(distinct(flatten([for p in values(var.proxy_targets) : ["acs:nlb:${var.region}:${var.account_id}:serverGroup/${p.server_group_id}", "acs:nlb:${var.region}:${var.account_id}:servergroup/${p.server_group_id}"]]))) },
    { Effect = "Allow", Action = ["nlb:RemoveServersFromServerGroup"], Resource = sort(distinct([for p in values(var.proxy_targets) : "acs:nlb:${var.region}:${var.account_id}:servergroup/${p.server_group_id}"])) },
    { Effect = "Allow", Action = ["nlb:GetListenerAttribute", "nlb:GetListenerHealthStatus"], Resource = sort(distinct([for p in values(var.proxy_targets) : "acs:nlb:${var.region}:${var.account_id}:loadbalancer/${p.load_balancer_id}"])) }
  ]
}
# Reuse existing invocation role/group/function and preserve all old addresses.
resource "alicloud_api_gateway_api" "proxy" {
  for_each    = local.proxy_routes
  group_id    = alicloud_api_gateway_group.read.id
  name        = "${var.name}-proxy-${each.key}"
  description = "Authenticated individual PgCat ${each.key} command"
  auth_type   = "ANONYMOUS"
  request_config {
    protocol = "HTTPS"
    method   = "POST"
    path     = each.value
    mode     = "PASSTHROUGH"
  }
  service_type = "FunctionCompute"
  fc_service_config {
    content_type_category = "CLIENT"
    function_version      = "3.0"
    function_type         = "HttpTrigger"
    region                = var.region
    function_name         = var.function_name
    function_base_url     = var.trigger_url
    path                  = each.value
    method                = "POST"
    only_business_path    = true
    arn_role              = alicloud_ram_role.invoke.arn
    timeout               = 30000
  }
  stage_names = ["RELEASE"]
  depends_on  = [alicloud_ram_role_policy_attachment.invoke]
}
