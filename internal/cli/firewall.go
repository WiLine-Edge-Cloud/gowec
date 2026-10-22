package cli

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"git.wiline.com/cloud/cloud-cli/internal/ui"
	"git.wiline.com/cloud/cloud-cli/pkg/wec"
)

func firewallCmd(r *run) *cobra.Command {
	acl := group("acl", "A VPC's network ACLs and their rules", []string{"acls"},
		aclListCmd(r), aclCreateCmd(r), aclRenameCmd(r), aclDeleteCmd(r), aclRulesCmd(r),
		group("rule", "Change a network ACL's rules", nil,
			aclRuleAddCmd(r), aclRuleUpdateCmd(r), aclRuleMoveCmd(r), aclRuleDeleteCmd(r)))
	sg := group("sg", "Security groups and their rules", []string{"security-group", "security-groups"},
		sgListCmd(r), sgCreateCmd(r), sgDeleteCmd(r), sgRulesCmd(r),
		group("rule", "Change a security group's rules", nil, sgRuleAddCmd(r), sgRuleDeleteCmd(r)))
	return group("firewall", "Network ACLs and security groups", []string{"fw"}, acl, sg)
}

func aclArg(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return r.completeNames("acl", listACLNames)
}

func listACLNames(ctx context.Context, c *wec.Client) ([]string, error) {
	acls, err := c.ListNetworkACLs(ctx, "")
	names := make([]string, len(acls))
	for i, a := range acls {
		names[i] = a.Name
	}
	return names, err
}

func sgArg(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return r.completeNames("security-group", listSGNames)
}

func listSGNames(ctx context.Context, c *wec.Client) ([]string, error) {
	groups, err := c.ListSecurityGroups(ctx)
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}
	return names, err
}

// ── network ACLs ─────────────────────────────────────────────────────

func aclListCmd(r *run) *cobra.Command {
	var vpc string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List network ACLs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			var vpcID string
			if vpc != "" {
				v, err := c.FindVPC(cmd.Context(), vpc)
				if err != nil {
					return err
				}
				vpcID = v.ID
			}
			acls, err := c.ListNetworkACLs(cmd.Context(), vpcID)
			if err != nil {
				return err
			}
			vpcNames := map[string]string{}
			if r.output == "table" && !r.quiet {
				if vpcs, err := c.ListVPCs(cmd.Context(), wec.ListOptions{}); err == nil {
					for _, v := range vpcs {
						vpcNames[v.ID] = v.Name
					}
				}
			}
			ids := make([]string, len(acls))
			rows := make([][]string, len(acls))
			for i, a := range acls {
				owner := ui.Dim.Render("built-in")
				if a.VPCID != "" {
					owner = dash(vpcNames[a.VPCID])
				}
				ids[i] = a.ID
				rows[i] = []string{a.Name, owner, ui.Dim.Render(dash(a.Description)), ui.Dim.Render(a.ID)}
			}
			return r.show(acls, ids, func() string {
				if len(acls) == 0 {
					return ui.Dim.Render("No network ACLs.") + "\n"
				}
				return ui.Table([]string{"NAME", "VPC", "DESCRIPTION", "ID"}, rows)
			})
		},
	}
	cmd.Flags().StringVar(&vpc, "vpc", "", "only this VPC's ACLs, and the built-in ones (name or id)")
	netCompleteFlag(r, cmd, "vpc", "vpc", listVPCNames)
	return cmd
}

func aclCreateCmd(r *run) *cobra.Command {
	var vpc, name, description string
	cmd := &cobra.Command{
		Use:     "create --vpc <vpc> --name <name>",
		Aliases: []string{"new"},
		Short:   "Create a network ACL in a VPC",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, err := c.FindVPC(cmd.Context(), vpc)
			if err != nil {
				return err
			}
			accepted, err := c.CreateNetworkACL(cmd.Context(), v.ID, name, description)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Creating "+name, "Created "+name+" in "+v.Name)
		},
	}
	cmd.Flags().StringVar(&vpc, "vpc", "", "the VPC (name or id)")
	cmd.Flags().StringVar(&name, "name", "", "the ACL's name")
	cmd.Flags().StringVar(&description, "description", "", "a description")
	_ = cmd.MarkFlagRequired("vpc")
	_ = cmd.MarkFlagRequired("name")
	netCompleteFlag(r, cmd, "vpc", "vpc", listVPCNames)
	return cmd
}

