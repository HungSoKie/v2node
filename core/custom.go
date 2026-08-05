package core

import (
	"bytes"
	"encoding/json"
	"net"
	"reflect"
	"strings"

	log "github.com/sirupsen/logrus"
	panel "github.com/wyx2685/v2node/api/v2board"
	"github.com/xtls/xray-core/app/dns"
	"github.com/xtls/xray-core/app/router"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	coreConf "github.com/xtls/xray-core/infra/conf"
)

// hasPublicIPv6 checks if the machine has a public IPv6 address
func hasPublicIPv6() bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		// Check if it's IPv6, not loopback, not link-local, not private/ULA
		if ip.To4() == nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsPrivate() {
			return true
		}
	}
	return false
}

func hasOutboundWithTag(list []*core.OutboundHandlerConfig, tag string) bool {
	for _, o := range list {
		if o != nil && o.Tag == tag {
			return true
		}
	}
	return false
}

func hasBalancerWithTag(list []*coreConf.BalancingRule, tag string) bool {
	for _, b := range list {
		if b != nil && b.Tag == tag {
			return true
		}
	}
	return false
}

// jsonSemanticEqual reports whether a and b encode the same JSON value,
// ignoring formatting differences such as whitespace and key order.
func jsonSemanticEqual(a, b []byte) bool {
	var av, bv interface{}
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// scopeRuleToInbound injects an inboundTag selector into a raw RuleObject
// JSON fragment so a rule delivered for one node cannot silently match
// traffic on other nodes' inbounds when several nodes run in the same
// v2node process. Rules that already declare their own inboundTag are left
// untouched, since that is an explicit choice by the panel operator.
func scopeRuleToInbound(raw json.RawMessage, tag string) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw
	}
	if _, ok := fields["inboundTag"]; ok {
		return raw
	}
	tagJSON, err := json.Marshal([]string{tag})
	if err != nil {
		return raw
	}
	fields["inboundTag"] = tagJSON
	scoped, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return scoped
}

