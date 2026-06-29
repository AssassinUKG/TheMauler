package app

import "testing"

func TestParseIPAddrShowDetectsLikelyVPNs(t *testing.T) {
	raw := `1: lo    inet 127.0.0.1/8 scope host lo
2: eth0    inet 172.22.1.12/20 brd 172.22.15.255 scope global eth0
3: tun0    inet 10.10.14.23/23 scope global tun0
4: wg0    inet 10.8.0.2/24 scope global wg0`
	got := parseIPAddrShow(raw, "wsl")
	if len(got) != 4 {
		t.Fatalf("parsed %d interfaces, want 4: %#v", len(got), got)
	}
	var tun, wg VPNInterfaceInfo
	for _, item := range got {
		if item.Name == "tun0" {
			tun = item
		}
		if item.Name == "wg0" {
			wg = item
		}
	}
	if tun.IP != "10.10.14.23" || tun.CIDR != "10.10.14.23/23" || !tun.LikelyVPN || tun.Kind != "wsl" {
		t.Fatalf("bad tun0 parse: %#v", tun)
	}
	if wg.IP != "10.8.0.2" || !wg.LikelyVPN {
		t.Fatalf("bad wg0 parse: %#v", wg)
	}
}

func TestSortVPNInterfacesPrioritizesLikelyVPNAndWSL(t *testing.T) {
	items := []VPNInterfaceInfo{
		vpnInterfaceInfo("Ethernet", "192.168.1.2", "192.168.1.2/24", "windows"),
		vpnInterfaceInfo("tun0", "10.10.14.23", "10.10.14.23/23", "wsl"),
		vpnInterfaceInfo("wg0", "10.8.0.2", "10.8.0.2/24", "windows"),
	}
	got := sortVPNInterfaces(items)
	if got[0].Name != "tun0" {
		t.Fatalf("expected WSL tun0 first, got %#v", got)
	}
}

func TestSelectedVPNInfoMatchesNameIPOrCIDR(t *testing.T) {
	item := vpnInterfaceInfo("tun0", "10.10.14.23", "10.10.14.23/23", "wsl")
	if item.Label == "" || !item.LikelyVPN {
		t.Fatalf("expected useful label/vpn signal: %#v", item)
	}
}
