//go:build windows

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

type windowsDiscovery struct {
	VPNIndex       int    `json:"vpnIndex"`
	VPNAlias       string `json:"vpnAlias"`
	VPNGateway     string `json:"vpnGateway"`
	VPNRouteMetric int    `json:"vpnRouteMetric"`
	LocalIndex     int    `json:"localIndex"`
	LocalAlias     string `json:"localAlias"`
	LocalGateway   string `json:"localGateway"`
	LocalMetric    int    `json:"localMetric"`
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func powershellEncoded(script string) string {
	encoded := utf16.Encode([]rune(script))
	bytes := make([]byte, len(encoded)*2)
	for i, value := range encoded {
		bytes[i*2] = byte(value)
		bytes[i*2+1] = byte(value >> 8)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

func runPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", powershellEncoded(script))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("PowerShell: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func discoverWindows() (windowsDiscovery, error) {
	script := `$ErrorActionPreference='Stop'
$ifs=@{}; Get-NetIPInterface -AddressFamily IPv4 | ForEach-Object {$ifs[$_.InterfaceIndex]=$_.InterfaceMetric}
$defs=@(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -PolicyStore ActiveStore | Where-Object {$_.State -ne 'Invalid'} | Sort-Object @{Expression={$_.RouteMetric + $ifs[$_.InterfaceIndex]}})
if($defs.Count -eq 0){throw '没有 IPv4 默认路由'}
$vpn=$defs[0]
$locals=@()
Get-NetAdapter -Physical | Where-Object {$_.Status -eq 'Up'} | ForEach-Object {
  $idx=$_.ifIndex; $alias=$_.Name
  $r=@($defs | Where-Object {$_.InterfaceIndex -eq $idx} | Sort-Object RouteMetric | Select-Object -First 1)
  if($r.Count -gt 0 -and $r[0].NextHop -ne '0.0.0.0'){
    $locals += [PSCustomObject]@{Index=$idx;Alias=$alias;Gateway=$r[0].NextHop;Metric=$r[0].RouteMetric;Total=($r[0].RouteMetric+$ifs[$idx])}
  }
}
if($locals.Count -eq 0){throw '未找到已联网的物理网卡默认网关'}
$local=$locals | Sort-Object Total | Select-Object -First 1
[PSCustomObject]@{vpnIndex=$vpn.InterfaceIndex;vpnAlias=$vpn.InterfaceAlias;vpnGateway=$vpn.NextHop;vpnRouteMetric=$vpn.RouteMetric;localIndex=$local.Index;localAlias=$local.Alias;localGateway=$local.Gateway;localMetric=$local.Metric} | ConvertTo-Json -Compress`
	out, err := runPowerShell(script)
	if err != nil {
		return windowsDiscovery{}, err
	}
	var info windowsDiscovery
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return info, fmt.Errorf("无法解析 Windows 路由信息：%w", err)
	}
	return info, nil
}

func discoverBeforeApply() (NetworkSnapshot, error) {
	info, err := discoverWindows()
	if err != nil {
		return NetworkSnapshot{}, err
	}
	snapshot := NetworkSnapshot{
		VPNGateway: info.VPNGateway, VPNInterface: info.VPNAlias, VPNIndex: info.VPNIndex,
		LocalGateway: info.LocalGateway, LocalInterface: info.LocalAlias, LocalIndex: info.LocalIndex,
	}
	snapshot.Ready = info.VPNIndex != info.LocalIndex
	if snapshot.Ready {
		snapshot.Summary = "校园 VPN 已接管默认路由，可以执行分流。"
	} else {
		snapshot.Summary = "当前默认路由仍是物理网卡，不像是校园 VPN 全局路由。"
	}
	return snapshot, nil
}

func windowsRouteInterface(ip string) (string, error) {
	script := "$r=Find-NetRoute -RemoteIPAddress " + psQuote(ip) + " | Select-Object -First 1; [PSCustomObject]@{Alias=$r.InterfaceAlias;Index=$r.InterfaceIndex} | ConvertTo-Json -Compress"
	out, err := runPowerShell(script)
	if err != nil {
		return "", err
	}
	var value struct {
		Alias string `json:"Alias"`
		Index int    `json:"Index"`
	}
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		return "", err
	}
	return value.Alias, nil
}

func inspectRoutes(targets []Target, state *SavedState) (NetworkSnapshot, error) {
	if state == nil {
		snapshot, err := discoverBeforeApply()
		if err != nil {
			return NetworkSnapshot{}, err
		}
		snapshot.TargetRoutes = make(map[string]string)
		for _, target := range targets {
			if alias, routeErr := windowsRouteInterface(target.Probe); routeErr == nil {
				snapshot.TargetRoutes[target.Prefix] = alias
			}
		}
		snapshot.InternetRoute = snapshot.VPNInterface
		return snapshot, nil
	}
	snapshot := NetworkSnapshot{
		VPNGateway: state.VPNGateway, VPNInterface: state.VPNInterface, VPNIndex: state.VPNIndex,
		LocalGateway: state.LocalGateway, LocalInterface: state.LocalInterface, LocalIndex: state.LocalIndex,
		TargetRoutes: make(map[string]string),
	}
	internet, err := windowsRouteInterface("9.9.9.9")
	if err != nil {
		return snapshot, err
	}
	snapshot.InternetRoute = internet
	// A second VPN is allowed to take over ordinary traffic after splitting.
	allGood := internet != state.VPNInterface
	for _, target := range state.Targets {
		alias, routeErr := windowsRouteInterface(target.Probe)
		if routeErr != nil {
			snapshot.TargetRoutes[target.Prefix] = "不可达"
			allGood = false
			continue
		}
		snapshot.TargetRoutes[target.Prefix] = alias
		if alias != state.VPNInterface {
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

func targetPrefix(target Target) string {
	if target.Host {
		return target.Prefix + "/32"
	}
	return target.Prefix
}

func applyRoutes(targets []Target, stateFile string) OperationResult {
	if old, err := readState(stateFile); err == nil && old != nil {
		return OperationResult{Message: "检测到尚未恢复的分流状态。请先点击“恢复路由”，再重新应用。"}
	}
	info, err := discoverWindows()
	if err != nil {
		return OperationResult{Message: "检测网络失败：" + err.Error()}
	}
	if info.VPNIndex == info.LocalIndex {
		return OperationResult{Message: "校园 VPN 尚未接管默认路由。请先关闭其他 VPN，只连接校园 VPN 后重试。"}
	}
	state := SavedState{
		Version: 1, Platform: platformName(), AppliedAt: now(), Targets: targets,
		VPNGateway: info.VPNGateway, VPNInterface: info.VPNAlias, VPNIndex: info.VPNIndex,
		LocalGateway: info.LocalGateway, LocalInterface: info.LocalAlias, LocalIndex: info.LocalIndex,
		VPNDefault:      &MetricState{InterfaceIndex: info.VPNIndex, NextHop: info.VPNGateway, RouteMetric: info.VPNRouteMetric},
		PhysicalDefault: &MetricState{InterfaceIndex: info.LocalIndex, NextHop: info.LocalGateway, RouteMetric: info.LocalMetric},
	}
	if err := writeJSON(stateFile, state); err != nil {
		return OperationResult{Message: "无法保存恢复信息，未修改路由：" + err.Error()}
	}
	var added []Target
	for _, target := range targets {
		prefix := targetPrefix(target)
		script := "$existing=@(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix " + psQuote(prefix) + " -InterfaceIndex " + strconv.Itoa(info.VPNIndex) + " -PolicyStore ActiveStore -ErrorAction SilentlyContinue);" +
			"if($existing.Count -eq 0){New-NetRoute -AddressFamily IPv4 -DestinationPrefix " + psQuote(prefix) + " -InterfaceIndex " + strconv.Itoa(info.VPNIndex) + " -NextHop " + psQuote(info.VPNGateway) + " -RouteMetric 1 -PolicyStore ActiveStore -ErrorAction Stop | Out-Null; 'ADDED'}else{'EXISTS'}"
		out, addErr := runPowerShell(script)
		if addErr != nil {
			state.AddedTargets = added
			_ = rollbackWindows(state)
			_ = os.Remove(stateFile)
			return OperationResult{Message: "添加校园路由 " + prefix + " 失败：" + addErr.Error()}
		}
		if strings.Contains(out, "ADDED") {
			added = append(added, target)
		}
	}
	state.AddedTargets = added
	if err := writeJSON(stateFile, state); err != nil {
		_ = rollbackWindows(state)
		_ = os.Remove(stateFile)
		return OperationResult{Message: "保存路由状态失败，已尝试回滚：" + err.Error()}
	}
	metricScript := "$ErrorActionPreference='Stop';" +
		"Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -InterfaceIndex " + strconv.Itoa(info.LocalIndex) + " -PolicyStore ActiveStore | Where-Object {$_.NextHop -eq " + psQuote(info.LocalGateway) + "} | Set-NetRoute -RouteMetric 1 -Confirm:$false;" +
		"Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -InterfaceIndex " + strconv.Itoa(info.VPNIndex) + " -PolicyStore ActiveStore | Where-Object {$_.NextHop -eq " + psQuote(info.VPNGateway) + "} | Set-NetRoute -RouteMetric 5000 -Confirm:$false"
	if _, err := runPowerShell(metricScript); err != nil {
		_ = rollbackWindows(state)
		_ = os.Remove(stateFile)
		return OperationResult{Message: "调整默认路由失败，已尝试回滚：" + err.Error()}
	}
	checked, checkErr := inspectRoutes(targets, &state)
	if checkErr != nil || !checked.Ready {
		return OperationResult{OK: true, Message: "路由已经修改，但自动验证未完全通过。", Snapshot: &checked, Details: []string{"可点击“重新检查”；若网络异常，请点击“恢复路由”。"}}
	}
	return OperationResult{OK: true, Message: "分流完成。现在可以开启你的另一个 VPN/代理。", Snapshot: &checked}
}

func rollbackWindows(state SavedState) error {
	var scripts []string
	for _, target := range state.AddedTargets {
		scripts = append(scripts, "Get-NetRoute -AddressFamily IPv4 -DestinationPrefix "+psQuote(targetPrefix(target))+" -InterfaceIndex "+strconv.Itoa(state.VPNIndex)+" -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false")
	}
	if state.VPNDefault != nil {
		scripts = append(scripts, "Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -InterfaceIndex "+strconv.Itoa(state.VPNDefault.InterfaceIndex)+" -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object {$_.NextHop -eq "+psQuote(state.VPNDefault.NextHop)+"} | Set-NetRoute -RouteMetric "+strconv.Itoa(state.VPNDefault.RouteMetric)+" -Confirm:$false")
	}
	if state.PhysicalDefault != nil {
		scripts = append(scripts, "Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -InterfaceIndex "+strconv.Itoa(state.PhysicalDefault.InterfaceIndex)+" -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object {$_.NextHop -eq "+psQuote(state.PhysicalDefault.NextHop)+"} | Set-NetRoute -RouteMetric "+strconv.Itoa(state.PhysicalDefault.RouteMetric)+" -Confirm:$false")
	}
	_, err := runPowerShell(strings.Join(scripts, ";"))
	return err
}

func restoreRoutes(stateFile string) OperationResult {
	state, err := readState(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return OperationResult{OK: true, Message: "没有需要恢复的分流状态。"}
		}
		return OperationResult{Message: "无法读取恢复信息：" + err.Error()}
	}
	if err := rollbackWindows(*state); err != nil {
		return OperationResult{Message: "恢复路由失败。可直接断开校园 VPN 后重新联网：" + err.Error()}
	}
	_ = os.Remove(stateFile)
	return OperationResult{OK: true, Message: "已恢复路由。也可以直接断开校园 VPN。"}
}

func isAdministrator() bool {
	result, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("IsUserAnAdmin").Call()
	return result != 0
}

func quoteWindowsArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\n\v\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range s {
		if r == '\\' {
			slashes++
			continue
		}
		if r == '"' {
			b.WriteString(strings.Repeat("\\", slashes*2+1))
			b.WriteRune(r)
			slashes = 0
			continue
		}
		b.WriteString(strings.Repeat("\\", slashes))
		slashes = 0
		b.WriteRune(r)
	}
	b.WriteString(strings.Repeat("\\", slashes*2))
	b.WriteByte('"')
	return b.String()
}

func startElevated(args []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteWindowsArg(arg)
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(self)
	params, _ := syscall.UTF16PtrFromString(strings.Join(quoted, " "))
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(self))
	proc := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
	result, _, callErr := proc.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(params)), uintptr(unsafe.Pointer(dir)), 0)
	if result <= 32 {
		return fmt.Errorf("Windows UAC 启动失败（代码 %d）：%v", result, callErr)
	}
	return nil
}

func openBrowser(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
