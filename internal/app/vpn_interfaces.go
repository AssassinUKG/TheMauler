package app

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"mauler/internal/settings"
)

type VPNInterfaceInfo struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	CIDR      string `json:"cidr"`
	Kind      string `json:"kind"` // windows | wsl | linux
	LikelyVPN bool   `json:"likely_vpn"`
	Label     string `json:"label"`
}

func (a *App) ListVPNInterfaces() ([]VPNInterfaceInfo, error) {
	a.mu.Lock()
	cfg := *a.cfg
	a.mu.Unlock()
	return listVPNInterfacesForConfig(cfg), nil
}

func listVPNInterfacesForConfig(cfg settings.Settings) []VPNInterfaceInfo {
	var out []VPNInterfaceInfo
	out = append(out, localIPv4Interfaces()...)
	if runtime.GOOS == "windows" && strings.EqualFold(strings.TrimSpace(cfg.Tools.ShellBackend), "wsl") {
		out = append(out, wslIPv4Interfaces(cfg.Tools.ShellDistro, cfg.Tools.ShellUser)...)
	} else if runtime.GOOS != "windows" {
		out = append(out, linuxIPv4Interfaces()...)
	}
	return sortVPNInterfaces(dedupeVPNInterfaces(out))
}

func selectedVPNInfo(cfg settings.Settings) (VPNInterfaceInfo, bool) {
	want := strings.TrimSpace(cfg.Context.Lab.VPNInterface)
	if want == "" {
		return VPNInterfaceInfo{}, false
	}
	for _, item := range listVPNInterfacesForConfig(cfg) {
		if item.Name == want || item.CIDR == want || item.IP == want || item.Label == want {
			return item, true
		}
	}
	return VPNInterfaceInfo{Name: want, Label: want}, false
}

func localIPv4Interfaces() []VPNInterfaceInfo {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []VPNInterfaceInfo
	kind := "windows"
	if runtime.GOOS != "windows" {
		kind = "linux"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, cidr, ok := ipv4AddrParts(addr.String())
			if !ok {
				continue
			}
			out = append(out, vpnInterfaceInfo(iface.Name, ip, cidr, kind))
		}
	}
	return out
}

func wslIPv4Interfaces(distro, user string) []VPNInterfaceInfo {
	args := []string{}
	if strings.TrimSpace(distro) != "" {
		args = append(args, "-d", strings.TrimSpace(distro))
	}
	if strings.TrimSpace(user) != "" {
		args = append(args, "-u", strings.TrimSpace(user))
	}
	args = append(args, "--", "ip", "-o", "-4", "addr", "show")
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "wsl.exe", args...).Output()
	if err != nil {
		return nil
	}
	return parseIPAddrShow(string(out), "wsl")
}

func linuxIPv4Interfaces() []VPNInterfaceInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", "-o", "-4", "addr", "show").Output()
	if err != nil {
		return nil
	}
	return parseIPAddrShow(string(out), "linux")
}

var ipAddrLineRE = regexp.MustCompile(`^\s*\d+:\s+([^:\s]+).*?\binet\s+([0-9.]+/\d+)`)

func parseIPAddrShow(raw, kind string) []VPNInterfaceInfo {
	var out []VPNInterfaceInfo
	for _, line := range strings.Split(raw, "\n") {
		match := ipAddrLineRE.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		name := strings.TrimSuffix(match[1], "@")
		if idx := strings.Index(name, "@"); idx >= 0 {
			name = name[:idx]
		}
		ip, cidr, ok := ipv4AddrParts(match[2])
		if !ok {
			continue
		}
		out = append(out, vpnInterfaceInfo(name, ip, cidr, kind))
	}
	return out
}

func ipv4AddrParts(cidr string) (string, string, bool) {
	cidr = strings.TrimSpace(cidr)
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil || ip == nil || ip.To4() == nil {
		return "", "", false
	}
	return ip.String(), cidr, true
}

func vpnInterfaceInfo(name, ip, cidr, kind string) VPNInterfaceInfo {
	name = strings.TrimSpace(name)
	ip = strings.TrimSpace(ip)
	cidr = strings.TrimSpace(cidr)
	info := VPNInterfaceInfo{
		Name:      name,
		IP:        ip,
		CIDR:      cidr,
		Kind:      strings.TrimSpace(kind),
		LikelyVPN: likelyVPNInterface(name, ip),
	}
	info.Label = vpnInterfaceLabel(info)
	return info
}

func likelyVPNInterface(name, ip string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(lower, "tun") ||
		strings.HasPrefix(lower, "tap") ||
		strings.HasPrefix(lower, "wg") ||
		strings.HasPrefix(lower, "ppp") ||
		strings.Contains(lower, "tailscale") ||
		strings.Contains(lower, "openvpn") ||
		strings.Contains(lower, "wireguard") {
		return true
	}
	return strings.HasPrefix(ip, "10.10.") || strings.HasPrefix(ip, "10.129.")
}

func vpnInterfaceLabel(info VPNInterfaceInfo) string {
	label := info.Name
	if info.IP != "" {
		label += " · " + info.IP
	}
	if info.Kind != "" {
		label += " · " + info.Kind
	}
	if info.LikelyVPN {
		label += " · VPN"
	}
	return label
}

func dedupeVPNInterfaces(items []VPNInterfaceInfo) []VPNInterfaceInfo {
	seen := map[string]bool{}
	out := make([]VPNInterfaceInfo, 0, len(items))
	for _, item := range items {
		key := fmt.Sprintf("%s|%s|%s", item.Kind, item.Name, item.CIDR)
		if item.Name == "" || item.IP == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func sortVPNInterfaces(items []VPNInterfaceInfo) []VPNInterfaceInfo {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].LikelyVPN != items[j].LikelyVPN {
			return items[i].LikelyVPN
		}
		if items[i].Kind != items[j].Kind {
			return items[i].Kind == "wsl"
		}
		return items[i].Name < items[j].Name
	})
	return items
}
