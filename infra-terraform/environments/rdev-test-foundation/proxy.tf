# This service-linked role belongs to Alibaba Cloud NLB at account scope.
# Retiring DP00001 releases only this stack's state reference.
removed {
  from = module.proxy.alicloud_resource_manager_service_linked_role.nlb
  lifecycle { destroy = false }
}
