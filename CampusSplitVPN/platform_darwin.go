//go:build darwin

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type routeInfo struct {
	Gateway   string
	Interface string
}

func commandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %v (%s)", filepath.Base(name), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func routeGet(destination string) (routeInfo, error) {
	out, err := commandOutput("/sbin/route", "-n", "get", destination)
	if err != nil {
		return routeInfo{}, err
	}
	var info routeInfo
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "gateway":
			info.Gateway = fields[1]
		case "interface":
			info.Interface = fields[1]
		}
	}
	if info.Interface == "" {
		return info, fmt.Errorf("无法从 route 输出识别网卡")
	}
	return info, nil
}

func hardwareDevices() ([]string, error) {
	out, err := commandOutput("/usr/sbin/networksetup", "-listallhardwareports")
	if err != nil {
		return nil, err
	}
	var devices []string
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Device:") {
			device := strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
			if device != "" {
				devices = append(devices, device)
			}
		}
	}
	return devices, scanner.Err()
}

func findLocalNetwork(exclude string) (gateway, device string, err error) {
	devices, err := hardwareDevices()
	if err != nil {
		return "", "", err
	}
	for _, dev := range devices {
		if dev == exclude || strings.HasPrefix(dev, "utun") || strings.HasPrefix(dev, "ppp") {
			continue
		}
		gw, gwErr := commandOutput("/usr/sbin/ipconfig", "getoption", dev, "router")
		if gwErr != nil {
			continue
		}
		gw = strings.TrimSpace(gw)
		if gw == "" {
			continue
		}
		if _, addrErr := commandOutput("/usr/sbin/ipconfig", "getifaddr", dev); addrErr == nil {
			return gw, dev, nil
		}
	}
	return "", "", fmt.Errorf("未找到已联网的 Wi-Fi/有线网卡及其本地网关")
}

func isTunnelInterface(name string) bool {
	return strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "ppp") || strings.HasPrefix(name, "ipsec") || strings.HasPrefix(name, "tap") || strings.HasPrefix(name, "tun")
}

func discoverBeforeApply() (NetworkSnapshot, error) {
	current, err := routeGet("default")
	if err != nil {
		return NetworkSnapshot{}, fmt.Errorf("无法读取默认路由：%w", err)
	}
	localGateway, localInterface, err := findLocalNetwork(current.Interface)
	if err != nil {
		return NetworkSnapshot{}, err
	}
	snapshot := NetworkSnapshot{
		VPNGateway: current.Gateway, VPNInterface: current.Interface,
		LocalGateway: localGateway, LocalInterface: localInterface,
	}
	snapshot.Ready = current.Interface != localInterface && isTunnelInterface(current.Interface)
	if snapshot.Ready {
		snapshot.Summary = "校园 VPN 已接管默认路由，可以执行分流。"
	} else {
		snapshot.Summary = fmt.Sprintf("当前默认路由走 %s，不像是校园 VPN 隧道。", current.Interface)
	}
	return snapshot, nil
}

func inspectRoutes(targets []Target, state *SavedState) (NetworkSnapshot, error) {
	current, err := routeGet("default")
	if err != nil {
		return NetworkSnapshot{}, err
	}
	snapshot := NetworkSnapshot{InternetRoute: current.Interface, TargetRoutes: make(map[string]string)}
	if state == nil {
		before, discoverErr := discoverBeforeApply()
		if discoverErr != nil {
			return snapshot, discoverErr
		}
		before.InternetRoute = current.Interface
		for _, target := range targets {
			if route, routeErr := routeGet(target.Probe); routeErr == nil {
				before.TargetRoutes[target.Prefix] = route.Interface
			}
		}
		return before, nil
	}
	snapshot.VPNGateway = state.VPNGateway
	snapshot.VPNInterface = state.VPNInterface
	snapshot.LocalGateway = state.LocalGateway
	snapshot.LocalInterface = state.LocalInterface
	// A second VPN may become the new default after splitting. That is expected;
	// the important invariant is that ordinary traffic no longer uses campus VPN.
	allGood := current.Interface != state.VPNInterface
	for _, target := range state.Targets {
		route, routeErr := routeGet(target.Probe)
		if routeErr != nil {
			snapshot.TargetRoutes[target.Prefix] = "不可达"
			allGood = false
			continue
		}
		snapshot.TargetRoutes[target.Prefix] = route.Interface
		if route.Interface != state.VPNInterface {
			allGood = false
		}
	}
	snapshot.Ready = allGood
	if allGood {
		snapshot.Summary = "分流正常：校园地址走校园 VPN，普通流量走本地网络。"
	} else {
		snapshot.Summary = "分流状态与保存值不一致，校园 VPN 可能重写了路由。"
	}
	return snapshot, nil
}

func runRoute(args ...string) (string, error) {
	return commandOutput("/sbin/route", append([]string{"-n"}, args...)...)
}

