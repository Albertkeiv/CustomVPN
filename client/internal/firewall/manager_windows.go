//go:build windows

package firewall

import (
	"context"
	"fmt"
	"net"
	"strings"

	"customvpn/client/internal/logging"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

const (
	killSwitchGroup = "CustomVPN KillSwitch"

	netFwProfile2Domain  = 1
	netFwProfile2Private = 2
	netFwProfile2Public  = 4
	netFwProfile2All     = 0x7fffffff

	netFwActionBlock = 0
	netFwDirOutbound = 2

	netFwProtocolTCP = 6
	netFwProtocolUDP = 17
)

type Manager struct {
	logger *logging.Logger
}

func NewManager(logger *logging.Logger) *Manager {
	return &Manager{logger: logger}
}

func (m *Manager) BlockDNSOnInterface(ctx context.Context, iface string, _ []string, _ string) ([]string, error) {
	if strings.TrimSpace(iface) == "" {
		return nil, fmt.Errorf("interface alias is empty")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	localAddrs, err := interfaceAddresses(iface)
	if err != nil {
		return nil, err
	}
	rules := []struct {
		name     string
		protocol int
	}{
		{name: fmt.Sprintf("CustomVPN DNS Block (%s) UDP", iface), protocol: netFwProtocolUDP},
		{name: fmt.Sprintf("CustomVPN DNS Block (%s) TCP", iface), protocol: netFwProtocolTCP},
	}
	created := make([]string, 0, len(rules))
	err = withFirewallPolicy(func(policy *ole.IDispatch) error {
		rulesDisp, cleanup, err := firewallRules(policy)
		if err != nil {
			return err
		}
		defer cleanup()
		for _, rule := range rules {
			if err := removeRuleByName(rulesDisp, rule.name); err != nil {
				if m.logger != nil {
					m.logger.Debugf("firewall rule remove skipped: %s (%v)", rule.name, err)
				}
			}
			if err := addBlockRule(rulesDisp, rule.name, iface, localAddrs, rule.protocol); err != nil {
				return err
			}
			created = append(created, rule.name)
			if m.logger != nil {
				m.logger.Debugf("firewall rule added: %s", rule.name)
			}
		}
		return nil
	})
	if err != nil {
		if len(created) > 0 {
			_ = m.RemoveRules(ctx, created)
		}
		return created, err
	}
	return created, nil
}

func (m *Manager) CheckAvailable(ctx context.Context, iface string) error {
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface alias is empty")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if _, err := interfaceByName(iface); err != nil {
		return fmt.Errorf("interface not found: %w", err)
	}
	return withFirewallPolicy(func(policy *ole.IDispatch) error {
		enabled, err := firewallEnabled(policy)
		if err != nil {
			return err
		}
		if !enabled {
			return fmt.Errorf("%w: no enabled profiles", ErrFirewallDisabled)
		}
		if state, err := localPolicyModifyState(policy); err == nil {
			if state != 0 {
				return fmt.Errorf("%w: state=%d", ErrLocalPolicyMergeDisabled, state)
			}
		} else if m.logger != nil {
			m.logger.Debugf("firewall check: local policy modify state unavailable: %v", err)
		}
		allowed, err := allowLocalFirewallRules(policy, enabledProfiles(policy))
		if err != nil {
			if m.logger != nil {
				m.logger.Debugf("firewall check: allow local rules unavailable: %v", err)
			}
			return nil
		}
		if !allowed {
			return fmt.Errorf("%w: local rules disabled", ErrLocalPolicyMergeDisabled)
		}
		return nil
	})
}

func (m *Manager) EnableLocalPolicyMerge(ctx context.Context) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return withFirewallPolicy(func(policy *ole.IDispatch) error {
		for _, profile := range []int{netFwProfile2Domain, netFwProfile2Private, netFwProfile2Public} {
			if v, err := oleutil.GetProperty(policy, "AllowLocalFirewallRules", profile); err != nil {
				return fmt.Errorf("%w: %w", ErrLocalPolicyMergeUnsupported, err)
			} else {
				v.Clear()
			}
			if err := setAllowLocalFirewallRules(policy, profile, true); err != nil {
				return err
			}
		}
		if m.logger != nil {
			m.logger.Debugf("firewall: enabled local firewall rules for all profiles")
		}
		return nil
	})
}