func aclRenameCmd(r *run) *cobra.Command {
	var name, description string
	cmd := &cobra.Command{
		Use:               "rename <acl> --name <new-name>",
		Aliases:           []string{"update"},
		Short:             "Rename a network ACL",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: aclArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			a, err := c.FindNetworkACL(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("description") {
				description = a.Description
			}
			accepted, err := c.UpdateNetworkACL(cmd.Context(), a.ID, name, description)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Renaming "+a.Name, a.Name+" is now "+name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the new name")
	cmd.Flags().StringVar(&description, "description", "", "a new description")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func aclDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <acl>",
		Aliases:           []string{"rm"},
		Short:             "Delete a network ACL (no network may use it)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: aclArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			a, err := c.FindNetworkACL(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := r.confirm("Delete the network ACL "+a.Name+" and its rules?", "deleting can't be undone"); err != nil {
				return err
			}
			accepted, err := c.DeleteNetworkACL(cmd.Context(), a.ID)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Deleting "+a.Name, a.Name+" is deleted")
		},
	}
}

func aclRulesCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "rules <acl>",
		Short:             "List a network ACL's rules, in the order they apply",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: aclArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			a, err := c.FindNetworkACL(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			rules, err := c.ListACLRules(cmd.Context(), a.ID)
			if err != nil {
				return err
			}
			ids := make([]string, len(rules))
			for i, rule := range rules {
				ids[i] = rule.ID
			}
			return r.show(rules, ids, func() string {
				if len(rules) == 0 {
					return ui.Dim.Render("No rules in "+a.Name+".") + "\n"
				}
				ordered := append([]wec.ACLRule(nil), rules...)
				sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Number < ordered[j].Number })
				rows := make([][]string, len(ordered))
				for i, rule := range ordered {
					rows[i] = []string{strconv.Itoa(rule.Number), ruleAction(rule.Action), ruleProtocol(rule.Protocol),
						ruleTarget(rule.Protocol, rule.StartPort, rule.EndPort, rule.ICMPType, rule.ICMPCode),
						ruleCIDRs(rule.CIDR), strings.ToLower(rule.TrafficType), ui.Dim.Render(clip(rule.Reason, 32)),
						ui.Dim.Render(rule.ID)}
				}
				return ui.Table([]string{"#", "ACTION", "PROTOCOL", "PORTS", "CIDR", "TRAFFIC", "REASON", "ID"}, rows)
			})
		},
	}
}

func ruleAction(action string) string {
	switch strings.ToLower(action) {
	case "allow":
		return ui.Green.Render("allow")
	case "deny":
		return ui.Red.Render("deny")
	}
	return dash(action)
}

// findACLRule is the rule ref names in an ACL: its id, or its number.
func findACLRule(ctx context.Context, c *wec.Client, acl *wec.NetworkACL, ref string) (*wec.ACLRule, error) {
	rules, err := c.ListACLRules(ctx, acl.ID)
	if err != nil {
		return nil, err
	}
	number, numErr := strconv.Atoi(ref)
	for i, rule := range rules {
		if strings.EqualFold(rule.ID, ref) || (numErr == nil && rule.Number == number) {
			return &rules[i], nil
		}
	}
	return nil, usagef("%s has no rule %s; see gowec firewall acl rules %s", acl.Name, ref, acl.Name)
}

// ruleFlags are the flags that describe an ACL or security group rule.
type ruleFlags struct {
	protocol, action, traffic, cidr, port, reason string
	icmpType, icmpCode, number                    int
}

func (f *ruleFlags) add(cmd *cobra.Command, acl bool) {
	fs := cmd.Flags()
	fs.StringVar(&f.protocol, "protocol", "", "tcp, udp, icmp, all, or a protocol number")
	fs.StringVar(&f.cidr, "cidr", "", "the CIDRs it applies to, comma-separated (default any)")
	fs.StringVar(&f.port, "port", "", "one port (22) or a range (8000-8100), for tcp and udp")
	fs.IntVar(&f.icmpType, "icmp-type", -1, "the ICMP type, for icmp")
	fs.IntVar(&f.icmpCode, "icmp-code", -1, "the ICMP code, for icmp")
	_ = cmd.RegisterFlagCompletionFunc("protocol", fixed("tcp", "udp", "icmp", "all"))
	if !acl {
		return
	}
	fs.StringVar(&f.action, "action", "", "allow or deny; a new rule allows")
	fs.StringVar(&f.traffic, "traffic", "", "ingress (inbound) or egress (outbound); a new rule is ingress")
	fs.IntVar(&f.number, "number", 0, "its position in the ACL; lower numbers apply first")
	fs.StringVar(&f.reason, "reason", "", "a note on why the rule is there")
	_ = cmd.RegisterFlagCompletionFunc("action", fixed("allow", "deny"))
	_ = cmd.RegisterFlagCompletionFunc("traffic", fixed("ingress", "egress"))
}

