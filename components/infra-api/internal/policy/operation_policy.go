package policy

import "strings"

const ApprovalPolicyMetaKey = "io.raptor-iap/approval-policy"

type ApprovalVariant struct {
	Capability            string `json:"capability"`
	TargetKind            string `json:"targetKind"`
	Condition             string `json:"condition"`
	RequiresHumanApproval bool   `json:"requiresHumanApproval"`
	label                 string
}

type ToolApprovalPolicy struct {
	Version  int               `json:"version"`
	Variants []ApprovalVariant `json:"variants"`
}

var (
	scaleOut = ApprovalVariant{Capability: "db-proxy.scale-out", TargetKind: "db-proxy", Condition: "desired-greater-than-current", label: "Scale out when the desired capacity is greater than the current capacity"}
	scaleIn  = ApprovalVariant{Capability: "db-proxy.scale-in", TargetKind: "db-proxy", Condition: "desired-less-than-current", RequiresHumanApproval: true, label: "Scale in when the desired capacity is less than the current capacity"}
)

func ApprovalPolicyForTool(tool string) (ToolApprovalPolicy, bool) {
	var variants []ApprovalVariant
	switch tool {
	case "db_proxy_scale":
		variants = []ApprovalVariant{scaleOut, scaleIn}
	case "db_proxy_node_protection_set":
		variants = []ApprovalVariant{
			{Capability: "db-proxy.node-protection", TargetKind: "db-proxy", Condition: "always", label: "Change protection for the bound proxy nodes"},
		}
	case "db_proxy_nodes_deregister":
		variants = []ApprovalVariant{
			{Capability: "db-proxy.drain", TargetKind: "db-proxy", Condition: "always", label: "Set the bound nodes' NLB routing weight to zero"},
		}
	case "deployment_restart":
		variants = []ApprovalVariant{
			{Capability: "application.restart", TargetKind: "application", Condition: "always", label: "Roll the bound application deployment"},
		}
	case "deployments_list":
		variants = []ApprovalVariant{
			{Capability: "resource.deployments-list", TargetKind: "catalog-resource", Condition: "always", label: "Discover workloads for the bound catalog resource"},
		}
	case "deployment_status_get":
		variants = []ApprovalVariant{
			{Capability: "application.deployment-status", TargetKind: "application", Condition: "always", label: "Read status for the bound application deployment"},
		}
	case "proxy_observation_get":
		variants = []ApprovalVariant{
			{Capability: "db-proxy.observe", TargetKind: "db-proxy", Condition: "always", label: "Read the bound proxy's fleet, routing, or capacity"},
		}
	case "proxy_pgcat_metrics_get":
		variants = []ApprovalVariant{
			{Capability: "db-proxy.read-metrics", TargetKind: "db-proxy", Condition: "always", label: "Read PgCat metrics for the bound proxy"},
		}
	default:
		return ToolApprovalPolicy{}, false
	}
	return ToolApprovalPolicy{Version: 1, Variants: variants}, true
}

func (p ToolApprovalPolicy) Description() string {
	descriptions := make([]string, 0, len(p.Variants))
	for _, v := range p.Variants {
		if v.RequiresHumanApproval {
			descriptions = append(descriptions, v.label+" (requires human approval bound to the current request, target and parameters).")
		} else {
			descriptions = append(descriptions, v.label+" (no separate human approval required).")
		}
	}
	return strings.Join(descriptions, " ")
}

// ScaleApprovalVariant classifies a mutation using the verified fleet snapshot.
// A no-change request does not have an approval variant.
func ScaleApprovalVariant(s ProxySnapshot, desired int) (ApprovalVariant, error) {
	direction, err := ScaleDirection(s, desired)
	if err != nil {
		return ApprovalVariant{}, err
	}
	switch direction {
	case "out":
		return scaleOut, nil
	case "in":
		return scaleIn, nil
	default:
		return ApprovalVariant{}, nil
	}
}
