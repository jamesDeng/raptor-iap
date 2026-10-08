mock_provider "alicloud" {
  mock_data "alicloud_account" {
    defaults = { id = "1234567890123456" }
  }
}
variables {
  account_id     = "1234567890123456"
  env_code       = "fixture.env"
  db_code        = "db-fixture"
  region         = "ap-southeast-1"
  zone           = "ap-southeast-1a"
  vpc_id         = "vpc-fixture"
  vswitch_id     = "vsw-fixture"
  instance_class = "pg.fixture"
  engine_version = "14.0"
  storage_gib    = 10
  client_cidrs   = ["10.70.1.0/24"]
}
run "private_database_identity" {
  command = plan
  assert {
    condition     = alicloud_db_instance.test.tags["env"] == "fixture.env" && alicloud_db_instance.test.tags["db-code"] == "db-fixture"
    error_message = "Discovery tags must identify dedicated DB."
  }
  assert {
    condition     = alicloud_db_instance.test.ssl_action == "Open" && alicloud_db_instance.test.storage_auto_scale == "Disable" && alicloud_db_instance.test.instance_charge_type == "Postpaid"
    error_message = "Private TLS-enabled bounded DB required."
  }
}
run "wrong_region" {
  command = plan
  variables { region = "cn-hangzhou" }
  expect_failures = [var.region]
}
run "blank_code" {
  command = plan
  variables { db_code = "" }
  expect_failures = [var.db_code]
}
run "public_cidr" {
  command = plan
  variables { client_cidrs = ["0.0.0.0/0"] }
  expect_failures = [var.client_cidrs]
}
run "wrong_account" {
  command = plan
  variables { account_id = "9999999999999999" }
  expect_failures = [terraform_data.account_guard]
}
run "changed_apply_account" {
  command = apply
  override_data {
    target = data.alicloud_account.at_apply
    values = { id = "9999999999999999" }
  }
  expect_failures = [terraform_data.account_guard]
}