func applyRoutes(targets []Target, stateFile string) OperationResult {
	if old, err := readState(stateFile); err == nil && old != nil {
		return OperationResult{OK: false, Message: "检测到尚未恢复的分流状态。请先点击“恢复路由”，再重新应用。"}
	}
	snapshot, err := discoverBeforeApply()
	if err != nil {
		return OperationResult{Message: "检测网络失败：" + err.Error()}
	}
	if !snapshot.Ready {
		return OperationResult{Message: snapshot.Summary + " 请先关闭其他 VPN，只连接校园 VPN 后重试。", Snapshot: &snapshot}
	}
	state := SavedState{
		Version: 1, Platform: platformName(), AppliedAt: now(), Targets: targets,
		VPNGateway: snapshot.VPNGateway, VPNInterface: snapshot.VPNInterface,
		LocalGateway: snapshot.LocalGateway, LocalInterface: snapshot.LocalInterface,
	}
	if err := writeJSON(stateFile, state); err != nil {
		return OperationResult{Message: "无法保存恢复信息，未修改路由：" + err.Error()}
	}
	var added []Target
	rollback := func() {
		for _, target := range added {
			_, _ = deleteDarwinTarget(target)
		}
		_ = os.Remove(stateFile)
	}
	for _, target := range targets {
		args := []string{"add"}
		if target.Host {
			args = append(args, "-host", target.Prefix)
		} else {
			args = append(args, "-net", target.Prefix)
		}
		args = append(args, "-interface", snapshot.VPNInterface)
		out, addErr := runRoute(args...)
		if addErr != nil {
			if strings.Contains(strings.ToLower(out+addErr.Error()), "file exists") {
				route, routeErr := routeGet(target.Probe)
				if routeErr == nil && route.Interface == snapshot.VPNInterface {
					continue
				}
			}
			rollback()
			return OperationResult{Message: fmt.Sprintf("添加校园路由 %s 失败：%v", target.Prefix, addErr)}
		}
		added = append(added, target)
	}
	state.AddedTargets = added
	if err := writeJSON(stateFile, state); err != nil {
		rollback()
		return OperationResult{Message: "保存路由状态失败：" + err.Error()}
	}
	if _, err := runRoute("change", "default", snapshot.LocalGateway); err != nil {
		rollback()
		return OperationResult{Message: "恢复本地默认路由失败，已回滚新增的校园路由：" + err.Error()}
	}
	checked, checkErr := inspectRoutes(targets, &state)
	if checkErr != nil || !checked.Ready {
		message := "路由已经修改，但自动验证未完全通过。"
		if checkErr != nil {
			message += " " + checkErr.Error()
		}
		return OperationResult{OK: true, Message: message, Snapshot: &checked, Details: []string{"可点击“重新检查”；若网络异常，请点击“恢复路由”。"}}
	}
	return OperationResult{OK: true, Message: "分流完成。现在可以开启你的另一个 VPN/代理。", Snapshot: &checked}
}

func deleteDarwinTarget(target Target) (string, error) {
	if target.Host {
		return runRoute("delete", "-host", target.Prefix)
	}
	return runRoute("delete", "-net", target.Prefix)
}

func restoreRoutes(stateFile string) OperationResult {
	state, err := readState(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return OperationResult{OK: true, Message: "没有需要恢复的分流状态。"}
		}
		return OperationResult{Message: "无法读取恢复信息：" + err.Error()}
	}
	var details []string
	for _, target := range state.AddedTargets {
		if _, err := deleteDarwinTarget(target); err != nil {
			details = append(details, fmt.Sprintf("删除 %s 时系统返回：%v", target.Prefix, err))
		}
	}
	current, currentErr := routeGet("default")
	if currentErr == nil && current.Interface != state.LocalInterface && current.Interface != state.VPNInterface {
		details = append(details, "检测到另一个 VPN 正在接管默认路由，因此没有改动它；断开该 VPN 和校园 VPN 即可完全恢复。")
	} else if state.VPNGateway != "" && !strings.HasPrefix(state.VPNGateway, "link#") {
		if _, err := runRoute("change", "default", state.VPNGateway); err != nil {
			return OperationResult{Message: "恢复校园 VPN 默认路由失败。可直接断开校园 VPN 并重新连接：" + err.Error(), Details: details}
		}
	} else {
		details = append(details, "校园 VPN 使用链路网关；请断开并重新连接校园 VPN 以恢复其默认路由。")
	}
	if err := os.Remove(stateFile); err != nil && !os.IsNotExist(err) {
		details = append(details, "无法删除状态文件："+err.Error())
	}
	return OperationResult{OK: true, Message: "已恢复路由。也可以直接断开校园 VPN。", Details: details}
}

func isAdministrator() bool { return os.Geteuid() == 0 }

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func startElevated(args []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	parts := []string{shellQuote(self)}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	command := strings.Join(parts, " ")
	scriptText := "do shell script " + strconv.Quote(command) + " with administrator privileges"
	cmd := exec.Command("/usr/bin/osascript", "-e", scriptText)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func openBrowser(url string) error {
	return exec.Command("/usr/bin/open", url).Start()
}
