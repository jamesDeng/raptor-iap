mock_provider "alicloud" {}
run "disabled_instance" {
  command = plan
  assert {
    condition     = length(module.database) == 0 && length(module.proxy) == 0
    error_message = "Disabled test instance must contain no module resources."
  }
}
