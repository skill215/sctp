//go:build linux && !386

package sctp

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIPv6ZoneNameIndexConflict(t *testing.T) {
	const parentNamespaceEnv = "SCTP_TEST_ZONE_PARENT_NETNS"
	currentNamespace, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Skipf("network namespaces unavailable: %v", err)
	}
	if parentNamespace := os.Getenv(parentNamespaceEnv); parentNamespace != "" {
		if currentNamespace == parentNamespace {
			t.Fatal("interface creation requires an isolated network namespace")
		}
		// "1" also collides with the loopback interface's numeric index.
		// Both interfaces disappear when this child process exits.
		for _, name := range []string{"1", "42"} {
			if output, err := exec.Command("ip", "link", "add", "name", name, "type", "dummy").CombinedOutput(); err != nil {
				t.Fatalf("create interface %q: %v\n%s", name, err, output)
			}
		}
		for _, name := range []string{"1", "42"} {
			ifi, err := net.InterfaceByName(name)
			if err != nil {
				t.Fatal(err)
			}
			index := strconv.Itoa(ifi.Index)
			if index == name {
				t.Fatalf("interface %q must differ from its index", name)
			}
			for _, zone := range []string{name, index} {
				addr, err := ResolveSCTPAddr("sctp6", "[fe80::1%"+zone+"]:9000")
				if err != nil {
					t.Fatal(err)
				}
				raw := addr.ToRawSockAddrBuf()
				if got := nativeEndian.Uint32(raw[24:28]); got != uint32(ifi.Index) {
					t.Fatalf("zone %q encoded scope_id=%d, want interface %q index %d", zone, got, name, ifi.Index)
				}
				decoded, err := resolveFromRawAddr(raw, 1)
				if err != nil {
					t.Fatal(err)
				}
				if decoded.IPAddrs[0].Zone != name {
					t.Fatalf("decoded zone=%q, want interface name %q", decoded.IPAddrs[0].Zone, name)
				}
				if got := nativeEndian.Uint32(decoded.ToRawSockAddrBuf()[24:28]); got != uint32(ifi.Index) {
					t.Fatalf("round-trip changed interface index to %d", got)
				}
			}
		}
		return
	}
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		t.Skip("unshare is required for the isolated interface test")
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skip("iproute2 is required for the isolated interface test")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, unshare, "--user", "--map-root-user", "--net", executable,
		"-test.run=^TestIPv6ZoneNameIndexConflict$", "-test.v", "-test.timeout=10s")
	child.Env = append(os.Environ(), parentNamespaceEnv+"="+currentNamespace)
	output, err := child.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "unshare failed:") &&
			(strings.Contains(string(output), "Operation not permitted") || strings.Contains(string(output), "Invalid argument")) {
			t.Skipf("isolated network namespaces unavailable: %s", output)
		}
		t.Fatalf("isolated interface test failed: %v\n%s", err, output)
	}
	t.Logf("isolated interface test:\n%s", output)
}
