package pushsupport

import (
	"net/netip"
	"testing"
)

func TestBlockedAddressRanges(t *testing.T) {
	blocked := []string{
		"0.0.0.1",
		"0.255.255.255",
		"100.64.0.1",
		"100.127.255.254",
		"192.0.0.1",
		"192.0.0.255",
		"198.18.0.1",
		"198.19.255.254",
		"240.0.0.1",
		"255.255.255.255",
	}
	for _, raw := range blocked {
		if !BlockedAddress(netip.MustParseAddr(raw)) {
			t.Errorf("expected %s to be blocked", raw)
		}
	}
	allowed := []string{
		"1.1.1.1",
		"100.63.255.255",
		"100.128.0.1",
		"192.0.1.1",
		"198.17.255.255",
		"198.20.0.1",
		"223.255.255.254",
	}
	for _, raw := range allowed {
		if BlockedAddress(netip.MustParseAddr(raw)) {
			t.Errorf("expected %s to be allowed", raw)
		}
	}
}
