package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/v2rayA/RoutingA"
	"github.com/v2rayA/v2rayA/db/configure"
)

type VisualRule struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	MatchType string   `json:"matchType"`
	Values    []string `json:"values"`
	Outbound  string   `json:"outbound"`
	Enabled   bool     `json:"enabled"`
	Priority  int      `json:"priority"`
	Color     string   `json:"color"`
	Comment   string   `json:"comment"`
}

type VisualRoutingConfig struct {
	DefaultOutbound string       `json:"defaultOutbound"`
	Rules           []*VisualRule `json:"rules"`
}

const (
	RuleTypeDomain    = "domain"
	RuleTypeIP        = "ip"
	RuleTypePort      = "port"
	RuleTypeProtocol  = "protocol"
	RuleTypeNetwork   = "network"
	RuleTypeProcess   = "process"
	RuleTypeGeoSite   = "geosite"
	RuleTypeGeoIP     = "geoip"
)

var ruleColors = map[string]string{
	RuleTypeDomain:   "#3eaf7c",
	RuleTypeIP:       "#42b983",
	RuleTypePort:     "#f0a020",
	RuleTypeProtocol: "#722ed1",
	RuleTypeNetwork:  "#13c2c2",
	RuleTypeProcess:  "#eb2f96",
	RuleTypeGeoSite:  "#1890ff",
	RuleTypeGeoIP:    "#fa8c16",
}

var outboundColors = map[string]string{
	"proxy":  "#409eff",
	"direct": "#67c23a",
	"block":  "#f56c6c",
	"reject": "#f56c6c",
}

func GetRuleColor(ruleType string) string {
	if color, ok := ruleColors[ruleType]; ok {
		return color
	}
	return "#909399"
}

func GetOutboundColor(outbound string) string {
	if color, ok := outboundColors[strings.ToLower(outbound)]; ok {
		return color
	}
	return "#909399"
}

func ParseRoutingAToVisual(routingA string) (*VisualRoutingConfig, error) {
	config := &VisualRoutingConfig{
		DefaultOutbound: "proxy",
		Rules:           make([]*VisualRule, 0),
	}

	lines := strings.Split(routingA, "\n")
	priority := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "default:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				config.DefaultOutbound = strings.TrimSpace(parts[1])
			}
			continue
		}

		rule, err := parseLineToVisualRule(line, priority)
		if err != nil {
			continue
		}
		if rule != nil {
			config.Rules = append(config.Rules, rule)
			priority++
		}
	}

	return config, nil
}

func parseLineToVisualRule(line string, priority int) (*VisualRule, error) {
	arrowParts := strings.SplitN(line, "->", 2)
	if len(arrowParts) != 2 {
		return nil, fmt.Errorf("invalid rule format")
	}

	matchPart := strings.TrimSpace(arrowParts[0])
	outbound := strings.TrimSpace(arrowParts[1])

	re := regexp.MustCompile(`^(\w+)\((.*)\)$`)
	matches := re.FindStringSubmatch(matchPart)
	if matches == nil {
		return nil, fmt.Errorf("invalid match format")
	}

	matchType := strings.ToLower(matches[1])
	valuesStr := matches[2]

	var values []string
	if strings.Contains(valuesStr, ",") {
		for _, v := range strings.Split(valuesStr, ",") {
			v = strings.TrimSpace(v)
			if v != "" {
				values = append(values, v)
			}
		}
	} else if valuesStr != "" {
		values = []string{strings.TrimSpace(valuesStr)}
	}

	ruleType := RuleTypeDomain
	if strings.HasPrefix(matchType, "geosite") {
		ruleType = RuleTypeGeoSite
	} else if strings.HasPrefix(matchType, "geoip") {
		ruleType = RuleTypeGeoIP
	} else if matchType == "ip" || matchType == "ipcidr" {
		ruleType = RuleTypeIP
	} else if matchType == "port" {
		ruleType = RuleTypePort
	} else if matchType == "protocol" {
		ruleType = RuleTypeProtocol
	} else if matchType == "network" {
		ruleType = RuleTypeNetwork
	} else if matchType == "process" {
		ruleType = RuleTypeProcess
	}

	return &VisualRule{
		ID:        fmt.Sprintf("rule-%d", priority),
		Type:      ruleType,
		MatchType: matchType,
		Values:    values,
		Outbound:  outbound,
		Enabled:   true,
		Priority:  priority,
		Color:     GetRuleColor(ruleType),
	}, nil
}

func ConvertVisualToRoutingA(config *VisualRoutingConfig) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("default: %s\n", config.DefaultOutbound))
	sb.WriteString("# write your own rules below\n")

	rules := make([]*VisualRule, len(config.Rules))
	copy(rules, config.Rules)

	for i, rule := range rules {
		rule.Priority = i
	}

	for _, rule := range rules {
		if !rule.Enabled {
			sb.WriteString("# ")
		}

		var valuesStr string
		if len(rule.Values) > 0 {
			valuesStr = strings.Join(rule.Values, ", ")
		}

		sb.WriteString(fmt.Sprintf("%s(%s)->%s\n", rule.MatchType, valuesStr, rule.Outbound))
	}

	return sb.String()
}