func (m *Manager) RemoveRules(ctx context.Context, rules []string) error {
	if len(rules) == 0 {
		return nil
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return withFirewallPolicy(func(policy *ole.IDispatch) error {
		rulesDisp, cleanup, err := firewallRules(policy)
		if err != nil {
			return err
		}
		defer cleanup()
		for _, name := range rules {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if err := removeRuleByName(rulesDisp, name); err != nil {
				if m.logger != nil {
					m.logger.Debugf("firewall rule remove skipped: %s (%v)", name, err)
				}
				continue
			}
			if m.logger != nil {
				m.logger.Debugf("firewall rule removed: %s", name)
			}
		}
		return nil
	})
}

func (m *Manager) RemoveKillSwitchGroup(ctx context.Context) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return withFirewallPolicy(func(policy *ole.IDispatch) error {
		rulesDisp, cleanup, err := firewallRules(policy)
		if err != nil {
			return err
		}
		defer cleanup()
		names, err := rulesByGroup(rulesDisp, killSwitchGroup)
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := removeRuleByName(rulesDisp, name); err != nil {
				if m.logger != nil {
					m.logger.Debugf("firewall group rule remove skipped: %s (%v)", name, err)
				}
				continue
			}
			if m.logger != nil {
				m.logger.Debugf("firewall group rule removed: %s", name)
			}
		}
		return nil
	})
}

func withFirewallPolicy(fn func(*ole.IDispatch) error) error {
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		return fmt.Errorf("initialize COM: %w", err)
	}
	defer ole.CoUninitialize()
	obj, err := oleutil.CreateObject("HNetCfg.FwPolicy2")
	if err != nil {
		return fmt.Errorf("create firewall policy: %w", err)
	}
	defer obj.Release()
	policy, err := obj.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("query firewall policy: %w", err)
	}
	defer policy.Release()
	return fn(policy)
}

func firewallRules(policy *ole.IDispatch) (*ole.IDispatch, func(), error) {
	v, err := oleutil.GetProperty(policy, "Rules")
	if err != nil {
		return nil, nil, fmt.Errorf("get firewall rules: %w", err)
	}
	rules := v.ToIDispatch()
	cleanup := func() {
		if rules != nil {
			rules.Release()
		}
		v.Clear()
	}
	return rules, cleanup, nil
}

func addBlockRule(rules *ole.IDispatch, name, iface string, localAddrs []string, protocol int) error {
	ruleObj, err := oleutil.CreateObject("HNetCfg.FwRule")
	if err != nil {
		return fmt.Errorf("create firewall rule: %w", err)
	}
	defer ruleObj.Release()
	rule, err := ruleObj.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("query firewall rule: %w", err)
	}
	defer rule.Release()
	if _, err := oleutil.PutProperty(rule, "Name", name); err != nil {
		return err
	}
	_, _ = oleutil.PutProperty(rule, "Grouping", killSwitchGroup)
	_, _ = oleutil.PutProperty(rule, "Direction", netFwDirOutbound)
	_, _ = oleutil.PutProperty(rule, "Action", netFwActionBlock)
	_, _ = oleutil.PutProperty(rule, "Enabled", true)
	_, _ = oleutil.PutProperty(rule, "Protocol", protocol)
	_, _ = oleutil.PutProperty(rule, "RemotePorts", "53")
	_, _ = oleutil.PutProperty(rule, "Profiles", netFwProfile2All)
	if len(localAddrs) > 0 {
		_, _ = oleutil.PutProperty(rule, "LocalAddresses", strings.Join(localAddrs, ","))
	}
	if _, err := oleutil.CallMethod(rules, "Add", rule); err != nil {
		return fmt.Errorf("add firewall rule: %w", err)
	}
	return nil
}

