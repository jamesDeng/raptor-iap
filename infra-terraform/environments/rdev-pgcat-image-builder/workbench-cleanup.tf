removed {
 from = alicloud_security_group_rule.workbench_a
 lifecycle { destroy = true }
}

removed {
 from = alicloud_security_group_rule.workbench_b
 lifecycle { destroy = true }
}
