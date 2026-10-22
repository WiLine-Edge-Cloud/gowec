package wec

import (
	"context"
	"net/http"
	"net/url"
)

// NetworkACL is a VPC's set of traffic rules. The built-in ones have no VPC.
type NetworkACL struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	VPCID       string `json:"vpc_id,omitempty"`
}

// ACLRule is one rule of a network ACL. Action and TrafficType come back
// capitalised (Allow, Ingress).
type ACLRule struct {
	ID          string `json:"id"`
	Number      int    `json:"number"`
	Action      string `json:"action,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	TrafficType string `json:"traffic_type,omitempty"`
	CIDR        string `json:"cidr,omitempty"`
	StartPort   *int   `json:"start_port"`
	EndPort     *int   `json:"end_port"`
	ICMPType    *int   `json:"icmp_type"`
	ICMPCode    *int   `json:"icmp_code"`
	Reason      string `json:"reason,omitempty"`
	State       string `json:"state,omitempty"`
}

// ACLRuleSpec is the body that adds or replaces a network ACL rule. Action
// and TrafficType are lower case: allow or deny, ingress or egress.
type ACLRuleSpec struct {
	Protocol    string `json:"protocol"`
	Action      string `json:"action,omitempty"`
	TrafficType string `json:"traffic_type,omitempty"`
	CIDR        string `json:"cidr,omitempty"`
	StartPort   *int   `json:"start_port,omitempty"`
	EndPort     *int   `json:"end_port,omitempty"`
	ICMPType    *int   `json:"icmp_type,omitempty"`
	ICMPCode    *int   `json:"icmp_code,omitempty"`
	Number      *int   `json:"number,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

func aclPath(aclID string) string { return "/network-acls/" + PathEscape(aclID) }

func aclRulePath(aclID, ruleID string) string { return aclPath(aclID) + "/rules/" + PathEscape(ruleID) }

// ListNetworkACLs returns the network ACLs, or only a VPC's (id) when vpcID is set.
func (c *Client) ListNetworkACLs(ctx context.Context, vpcID string) ([]NetworkACL, error) {
	opts := ListOptions{}
	if vpcID != "" {
		opts.Extra = url.Values{"vpc_id": {vpcID}}
	}
	return listAll[NetworkACL](ctx, c, "/network-acls", opts)
}

// FindNetworkACL reads one network ACL by name or id.
func (c *Client) FindNetworkACL(ctx context.Context, acl string) (*NetworkACL, error) {
	all, err := c.ListNetworkACLs(ctx, "")
	if err != nil {
		return nil, err
	}
	return findRef(all, acl, "acl", func(a NetworkACL) (string, string) { return a.ID, a.Name })
}

// CreateNetworkACL creates a network ACL in a VPC (id).
func (c *Client) CreateNetworkACL(ctx context.Context, vpcID, name, description string) (*JobAccepted, error) {
	body := map[string]string{"vpc_id": vpcID, "name": name}
	if description != "" {
		body["description"] = description
	}
	return c.machineAction(ctx, http.MethodPost, "/network-acls", nil, body, "")
}

// UpdateNetworkACL renames a network ACL (id); description may be empty.
func (c *Client) UpdateNetworkACL(ctx context.Context, aclID, name, description string) (*JobAccepted, error) {
	body := map[string]string{"name": name}
	if description != "" {
		body["description"] = description
	}
	return c.machineAction(ctx, http.MethodPatch, aclPath(aclID), nil, body, "")
}

// DeleteNetworkACL deletes a network ACL (id) no network uses.
func (c *Client) DeleteNetworkACL(ctx context.Context, aclID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, aclPath(aclID), nil, nil, "")
}

// ListACLRules returns a network ACL's (id) rules.
func (c *Client) ListACLRules(ctx context.Context, aclID string) ([]ACLRule, error) {
	return listAll[ACLRule](ctx, c, aclPath(aclID)+"/rules", ListOptions{})
}