func removeRuleByName(rules *ole.IDispatch, name string) error {
	_, err := oleutil.CallMethod(rules, "Remove", name)
	if err != nil {
		return fmt.Errorf("remove firewall rule %s: %w", name, err)
	}
	return nil
}

func rulesByGroup(rules *ole.IDispatch, group string) ([]string, error) {
	var names []string
	err := oleutil.ForEach(rules, func(item *ole.VARIANT) error {
		defer item.Clear()
		rule := item.ToIDispatch()
		if rule == nil {
			return nil
		}
		defer rule.Release()
		groupVar, err := oleutil.GetProperty(rule, "Grouping")
		if err != nil {
			return nil
		}
		grouping := strings.TrimSpace(groupVar.ToString())
		groupVar.Clear()
		if grouping != group {
			return nil
		}
		nameVar, err := oleutil.GetProperty(rule, "Name")
		if err != nil {
			return nil
		}
		name := strings.TrimSpace(nameVar.ToString())
		nameVar.Clear()
		if name != "" {
			names = append(names, name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("enumerate rules: %w", err)
	}
	return names, nil
}

func firewallEnabled(policy *ole.IDispatch) (bool, error) {
	enabled := false
	for _, profile := range []int{netFwProfile2Domain, netFwProfile2Private, netFwProfile2Public} {
		val, err := oleutil.GetProperty(policy, "FirewallEnabled", profile)
		if err != nil {
			return false, fmt.Errorf("firewall enabled check failed: %w", err)
		}
		if val.Value().(bool) {
			enabled = true
		}
		val.Clear()
	}
	return enabled, nil
}

func enabledProfiles(policy *ole.IDispatch) []int {
	var enabled []int
	for _, profile := range []int{netFwProfile2Domain, netFwProfile2Private, netFwProfile2Public} {
		val, err := oleutil.GetProperty(policy, "FirewallEnabled", profile)
		if err == nil {
			if val.Value().(bool) {
				enabled = append(enabled, profile)
			}
			val.Clear()
		}
	}
	return enabled
}

func localPolicyModifyState(policy *ole.IDispatch) (int, error) {
	v, err := oleutil.GetProperty(policy, "LocalPolicyModifyState")
	if err != nil {
		return 0, err
	}
	state := int(v.Val)
	v.Clear()
	return state, nil
}

func allowLocalFirewallRules(policy *ole.IDispatch, profiles []int) (bool, error) {
	if len(profiles) == 0 {
		return true, nil
	}
	for _, profile := range profiles {
		v, err := oleutil.GetProperty(policy, "AllowLocalFirewallRules", profile)
		if err != nil {
			return true, err
		}
		allowed, ok := v.Value().(bool)
		v.Clear()
		if ok && !allowed {
			return false, nil
		}
	}
	return true, nil
}

func setAllowLocalFirewallRules(policy *ole.IDispatch, profile int, value bool) error {
	_, err := oleutil.PutProperty(policy, "AllowLocalFirewallRules", profile, value)
	return err
}

func interfaceByName(name string) (*net.Interface, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("interface name is empty")
	}
	iface, err := net.InterfaceByName(name)
	if err == nil {
		return iface, nil
	}
	ifaces, listErr := net.Interfaces()
	if listErr != nil {
		return nil, err
	}
	for i := range ifaces {
		if strings.EqualFold(ifaces[i].Name, name) {
			return &ifaces[i], nil
		}
	}
	return nil, err
}

func interfaceAddresses(name string) ([]string, error) {
	iface, err := interfaceByName(name)
	if err != nil {
		return nil, err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("read interface addresses: %w", err)
	}
	var result []string
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil {
			continue
		}
		result = append(result, ip.String())
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("interface %s has no addresses", name)
	}
	return result, nil
}
