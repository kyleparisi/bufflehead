package license

import (
	"errors"
	"net"
)

// primaryMACAddress returns the six raw bytes of the primary network
// interface's hardware address, which is the "device GUID" Apple's receipt
// device-binding hash is computed over.
//
// Apple specifies the *primary* interface, which on Mac hardware is en0 — the
// built-in Ethernet or Wi-Fi NIC. Picking a different interface (a VPN tun, a
// Thunderbolt dock, a virtual bridge) produces a different GUID and a spurious
// hash mismatch, so en0 is tried by name first and only then do we fall back to
// scanning. The fallback deliberately skips loopback, point-to-point and
// virtual interfaces for the same reason.
func primaryMACAddress() ([]byte, error) {
	if iface, err := net.InterfaceByName("en0"); err == nil && len(iface.HardwareAddr) == 6 {
		return append([]byte(nil), iface.HardwareAddr...), nil
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		if len(iface.HardwareAddr) != 6 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		if isVirtualInterface(iface.Name) {
			continue
		}
		return append([]byte(nil), iface.HardwareAddr...), nil
	}
	return nil, errors.New("no primary network interface with a hardware address")
}

// isVirtualInterface reports whether a macOS interface name belongs to a
// virtual device that must not be mistaken for the primary NIC.
func isVirtualInterface(name string) bool {
	// awdl/llw: Apple Wireless Direct Link. bridge: Internet Sharing and VM
	// bridges. utun/ipsec/ppp: VPN tunnels. vmnet/vnic: virtualisation.
	prefixes := []string{"awdl", "llw", "bridge", "utun", "ipsec", "ppp", "vmnet", "vnic", "ap"}
	for _, p := range prefixes {
		if len(name) >= len(p) && name[:len(p)] == p {
			return true
		}
	}
	return false
}
