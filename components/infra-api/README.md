# Infra API read service

Independent stateless Go HTTP service. Discovery reads STS/RDS/ESS and registered ACK Deployment status. Missing providers/cluster configuration return errors rather than successful empty inventories. This slice has no mutations.

Read [the setup guide](../../docs/setup/infra-api-read.md) before cloud deployment. Supply private scope JSON (array of code/accountId/region/optional clusterId) and caller credentials through private runtime configuration. Serverless Devs owns FC3 code/function/HTTP trigger; Terraform owns RAM roles and API Gateway. Gateway forwards `X-Infra-Authorization`, while FC's `authType: function` requires IAM signatures.

`make build INFRA_SCOPE_FILE=/private/path/scope.json` builds a static Linux binary and copies the credential-free private scope. `.build` is ignored. Caller credentials are not in the code package; they are externally injected in the function configuration. Do not publish rendered Serverless configuration or logs.

Every kubeconfig read requests a fresh 15-minute credential, validates HTTPS/CA, and rejects external credential plugins and local paths. Role permissions do not by themselves establish Kubernetes RBAC: bind get/list Deployments, ReplicaSets and Pods for the selected cluster before live ACK acceptance.