func GetCustomConfig(infos []*panel.NodeInfo) (*dns.Config, []*core.OutboundHandlerConfig, *router.Config, error) {
	//dns
	queryStrategy := "UseIPv4v6"
	if !hasPublicIPv6() {
		queryStrategy = "UseIPv4"
	}
	coreDnsConfig := &coreConf.DNSConfig{
		Servers: []*coreConf.NameServerConfig{
			{
				Address: &coreConf.Address{
					Address: xnet.ParseAddress("localhost"),
				},
			},
		},
		QueryStrategy: queryStrategy,
	}
	//outbound
	defaultoutbound, _ := buildDefaultOutbound()
	coreOutboundConfig := append([]*core.OutboundHandlerConfig{}, defaultoutbound)
	block, _ := buildBlockOutbound()
	coreOutboundConfig = append(coreOutboundConfig, block)
	dns, _ := buildDnsOutbound()
	coreOutboundConfig = append(coreOutboundConfig, dns)

	//route
	domainStrategy := "AsIs"
	dnsRule, _ := json.Marshal(map[string]interface{}{
		"port":        "53",
		"network":     "udp",
		"outboundTag": "dns_out",
	})
	coreRouterConfig := &coreConf.RouterConfig{
		RuleList:       []json.RawMessage{dnsRule},
		DomainStrategy: &domainStrategy,
	}

	// seenOutboundCase remembers the raw JSON of every outbound_case entry
	// registered so far, keyed by tag, so identical re-deliveries from other
	// nodes on the same panel can be told apart from genuine tag conflicts.
	seenOutboundCase := make(map[string]string)

	for _, info := range infos {
		if len(info.Common.Routes) == 0 &&
			len(bytes.TrimSpace(info.Common.RouteCase)) == 0 &&
			len(bytes.TrimSpace(info.Common.OutboundCase)) == 0 {
			continue
		}
		for _, route := range info.Common.Routes {
			switch route.Action {
			case "dns":
				if route.ActionValue == nil {
					continue
				}
				server := &coreConf.NameServerConfig{
					Address: &coreConf.Address{
						Address: xnet.ParseAddress(*route.ActionValue),
					},
				}
				if len(route.Match) != 0 {
					server.Domains = route.Match
					server.SkipFallback = true
				}
				coreDnsConfig.Servers = append(coreDnsConfig.Servers, server)
			case "block":
				rule := map[string]interface{}{
					"inboundTag":  info.Tag,
					"domain":      route.Match,
					"outboundTag": "block",
				}
				rawRule, err := json.Marshal(rule)
				if err != nil {
					continue
				}
				coreRouterConfig.RuleList = append(coreRouterConfig.RuleList, rawRule)
			case "block_ip":
				rule := map[string]interface{}{
					"inboundTag":  info.Tag,
					"ip":          route.Match,
					"outboundTag": "block",
				}
				rawRule, err := json.Marshal(rule)
				if err != nil {
					continue
				}
				coreRouterConfig.RuleList = append(coreRouterConfig.RuleList, rawRule)
			case "block_port":
				rule := map[string]interface{}{
					"inboundTag":  info.Tag,
					"port":        strings.Join(route.Match, ","),
					"outboundTag": "block",
				}
				rawRule, err := json.Marshal(rule)
				if err != nil {
					continue
				}
				coreRouterConfig.RuleList = append(coreRouterConfig.RuleList, rawRule)
			case "protocol":
				rule := map[string]interface{}{
					"inboundTag":  info.Tag,
					"protocol":    route.Match,
					"outboundTag": "block",
				}
				rawRule, err := json.Marshal(rule)
				if err != nil {
					continue
				}
				coreRouterConfig.RuleList = append(coreRouterConfig.RuleList, rawRule)
			case "route":
				if route.ActionValue == nil {
					continue
				}
				outbound := &coreConf.OutboundDetourConfig{}
				err := json.Unmarshal([]byte(*route.ActionValue), outbound)
				if err != nil {
					continue
				}
				rule := map[string]interface{}{
					"inboundTag":  info.Tag,
					"domain":      route.Match,
					"outboundTag": outbound.Tag,
				}
				rawRule, err := json.Marshal(rule)
				if err != nil {
					continue
				}
				coreRouterConfig.RuleList = append(coreRouterConfig.RuleList, rawRule)
				if hasOutboundWithTag(coreOutboundConfig, outbound.Tag) {
					continue
				}
				custom_outbound, err := outbound.Build()
				if err != nil {
					continue
				}
				coreOutboundConfig = append(coreOutboundConfig, custom_outbound)
			case "route_ip":
				if route.ActionValue == nil {
					continue
				}
				outbound := &coreConf.OutboundDetourConfig{}
				err := json.Unmarshal([]byte(*route.ActionValue), outbound)
				if err != nil {
					continue
				}
				rule := map[string]interface{}{
					"inboundTag":  info.Tag,
					"ip":          route.Match,
					"outboundTag": outbound.Tag,
				}
				rawRule, err := json.Marshal(rule)
				if err != nil {
					continue
				}
				coreRouterConfig.RuleList = append(coreRouterConfig.RuleList, rawRule)
				if hasOutboundWithTag(coreOutboundConfig, outbound.Tag) {
					continue
				}
				custom_outbound, err := outbound.Build()
				if err != nil {
					continue
				}
				coreOutboundConfig = append(coreOutboundConfig, custom_outbound)
			case "default_out":
				if route.ActionValue == nil {
					continue
				}
				outbound := &coreConf.OutboundDetourConfig{}
				err := json.Unmarshal([]byte(*route.ActionValue), outbound)
				if err != nil {
					continue
				}
				rule := map[string]interface{}{
					"inboundTag":  info.Tag,
					"network":     "tcp,udp",
					"outboundTag": outbound.Tag,
				}
				rawRule, err := json.Marshal(rule)
				if err != nil {
					continue
				}
				coreRouterConfig.RuleList = append(coreRouterConfig.RuleList, rawRule)
				if hasOutboundWithTag(coreOutboundConfig, outbound.Tag) {
					continue
				}
				custom_outbound, err := outbound.Build()
				if err != nil {
					continue
				}
				coreOutboundConfig = append(coreOutboundConfig, custom_outbound)
			default:
				continue
			}
		}

		// outbound_case: raw xray-core []conf.OutboundDetourConfig JSON delivered
		// directly by the panel, built and appended before route_case so any
		// rules referencing these tags have them registered.
		if len(bytes.TrimSpace(info.Common.OutboundCase)) > 0 {
			var rawOutbounds []json.RawMessage
			if err := json.Unmarshal(info.Common.OutboundCase, &rawOutbounds); err != nil {
				log.WithFields(log.Fields{"tag": info.Tag, "err": err}).Warn("failed to parse outbound_case, skipped")
			}
			for _, rawOutbound := range rawOutbounds {
				outbound := &coreConf.OutboundDetourConfig{}
				if err := json.Unmarshal(rawOutbound, outbound); err != nil {
					log.WithFields(log.Fields{"tag": info.Tag, "err": err}).Warn("failed to parse outbound_case entry, skipped")
					continue
				}
				if outbound.Tag != "" && hasOutboundWithTag(coreOutboundConfig, outbound.Tag) {
					// Nodes on the same panel usually receive identical
					// outbound_case content, so a same-content re-delivery
					// is expected; only a conflicting redefinition of the
					// tag warrants a warning.
					if prev, ok := seenOutboundCase[outbound.Tag]; ok && jsonSemanticEqual([]byte(prev), rawOutbound) {
						log.WithFields(log.Fields{"tag": info.Tag, "outboundTag": outbound.Tag}).Debug("outbound_case entry already registered with identical content, skipped")
					} else {
						log.WithFields(log.Fields{"tag": info.Tag, "outboundTag": outbound.Tag}).Warn("conflicting outbound tag in outbound_case, skipped")
					}
					continue
				}
				customOutbound, err := outbound.Build()
				if err != nil {
					log.WithFields(log.Fields{"tag": info.Tag, "outboundTag": outbound.Tag, "err": err}).Warn("failed to build outbound_case entry, skipped")
					continue
				}
				coreOutboundConfig = append(coreOutboundConfig, customOutbound)
				if outbound.Tag != "" {
					seenOutboundCase[outbound.Tag] = string(rawOutbound)
				}
			}
		}

		// route_case: raw xray-core conf.RouterConfig JSON delivered directly by
		// the panel. Rules without an explicit inboundTag are scoped to this
		// node so they cannot leak onto other nodes' inbounds when several
		// nodes run in the same v2node process.
		if len(bytes.TrimSpace(info.Common.RouteCase)) > 0 {
			rawRouterConfig := &coreConf.RouterConfig{}
			if err := json.Unmarshal(info.Common.RouteCase, rawRouterConfig); err != nil {
				log.WithFields(log.Fields{"tag": info.Tag, "err": err}).Warn("failed to parse route_case, skipped")
			} else if _, err := rawRouterConfig.Build(); err != nil {
				// Validate the whole block up front: a bad rule that only
				// surfaced in the final combined Build would panic the whole
				// process instead of degrading just this node's rules.
				log.WithFields(log.Fields{"tag": info.Tag, "err": err}).Warn("invalid route_case, skipped")
			} else {
				if rawRouterConfig.DomainStrategy != nil {
					if coreRouterConfig.DomainStrategy != nil && *coreRouterConfig.DomainStrategy != domainStrategy && *coreRouterConfig.DomainStrategy != *rawRouterConfig.DomainStrategy {
						log.WithField("tag", info.Tag).Warnf("route_case domainStrategy %q overrides earlier value %q", *rawRouterConfig.DomainStrategy, *coreRouterConfig.DomainStrategy)
					}
					coreRouterConfig.DomainStrategy = rawRouterConfig.DomainStrategy
				}
				for _, balancer := range rawRouterConfig.Balancers {
					if hasBalancerWithTag(coreRouterConfig.Balancers, balancer.Tag) {
						log.WithFields(log.Fields{"tag": info.Tag, "balancerTag": balancer.Tag}).Warn("duplicate balancerTag in route_case, skipped")
						continue
					}
					coreRouterConfig.Balancers = append(coreRouterConfig.Balancers, balancer)
				}
				for _, rule := range rawRouterConfig.RuleList {
					coreRouterConfig.RuleList = append(coreRouterConfig.RuleList, scopeRuleToInbound(rule, info.Tag))
				}
			}
		}
	}

	// The dispatcher falls back to the default outbound when a rule's target
	// tag has no handler, so a dangling reference silently sends matched
	// traffic through Default instead of failing loudly — surface it here.
	outboundTags := make(map[string]bool, len(coreOutboundConfig))
	for _, o := range coreOutboundConfig {
		outboundTags[o.Tag] = true
	}
	balancerTags := make(map[string]bool, len(coreRouterConfig.Balancers))
	for _, b := range coreRouterConfig.Balancers {
		balancerTags[b.Tag] = true
	}
	for _, raw := range coreRouterConfig.RuleList {
		var ref struct {
			OutboundTag string `json:"outboundTag"`
			BalancerTag string `json:"balancerTag"`
		}
		if err := json.Unmarshal(raw, &ref); err != nil {
			continue
		}
		if ref.OutboundTag != "" && !outboundTags[ref.OutboundTag] {
			log.WithField("outboundTag", ref.OutboundTag).Warn("routing rule references an outbound that does not exist; matched traffic will use the default outbound")
		}
		if ref.BalancerTag != "" && !balancerTags[ref.BalancerTag] {
			log.WithField("balancerTag", ref.BalancerTag).Warn("routing rule references a balancer that does not exist")
		}
	}

	DnsConfig, err := coreDnsConfig.Build()
	if err != nil {
		return nil, nil, nil, err
	}
	RouterConfig, err := coreRouterConfig.Build()
	if err != nil {
		return nil, nil, nil, err
	}
	return DnsConfig, coreOutboundConfig, RouterConfig, nil
}