// match fills a rule's protocol, ports, ICMP and CIDR fields from the flags
// that were given. A new protocol drops ports and ICMP fields it can't have.
func (f *ruleFlags) match(cmd *cobra.Command, proto *string, cidr *string, start, end, icmpType, icmpCode **int) error {
	changed := cmd.Flags().Changed
	if changed("protocol") {
		*proto = strings.ToLower(f.protocol)
	}
	p := *proto
	if p != "tcp" && p != "udp" {
		if changed("port") {
			return usagef("--port is for tcp and udp rules")
		}
		*start, *end = nil, nil
	}
	if p != "icmp" {
		if changed("icmp-type") || changed("icmp-code") {
			return usagef("--icmp-type and --icmp-code are for icmp rules")
		}
		*icmpType, *icmpCode = nil, nil
	}
	if changed("port") {
		var err error
		if *start, *end, err = parsePortFlag(f.port); err != nil {
			return err
		}
	}
	if changed("icmp-type") {
		*icmpType = &f.icmpType
	}
	if changed("icmp-code") {
		*icmpCode = &f.icmpCode
	}
	if changed("cidr") {
		*cidr = f.cidr
	}
	return nil
}

// apply lays the given flags over spec.
func (f *ruleFlags) apply(cmd *cobra.Command, spec *wec.ACLRuleSpec) error {
	changed := cmd.Flags().Changed
	if err := f.match(cmd, &spec.Protocol, &spec.CIDR, &spec.StartPort, &spec.EndPort, &spec.ICMPType, &spec.ICMPCode); err != nil {
		return err
	}
	if changed("action") {
		spec.Action = strings.ToLower(f.action)
	} else if spec.Action == "" {
		spec.Action = "allow"
	}
	if changed("traffic") {
		spec.TrafficType = strings.ToLower(f.traffic)
	} else if spec.TrafficType == "" {
		spec.TrafficType = "ingress"
	}
	if spec.Action != "allow" && spec.Action != "deny" {
		return usagef("--action must be allow or deny")
	}
	if spec.TrafficType != "ingress" && spec.TrafficType != "egress" {
		return usagef("--traffic must be ingress or egress")
	}
	if changed("number") {
		spec.Number = &f.number
	}
	if changed("reason") {
		spec.Reason = f.reason
	}
	return nil
}

func aclRuleAddCmd(r *run) *cobra.Command {
	var f ruleFlags
	cmd := &cobra.Command{
		Use:               "add <acl> --protocol <protocol> [--port <port>] [--action allow|deny] [--traffic ingress|egress]",
		Aliases:           []string{"create", "new"},
		Short:             "Add a rule to a network ACL",
		Example:           "  gowec firewall acl rule add web-acl --protocol tcp --port 443 --cidr 0.0.0.0/0 --number 10",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: aclArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			var spec wec.ACLRuleSpec
			if err := f.apply(cmd, &spec); err != nil {
				return err
			}
			a, err := c.FindNetworkACL(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			accepted, err := c.AddACLRule(cmd.Context(), a.ID, spec)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Adding the rule to "+a.Name, "Added the rule to "+a.Name)
		},
	}
	f.add(cmd, true)
	_ = cmd.MarkFlagRequired("protocol")
	return cmd
}