type v2rayRoutingRule struct {
	Type        string   `json:"type"`
	OutboundTag string   `json:"outboundTag,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Port        string   `json:"port,omitempty"`
	SourcePort  string   `json:"sourcePort,omitempty"`
	Network     string   `json:"network,omitempty"`
	Protocol    []string `json:"protocol,omitempty"`
	Source      []string `json:"source,omitempty"`
}

type v2rayRoutingConfig struct {
	DomainStrategy string             `json:"domainStrategy"`
	Rules          []v2rayRoutingRule `json:"rules"`
}

func ConvertVisualToJSON(config *VisualRoutingConfig) (string, error) {
	rules := make([]v2rayRoutingRule, 0, len(config.Rules))

	for _, rule := range config.Rules {
		if !rule.Enabled {
			continue
		}

		vRule := v2rayRoutingRule{
			Type:        "field",
			OutboundTag: rule.Outbound,
		}

		switch rule.MatchType {
		case "domain":
			vRule.Domain = rule.Values
		case "geosite":
			for _, v := range rule.Values {
				vRule.Domain = append(vRule.Domain, "geosite:"+v)
			}
		case "ip":
			vRule.IP = rule.Values
		case "geoip":
			for _, v := range rule.Values {
				vRule.IP = append(vRule.IP, "geoip:"+v)
			}
		case "port":
			if len(rule.Values) > 0 {
				vRule.Port = strings.Join(rule.Values, ",")
			}
		case "sourcePort":
			if len(rule.Values) > 0 {
				vRule.SourcePort = strings.Join(rule.Values, ",")
			}
		case "network":
			if len(rule.Values) > 0 {
				vRule.Network = rule.Values[0]
			}
		case "protocol":
			vRule.Protocol = rule.Values
		case "source":
			vRule.Source = rule.Values
		}

		rules = append(rules, vRule)
	}

	routingConfig := v2rayRoutingConfig{
		DomainStrategy: "AsIs",
		Rules:          rules,
	}

	jsonBytes, err := json.MarshalIndent(routingConfig, "", "  ")
	if err != nil {
		return "", err
	}

	return string(jsonBytes), nil
}

func ReorderRules(config *VisualRoutingConfig, fromIndex, toIndex int) (*VisualRoutingConfig, error) {
	if fromIndex < 0 || fromIndex >= len(config.Rules) || toIndex < 0 || toIndex >= len(config.Rules) {
		return nil, fmt.Errorf("index out of range")
	}

	rule := config.Rules[fromIndex]

	if fromIndex < toIndex {
		copy(config.Rules[fromIndex:toIndex], config.Rules[fromIndex+1:toIndex+1])
	} else {
		copy(config.Rules[toIndex+1:fromIndex+1], config.Rules[toIndex:fromIndex])
	}

	config.Rules[toIndex] = rule

	for i, r := range config.Rules {
		r.Priority = i
	}

	return config, nil
}

func ImportSurgeRules(surgeContent string) (*VisualRoutingConfig, error) {
	config := &VisualRoutingConfig{
		DefaultOutbound: "proxy",
		Rules:           make([]*VisualRule, 0),
	}

	lines := strings.Split(surgeContent, "\n")
	priority := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ",", 3)
		if len(parts) < 3 {
			continue
		}

		ruleType := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		outbound := strings.TrimSpace(parts[2])

		var visualType, matchType string
		var values []string

		switch ruleType {
		case "domain":
			visualType = RuleTypeDomain
			matchType = "domain"
			values = []string{value}
		case "domain-suffix":
			visualType = RuleTypeDomain
			matchType = "domain"
			values = []string{fmt.Sprintf("domain:%s", value)}
		case "domain-keyword":
			visualType = RuleTypeDomain
			matchType = "domain"
			values = []string{fmt.Sprintf("keyword:%s", value)}
		case "ip-cidr":
			visualType = RuleTypeIP
			matchType = "ip"
			values = []string{value}
		case "geoip":
			visualType = RuleTypeGeoIP
			matchType = "geoip"
			values = []string{fmt.Sprintf("geoip:%s", strings.ToLower(value))}
		case "process-name":
			visualType = RuleTypeProcess
			matchType = "process"
			values = []string{value}
		case "dest-port":
			visualType = RuleTypePort
			matchType = "port"
			values = []string{value}
		default:
			continue
		}

		if outbound == "REJECT" || outbound == "REJECT-DROP" {
			outbound = "block"
		} else if outbound == "DIRECT" {
			outbound = "direct"
		} else if outbound == "PROXY" || strings.HasPrefix(outbound, "Proxy") {
			outbound = "proxy"
		}

		config.Rules = append(config.Rules, &VisualRule{
			ID:        fmt.Sprintf("rule-%d", priority),
			Type:      visualType,
			MatchType: matchType,
			Values:    values,
			Outbound:  strings.ToLower(outbound),
			Enabled:   true,
			Priority:  priority,
			Color:     GetRuleColor(visualType),
		})
		priority++
	}

	return config, nil
}

func ImportClashRules(clashContent string) (*VisualRoutingConfig, error) {
	type ClashRule struct {
		Type      string `json:"type"`
		Payload   string `json:"payload"`
		Proxy     string `json:"proxy"`
		NoResolve bool   `json:"no-resolve,omitempty"`
	}

	var rules []ClashRule
	err := json.Unmarshal([]byte(clashContent), &rules)
	if err != nil {
		return importClashRulesPlain(clashContent)
	}

	config := &VisualRoutingConfig{
		DefaultOutbound: "proxy",
		Rules:           make([]*VisualRule, 0),
	}

	for i, rule := range rules {
		visualType, matchType, values := parseClashRuleType(rule.Type, rule.Payload)
		if visualType == "" {
			continue
		}

		outbound := strings.ToLower(rule.Proxy)
		if outbound == "reject" {
			outbound = "block"
		}

		config.Rules = append(config.Rules, &VisualRule{
			ID:        fmt.Sprintf("rule-%d", i),
			Type:      visualType,
			MatchType: matchType,
			Values:    values,
			Outbound:  outbound,
			Enabled:   true,
			Priority:  i,
			Color:     GetRuleColor(visualType),
		})
	}

	return config, nil
}

func importClashRulesPlain(content string) (*VisualRoutingConfig, error) {
	config := &VisualRoutingConfig{
		DefaultOutbound: "proxy",
		Rules:           make([]*VisualRule, 0),
	}

	lines := strings.Split(content, "\n")
	priority := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "- ") {
			if strings.HasPrefix(line, "- ") {
				line = strings.TrimPrefix(line, "- ")
			} else {
				continue
			}
		}

		parts := strings.SplitN(line, ",", 3)
		if len(parts) < 3 {
			continue
		}

		ruleType := strings.ToLower(strings.TrimSpace(parts[0]))
		payload := strings.TrimSpace(parts[1])
		proxy := strings.TrimSpace(parts[2])

		visualType, matchType, values := parseClashRuleType(ruleType, payload)
		if visualType == "" {
			continue
		}

		outbound := strings.ToLower(proxy)
		if outbound == "reject" {
			outbound = "block"
		}

		config.Rules = append(config.Rules, &VisualRule{
			ID:        fmt.Sprintf("rule-%d", priority),
			Type:      visualType,
			MatchType: matchType,
			Values:    values,
			Outbound:  outbound,
			Enabled:   true,
			Priority:  priority,
			Color:     GetRuleColor(visualType),
		})
		priority++
	}

	return config, nil
}

func parseClashRuleType(ruleType, payload string) (visualType, matchType string, values []string) {
	ruleType = strings.ToLower(ruleType)

	switch ruleType {
	case "domain":
		return RuleTypeDomain, "domain", []string{payload}
	case "domain-suffix":
		return RuleTypeDomain, "domain", []string{fmt.Sprintf("domain:%s", payload)}
	case "domain-keyword":
		return RuleTypeDomain, "domain", []string{fmt.Sprintf("keyword:%s", payload)}
	case "ip-cidr":
		return RuleTypeIP, "ip", []string{payload}
	case "ip-cidr6":
		return RuleTypeIP, "ip", []string{payload}
	case "geoip":
		return RuleTypeGeoIP, "geoip", []string{fmt.Sprintf("geoip:%s", strings.ToLower(payload))}
	case "dst-port":
		return RuleTypePort, "port", []string{payload}
	case "src-port":
		return RuleTypePort, "port", []string{payload}
	case "process-name":
		return RuleTypeProcess, "process", []string{payload}
	case "network":
		return RuleTypeNetwork, "network", []string{payload}
	default:
		return "", "", nil
	}
}

func SaveVisualRouting(config *VisualRoutingConfig) error {
	routingA := ConvertVisualToRoutingA(config)

	hardcodeReplacement := regexp.MustCompile(`\$\$.+?\$\$`)
	lines := strings.Split(routingA, "\n")
	for i := range lines {
		hardcodes := hardcodeReplacement.FindAllString(lines[i], -1)
		for _, hardcode := range hardcodes {
			lines[i] = strings.Replace(lines[i], hardcode, "", 1)
		}
	}

	_, err := RoutingA.Parse(strings.Join(lines, "\n"))
	if err != nil {
		return err
	}

	return configure.SetRoutingA(&routingA)
}

func GetVisualRouting() (*VisualRoutingConfig, error) {
	routingA := configure.GetRoutingA()
	return ParseRoutingAToVisual(routingA)
}
