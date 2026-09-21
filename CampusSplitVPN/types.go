package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultTarget = "172.25.24.135"

type Target struct {
	Prefix string `json:"prefix"`
	Probe  string `json:"probe"`
	Host   bool   `json:"host"`
}

type NetworkSnapshot struct {
	VPNGateway     string            `json:"vpnGateway,omitempty"`
	VPNInterface   string            `json:"vpnInterface,omitempty"`
	VPNIndex       int               `json:"vpnIndex,omitempty"`
	LocalGateway   string            `json:"localGateway,omitempty"`
	LocalInterface string            `json:"localInterface,omitempty"`
	LocalIndex     int               `json:"localIndex,omitempty"`
	InternetRoute  string            `json:"internetRoute,omitempty"`
	TargetRoutes   map[string]string `json:"targetRoutes,omitempty"`
	Ready          bool              `json:"ready"`
	Summary        string            `json:"summary"`
}

type MetricState struct {
	InterfaceIndex int    `json:"interfaceIndex"`
	NextHop        string `json:"nextHop"`
	RouteMetric    int    `json:"routeMetric"`
}

type SavedState struct {
	Version         int          `json:"version"`
	Platform        string       `json:"platform"`
	AppliedAt       time.Time    `json:"appliedAt"`
	Targets         []Target     `json:"targets"`
	AddedTargets    []Target     `json:"addedTargets"`
	VPNGateway      string       `json:"vpnGateway"`
	VPNInterface    string       `json:"vpnInterface"`
	VPNIndex        int          `json:"vpnIndex,omitempty"`
	LocalGateway    string       `json:"localGateway"`
	LocalInterface  string       `json:"localInterface"`
	LocalIndex      int          `json:"localIndex,omitempty"`
	VPNDefault      *MetricState `json:"vpnDefault,omitempty"`
	PhysicalDefault *MetricState `json:"physicalDefault,omitempty"`
}

type OperationResult struct {
	OK       bool             `json:"ok"`
	Message  string           `json:"message"`
	Details  []string         `json:"details,omitempty"`
	Snapshot *NetworkSnapshot `json:"snapshot,omitempty"`
}

func parseTargets(raw string) ([]Target, error) {
	raw = strings.NewReplacer("，", ",", "；", ",", ";", ",", "\n", ",", "\r", ",").Replace(raw)
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool)
	var targets []Target
	for _, part := range parts {
		s := strings.TrimSpace(part)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			ip, network, err := net.ParseCIDR(s)
			if err != nil || ip.To4() == nil {
				return nil, fmt.Errorf("%q 不是有效的 IPv4 网段", s)
			}
			ones, _ := network.Mask.Size()
			network.IP = ip.Mask(network.Mask)
			prefix := fmt.Sprintf("%s/%d", network.IP.String(), ones)
			probe := network.IP.To4()
			if ones < 32 {
				probe = append(net.IP(nil), probe...)
				probe[3]++
			}
			if !seen[prefix] {
				targets = append(targets, Target{Prefix: prefix, Probe: probe.String()})
				seen[prefix] = true
			}
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil || ip.To4() == nil {
			return nil, fmt.Errorf("%q 不是有效的 IPv4 地址", s)
		}
		value := ip.To4().String()
		if !seen[value] {
			targets = append(targets, Target{Prefix: value, Probe: value, Host: true})
			seen[value] = true
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("请至少填写一个校园服务器 IPv4 地址或网段")
	}
	return targets, nil
}

func statePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "CampusSplitVPN")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readState(path string) (*SavedState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state SavedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