func aclRuleUpdateCmd(r *run) *cobra.Command {
	var f ruleFlags
	cmd := &cobra.Command{
		Use:               "update <acl> <rule> [flags]",
		Aliases:           []string{"edit"},
		Short:             "Change a network ACL rule; flags left out keep their value",
		Example:           "  gowec firewall acl rule update web-acl 10 --action deny",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: aclArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			a, err := c.FindNetworkACL(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			rule, err := findACLRule(cmd.Context(), c, a, args[1])
			if err != nil {
				return err
			}
			// The API replaces the whole rule, so start from what it is now.
			number := rule.Number
			spec := wec.ACLRuleSpec{
				Protocol: strings.ToLower(rule.Protocol), Action: strings.ToLower(rule.Action),
				TrafficType: strings.ToLower(rule.TrafficType), CIDR: rule.CIDR,
				StartPort: rule.StartPort, EndPort: rule.EndPort, ICMPType: rule.ICMPType, ICMPCode: rule.ICMPCode,
				Number: &number, Reason: rule.Reason,
			}
			if err := f.apply(cmd, &spec); err != nil {
				return err
			}
			accepted, err := c.UpdateACLRule(cmd.Context(), a.ID, rule.ID, spec)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, fmt.Sprintf("Changing rule %d of %s", rule.Number, a.Name),
				fmt.Sprintf("Changed rule %d of %s", rule.Number, a.Name))
		},
	}
	f.add(cmd, true)
	return cmd
}

func aclRuleMoveCmd(r *run) *cobra.Command {
	var after, before string
	cmd := &cobra.Command{
		Use:               "move <acl> <rule> --after <rule> | --before <rule>",
		Short:             "Move a network ACL rule before or after another",
		Example:           "  gowec firewall acl rule move web-acl 30 --before 10",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: aclArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (after == "") == (before == "") {
				return usagef("give one of --after or --before")
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			a, err := c.FindNetworkACL(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			rule, err := findACLRule(cmd.Context(), c, a, args[1])
			if err != nil {
				return err
			}
			other, where := after, "after"
			if before != "" {
				other, where = before, "before"
			}
			anchor, err := findACLRule(cmd.Context(), c, a, other)
			if err != nil {
				return err
			}
			previousID, nextID := anchor.ID, ""
			if before != "" {
				previousID, nextID = "", anchor.ID
			}
			accepted, err := c.MoveACLRule(cmd.Context(), a.ID, rule.ID, previousID, nextID)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, fmt.Sprintf("Moving rule %d", rule.Number),
				fmt.Sprintf("Rule %d now comes %s rule %d", rule.Number, where, anchor.Number))
		},
	}
	cmd.Flags().StringVar(&after, "after", "", "the rule to come right after")
	cmd.Flags().StringVar(&before, "before", "", "the rule to come right before")
	return cmd
}

func aclRuleDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <acl> <rule>",
		Aliases:           []string{"rm"},
		Short:             "Remove a network ACL rule (by number or id)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: aclArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			a, err := c.FindNetworkACL(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			rule, err := findACLRule(cmd.Context(), c, a, args[1])
			if err != nil {
				return err
			}
			question := fmt.Sprintf("Remove rule %d of %s (%s %s %s)?", rule.Number, a.Name, strings.ToLower(rule.Action),
				strings.ToLower(rule.TrafficType), ruleProtocol(rule.Protocol))
			if err := r.confirm(question, "removing a rule"); err != nil {
				return err
			}
			accepted, err := c.DeleteACLRule(cmd.Context(), a.ID, rule.ID)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, fmt.Sprintf("Removing rule %d", rule.Number),
				fmt.Sprintf("Removed rule %d of %s", rule.Number, a.Name))
		},
	}
}

// ── security groups ──────────────────────────────────────────────────

func sgListCmd(r *run) *cobra.Command {
	return listCmd(r, "List security groups",
		func(cmd *cobra.Command, c *wec.Client) ([]wec.SecurityGroup, error) {
			return c.ListSecurityGroups(cmd.Context())
		},
		[]string{"NAME", "INBOUND", "OUTBOUND", "DESCRIPTION", "ID"},
		func(g wec.SecurityGroup) []string {
			return []string{g.Name, ruleCount(len(g.IngressRules)), ruleCount(len(g.EgressRules)),
				ui.Dim.Render(dash(g.Description)), ui.Dim.Render(g.ID)}
		},
		func(g wec.SecurityGroup) string { return g.ID })
}

func ruleCount(n int) string {
	switch n {
	case 0:
		return ui.Dim.Render("no rules")
	case 1:
		return "1 rule"
	}
	return fmt.Sprintf("%d rules", n)
}

