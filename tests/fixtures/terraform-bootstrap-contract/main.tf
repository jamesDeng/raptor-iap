# Local-only serialization fixture; no external provider or cloud resource.
terraform { required_version = "= 1.13.3" }
resource "terraform_data" "synthetic" {
  input = {
    vpc_id             = "vpc-fixture"
    vswitch_id         = "vsw-fixture"
    security_group_id  = "sg-fixture"
    execution_role_arn = "acs:ram::1234567890123456:role/raptor-storage"
  }
}
output "vpc_id" { value = terraform_data.synthetic.output.vpc_id }
output "vswitch_id" { value = terraform_data.synthetic.output.vswitch_id }
output "security_group_id" { value = terraform_data.synthetic.output.security_group_id }
output "execution_role_arn" { value = terraform_data.synthetic.output.execution_role_arn }
