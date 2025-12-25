package firewall

import "errors"

var ErrLocalPolicyMergeDisabled = errors.New("local firewall rules are disabled by policy")
var ErrFirewallDisabled = errors.New("windows firewall is disabled")
var ErrLocalPolicyMergeUnsupported = errors.New("allowlocalpolicymerge is not supported")