// AddACLRule adds a rule to a network ACL (id).
func (c *Client) AddACLRule(ctx context.Context, aclID string, rule ACLRuleSpec) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, aclPath(aclID)+"/rules", nil, rule, "")
}

// UpdateACLRule replaces a network ACL rule (ids) with rule.
func (c *Client) UpdateACLRule(ctx context.Context, aclID, ruleID string, rule ACLRuleSpec) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPut, aclRulePath(aclID, ruleID), nil, rule, "")
}

// MoveACLRule moves a rule (ids) to right after previousID, or right before
// nextID. Give one of them.
func (c *Client) MoveACLRule(ctx context.Context, aclID, ruleID, previousID, nextID string) (*JobAccepted, error) {
	body := map[string]string{}
	if previousID != "" {
		body["previous_rule_id"] = previousID
	}
	if nextID != "" {
		body["next_rule_id"] = nextID
	}
	return c.machineAction(ctx, http.MethodPost, aclRulePath(aclID, ruleID)+"/move", nil, body, "")
}

// DeleteACLRule removes a rule (ids) from a network ACL.
func (c *Client) DeleteACLRule(ctx context.Context, aclID, ruleID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, aclRulePath(aclID, ruleID), nil, nil, "")
}

// SecurityGroup is a security group with its rules.
type SecurityGroup struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	IngressRules []SecurityGroupRule `json:"ingress_rules"`
	EgressRules  []SecurityGroupRule `json:"egress_rules"`
}

// SecurityGroupRule is one inbound or outbound rule of a security group.
type SecurityGroupRule struct {
	ID        string `json:"id"`
	Protocol  string `json:"protocol,omitempty"`
	CIDR      string `json:"cidr,omitempty"`
	StartPort *int   `json:"start_port"`
	EndPort   *int   `json:"end_port"`
	ICMPType  *int   `json:"icmp_type"`
	ICMPCode  *int   `json:"icmp_code"`
}

// SecurityGroupRuleSpec is the body that adds a security group rule.
type SecurityGroupRuleSpec struct {
	Protocol  string `json:"protocol"`
	CIDR      string `json:"cidr,omitempty"`
	StartPort *int   `json:"start_port,omitempty"`
	EndPort   *int   `json:"end_port,omitempty"`
	ICMPType  *int   `json:"icmp_type,omitempty"`
	ICMPCode  *int   `json:"icmp_code,omitempty"`
}

// ListSecurityGroups returns the security groups and their rules.
func (c *Client) ListSecurityGroups(ctx context.Context) ([]SecurityGroup, error) {
	return listAll[SecurityGroup](ctx, c, "/security-groups", ListOptions{})
}

// FindSecurityGroup reads one security group by name or id.
func (c *Client) FindSecurityGroup(ctx context.Context, group string) (*SecurityGroup, error) {
	all, err := c.ListSecurityGroups(ctx)
	if err != nil {
		return nil, err
	}
	return findRef(all, group, "group", func(g SecurityGroup) (string, string) { return g.ID, g.Name })
}

// CreateSecurityGroup creates a security group.
func (c *Client) CreateSecurityGroup(ctx context.Context, name string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/security-groups", nil, map[string]string{"name": name}, "")
}

// DeleteSecurityGroup deletes a security group (id).
func (c *Client) DeleteSecurityGroup(ctx context.Context, groupID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, "/security-groups/"+PathEscape(groupID), nil, nil, "")
}

// AddSecurityGroupRule allows traffic in a direction, ingress or egress, on
// a security group (id).
func (c *Client) AddSecurityGroupRule(ctx context.Context, groupID, direction string, rule SecurityGroupRuleSpec) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/security-groups/"+PathEscape(groupID)+"/"+direction+"-rules", nil, rule, "")
}

// DeleteSecurityGroupRule removes an ingress or egress rule (id).
func (c *Client) DeleteSecurityGroupRule(ctx context.Context, direction, ruleID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, "/security-groups/"+direction+"-rules/"+PathEscape(ruleID), nil, nil, "")
}
