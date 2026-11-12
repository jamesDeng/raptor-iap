# This service-linked role belongs to Alibaba Cloud NLB at account scope.
# Retiring DP00001 releases only this stack's state reference.
removed {
  from = module.proxy.alicloud_resource_manager_service_linked_role.nlb
  lifecycle { destroy = false }
}

# Alibaba ESS deletes its final scaling configuration together with the group.
# The provider cannot delete the last active configuration independently.
removed {
  from = module.proxy.alicloud_ess_scaling_configuration.proxy
  lifecycle { destroy = false }
}