func sgCreateCmd(r *run) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:     "create --name <name>",
		Aliases: []string{"new"},
		Short:   "Create a security group",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			accepted, err := c.CreateSecurityGroup(cmd.Context(), name)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Creating "+name, "Created "+name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the group's name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func sgDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <group>",
		Aliases:           []string{"rm"},
		Short:             "Delete a security group",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sgArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			g, err := c.FindSecurityGroup(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := r.confirm("Delete the security group "+g.Name+" and its rules?", "deleting can't be undone"); err != nil {
				return err
			}
			accepted, err := c.DeleteSecurityGroup(cmd.Context(), g.ID)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Deleting "+g.Name, g.Name+" is deleted")
		},
	}
}

// sgRule is a security group rule with the direction it applies in.
type sgRule struct {
	wec.SecurityGroupRule
	Direction string
}

func sgRules(g *wec.SecurityGroup) []sgRule {
	var rules []sgRule
	for _, rule := range g.IngressRules {
		rules = append(rules, sgRule{rule, "ingress"})
	}
	for _, rule := range g.EgressRules {
		rules = append(rules, sgRule{rule, "egress"})
	}
	return rules
}

func sgRulesCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "rules <group>",
		Short:             "List a security group's rules",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sgArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			g, err := c.FindSecurityGroup(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			rules := sgRules(g)
			ids := make([]string, len(rules))
			rows := make([][]string, len(rules))
			for i, rule := range rules {
				ids[i] = rule.ID
				rows[i] = []string{rule.Direction, ruleProtocol(rule.Protocol),
					ruleTarget(rule.Protocol, rule.StartPort, rule.EndPort, rule.ICMPType, rule.ICMPCode),
					ruleCIDRs(rule.CIDR), ui.Dim.Render(rule.ID)}
			}
			return r.show(g, ids, func() string {
				if len(rules) == 0 {
					return ui.Dim.Render("No rules in "+g.Name+".") + "\n"
				}
				return ui.Table([]string{"TRAFFIC", "PROTOCOL", "PORTS", "CIDR", "ID"}, rows)
			})
		},
	}
}

func sgRuleAddCmd(r *run) *cobra.Command {
	var f ruleFlags
	cmd := &cobra.Command{
		Use:               "add <group> --protocol <protocol> [--port <port>] [--traffic ingress|egress]",
		Aliases:           []string{"create", "new"},
		Short:             "Allow traffic in a security group",
		Example:           "  gowec firewall sg rule add web --protocol tcp --port 22 --cidr 203.0.113.0/24",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sgArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.traffic = strings.ToLower(f.traffic)
			if f.traffic != "ingress" && f.traffic != "egress" {
				return usagef("--traffic must be ingress or egress")
			}
			var spec wec.SecurityGroupRuleSpec
			if err := f.match(cmd, &spec.Protocol, &spec.CIDR, &spec.StartPort, &spec.EndPort, &spec.ICMPType, &spec.ICMPCode); err != nil {
				return err
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			g, err := c.FindSecurityGroup(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if spec.CIDR == "" {
				// CloudStack refuses a security group rule without one (431).
				spec.CIDR = "0.0.0.0/0"
			}
			accepted, err := c.AddSecurityGroupRule(cmd.Context(), g.ID, f.traffic, spec)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Adding the rule to "+g.Name, "Added the rule to "+g.Name)
		},
	}
	f.add(cmd, false)
	cmd.Flags().StringVar(&f.traffic, "traffic", "ingress", "ingress (inbound) or egress (outbound)")
	_ = cmd.RegisterFlagCompletionFunc("traffic", fixed("ingress", "egress"))
	_ = cmd.MarkFlagRequired("protocol")
	return cmd
}

func sgRuleDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <group> <rule-id>",
		Aliases:           []string{"rm"},
		Short:             "Remove a security group rule",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: sgArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			g, err := c.FindSecurityGroup(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			var found *sgRule
			for _, rule := range sgRules(g) {
				if strings.EqualFold(rule.ID, args[1]) {
					found = &rule
					break
				}
			}
			if found == nil {
				return usagef("%s has no rule %s; see gowec firewall sg rules %s", g.Name, args[1], g.Name)
			}
			question := fmt.Sprintf("Remove the %s %s rule from %s?", found.Direction, ruleProtocol(found.Protocol), g.Name)
			if err := r.confirm(question, "removing a rule"); err != nil {
				return err
			}
			accepted, err := c.DeleteSecurityGroupRule(cmd.Context(), found.Direction, found.ID)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Removing the rule", "Removed the rule from "+g.Name)
		},
	}
}
