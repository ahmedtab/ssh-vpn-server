//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

// ── Server / tunnel constants ─────────────────────────────────────────────────
// Edit these to match your deployment before building.
const (
	sshHost     = "157.180.4.166"
	sshPort     = "2255"
	sshUser     = "root"
	tunSlot     = 0 // must match the tunnel="N" prefix in authorized_keys
	tunLocalIP  = "10.10.11.1"
	tunNetmask  = "255.255.255.252"
	tunMTU      = "1340"
	adapterName = "SSH-VPN-POC"
	lanSubnet   = "192.168.4.0"
	lanMask     = "255.255.255.0"
	lanGateway  = "10.10.11.2" // server-side TUN IP, used as next-hop for LAN route

	// fullTunnel = true  → route ALL internet through VPN (ifconfig.me returns server IP)
	// fullTunnel = false → split tunnel, only 192.168.4.0/24 goes through VPN
	fullTunnel = false
)

// SSH_TUNMODE_POINTOPOINT — matches OpenSSH channel open payload (tun@openssh.com)
const tunModePointToPoint = uint32(1)

// AF values sent as 4-byte big-endian prefix on every packet over the SSH channel.
// The server (Linux) uses Linux AF numbers: AF_INET=2, AF_INET6=10.
const (
	afInet  = uint32(2)
	afInet6 = uint32(10)
)

func logf(format string, a ...any) {
	fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
}

// defaultGateway returns the current IPv4 default gateway by parsing `route print 0.0.0.0`.
func defaultGateway() (string, error) {
	out, err := exec.Command("route", "print", "0.0.0.0").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "0.0.0.0" && f[1] == "0.0.0.0" {
			return f[2], nil
		}
	}
	return "", fmt.Errorf("default route not found in routing table")
}

// netsh runs a netsh command and logs any warning on failure (non-fatal for POC).
func netsh(args ...string) {
	out, err := exec.Command("netsh", args...).CombinedOutput()
	if err != nil {
		logf("WARN  netsh %v → %v: %s", args, err, string(out))
	}
}

func main() {
	// ── 1. Load SSH private key ───────────────────────────────────────────────
	keyPath := filepath.Join(os.Getenv("USERPROFILE"), ".ssh", "id_ed25519")
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		logf("ERROR  reading SSH key %s: %v", keyPath, err)
		logf("       Set USERPROFILE or adjust the keyPath constant in main.go")
		os.Exit(1)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		logf("ERROR  parsing SSH key: %v", err)
		os.Exit(1)
	}
	logf("SSH key loaded from %s", keyPath)

	// ── 2. Create wintun TUN adapter ─────────────────────────────────────────
	// wintun.dll must be next to this exe. Download from:
	//   https://www.wintun.net/builds/wintun-0.14.1.zip → amd64/wintun.dll
	logf("Creating wintun adapter %q…", adapterName)
	adapter, err := wintun.CreateAdapter(adapterName, "Wintun", nil)
	if err != nil {
		logf("ERROR  CreateAdapter: %v", err)
		logf("       Is wintun.dll (amd64) in the same folder as poc.exe?")
		logf("       Download: https://www.wintun.net/builds/wintun-0.14.1.zip")
		os.Exit(1)
	}
	logf("Adapter %q created", adapterName)

	// ── 3. Assign IP address and MTU ─────────────────────────────────────────
	logf("Configuring adapter  IP=%s  netmask=%s  MTU=%s", tunLocalIP, tunNetmask, tunMTU)
	netsh("interface", "ip", "set", "address", adapterName, "static", tunLocalIP, tunNetmask)
	netsh("interface", "ipv4", "set", "subinterface", adapterName, "mtu="+tunMTU, "store=active")

	// ── 4. Start wintun packet session (4 MB ring) ────────────────────────────
	session, err := adapter.StartSession(0x400000)
	if err != nil {
		logf("ERROR  StartSession: %v", err)
		adapter.Close()
		os.Exit(1)
	}
	logf("Wintun session started")

	// ── 5. SSH dial ───────────────────────────────────────────────────────────
	logf("Dialing SSH %s:%s…", sshHost, sshPort)
	sshCfg := &ssh.ClientConfig{
		User: sshUser,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		// ⚠ InsecureIgnoreHostKey is acceptable for a local-network POC.
		// Replace with ssh.FixedHostKey / knownhosts.New in production.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	}
	sshConn, err := ssh.Dial("tcp", net.JoinHostPort(sshHost, sshPort), sshCfg)
	if err != nil {
		logf("ERROR  SSH dial: %v", err)
		session.End()
		adapter.Close()
		os.Exit(1)
	}
	logf("SSH connected to %s:%s", sshHost, sshPort)

	// ── 6. Open tun@openssh.com channel ──────────────────────────────────────
	// Channel open extra data (SSH wire format, big-endian uint32 × 2):
	//   [0:4]  mode = 1  (SSH_TUNMODE_POINTOPOINT)
	//   [4:8]  unit = N  (tun device slot on the server)
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], tunModePointToPoint)
	binary.BigEndian.PutUint32(payload[4:8], uint32(tunSlot))

	logf("Opening tun@openssh.com channel (slot %d)…", tunSlot)
	tunCh, reqs, err := sshConn.OpenChannel("tun@openssh.com", payload)
	if err != nil {
		logf("ERROR  OpenChannel tun@openssh.com: %v", err)
		logf("       Server sshd_config must have:  PermitTunnel point-to-point")
		logf("       authorized_keys entry must have: tunnel=\"%d\" prefix for this key", tunSlot)
		sshConn.Close()
		session.End()
		adapter.Close()
		os.Exit(1)
	}
	go ssh.DiscardRequests(reqs)
	logf("SSH TUN channel open")

	// ── 6b. Configure server-side tun via SSH exec ────────────────────────────
	// OpenSSH creates tun<N> on the server but leaves it DOWN with no IP.
	// nm-ssh sends a remote ip command to fix this; we must do the same.
	serverTunDev := fmt.Sprintf("tun%d", tunSlot)
	// lanGateway  = server TUN IP (10.10.10.2)
	// tunLocalIP  = client TUN IP (10.10.10.1, used as peer from server POV)
	serverCfgCmd := fmt.Sprintf(
		"ip link set %s mtu %s 2>/dev/null; "+
			"ip addr flush dev %s 2>/dev/null; "+
			"ip addr add %s/32 peer %s dev %s; "+
			"ip link set %s up",
		serverTunDev, tunMTU,
		serverTunDev,
		lanGateway, tunLocalIP, serverTunDev,
		serverTunDev,
	)
	logf("Configuring server-side %s  (%s <-> %s)…", serverTunDev, lanGateway, tunLocalIP)
	cfgSess, err := sshConn.NewSession()
	if err != nil {
		logf("WARN  could not open exec session for server tun config: %v", err)
	} else {
		out, runErr := cfgSess.CombinedOutput(serverCfgCmd)
		cfgSess.Close()
		if runErr != nil {
			logf("WARN  server tun config: %v — %s", runErr, string(out))
		} else {
			logf("Server-side %s is UP", serverTunDev)
		}
	}

	// ── 7. Add split-tunnel route for remote LAN ──────────────────────────────
	logf("Adding route  %s/24 via %s…", lanSubnet, lanGateway)
	if out, err := exec.Command("route", "add", lanSubnet, "mask", lanMask, lanGateway).CombinedOutput(); err != nil {
		logf("WARN  route add: %v: %s", err, string(out))
	}

	// ── 7b. Full-tunnel routes (optional) ────────────────────────────────────
	// Splits the default route into two /1 routes that cover all of IPv4 without
	// replacing 0.0.0.0/0, which would break the SSH connection itself.
	var origGW string
	if fullTunnel {
		gw, gwErr := defaultGateway()
		if gwErr != nil {
			logf("WARN  full tunnel: cannot detect default gateway: %v", gwErr)
		} else {
			origGW = gw
			logf("Full tunnel: routing all internet via %s  (original gateway %s preserved for SSH)", lanGateway, origGW)
			// Keep the SSH server reachable via the original gateway
			exec.Command("route", "add", sshHost, "mask", "255.255.255.255", origGW).Run() //nolint:errcheck
			// Cover all IPv4 via TUN (two /1 routes beat the existing /0 default)
			exec.Command("route", "add", "0.0.0.0", "mask", "128.0.0.0", lanGateway).Run()   //nolint:errcheck
			exec.Command("route", "add", "128.0.0.0", "mask", "128.0.0.0", lanGateway).Run() //nolint:errcheck
			logf("Full tunnel routes active — curl ifconfig.me should return server IP")
		}
	}

	// ── 8. Packet bridge goroutines ───────────────────────────────────────────

	sshClosed := make(chan struct{})
	readWait := session.ReadWaitEvent()

	var txPkts, rxPkts atomic.Int64

	// TUN → SSH
	// Reads IP packets from the wintun ring and forwards them to the SSH channel
	// with a 4-byte AF prefix (Linux convention used by OpenSSH point-to-point mode).
	go func() {
		for {
			// Block up to 500 ms waiting for packets in the wintun ring.
			// Using a timeout (instead of INFINITE) keeps shutdown responsive.
			windows.WaitForSingleObject(readWait, 500) //nolint:errcheck

			// Drain every available packet from the ring.
			for {
				pkt, err := session.ReceivePacket()
				if err != nil {
					if err == windows.ERROR_NO_MORE_ITEMS {
						break // ring empty → wait again
					}
					return // session ended (ERROR_HANDLE_EOF or similar)
				}

				// Detect address family from IP version nibble.
				af := afInet
				if len(pkt) > 0 && pkt[0]>>4 == 6 {
					af = afInet6
				}

				frame := make([]byte, 4+len(pkt))
				binary.BigEndian.PutUint32(frame, af)
				copy(frame[4:], pkt)
				session.ReleaseReceivePacket(pkt)

				n := txPkts.Add(1)
				if n == 1 {
					logf("TUN→SSH first packet (%d bytes) — wintun is sending", len(pkt))
				}

				if _, werr := tunCh.Write(frame); werr != nil {
					return // SSH channel gone
				}
			}
		}
	}()

	// SSH → TUN
	// Reads framed packets from the SSH channel, strips the 4-byte AF prefix,
	// and injects the raw IP packets into the wintun ring.
	go func() {
		defer close(sshClosed)
		// Maximum packet: 4-byte AF prefix + 65535-byte IP packet
		buf := make([]byte, 4+65535)
		for {
			n, err := tunCh.Read(buf)
			if err != nil {
				return // SSH channel closed by remote or locally
			}
			if n <= 4 {
				continue // too short: AF prefix only, no IP header — discard
			}
			// buf[0:4] = AF prefix (strip); buf[4:n] = raw IPv4/IPv6 packet
			ipPkt := buf[4:n]

			rn := rxPkts.Add(1)
			if rn == 1 {
				logf("SSH→TUN first packet (%d bytes) — data is flowing!", len(ipPkt))
			}

			dst, err := session.AllocateSendPacket(len(ipPkt))
			if err != nil {
				return // session ended
			}
			copy(dst, ipPkt)
			session.SendPacket(dst)
		}
	}()

	logf("════════════════════════════════════════════")
	logf("Tunnel UP")
	logf("  Local  : %s (adapter %q)", tunLocalIP, adapterName)
	logf("  Remote : %s  via %s:%s", lanGateway, sshHost, sshPort)
	logf("  LAN    : %s/24", lanSubnet)
	logf("Press Ctrl+C to disconnect")
	logf("════════════════════════════════════════════")

	// ── 9. Wait for Ctrl+C or SSH channel close ───────────────────────────────
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
		logf("Signal received — disconnecting…")
	case <-sshClosed:
		logf("SSH channel closed by server — disconnecting…")
	}

	// ── Cleanup ───────────────────────────────────────────────────────────────
	tunCh.Close()
	sshConn.Close()
	session.End()                                    // unblocks TUN→SSH goroutine
	exec.Command("route", "delete", lanSubnet).Run() //nolint:errcheck
	if fullTunnel && origGW != "" {
		exec.Command("route", "delete", sshHost, "mask", "255.255.255.255", origGW).Run()   //nolint:errcheck
		exec.Command("route", "delete", "0.0.0.0", "mask", "128.0.0.0", lanGateway).Run()   //nolint:errcheck
		exec.Command("route", "delete", "128.0.0.0", "mask", "128.0.0.0", lanGateway).Run() //nolint:errcheck
	}
	adapter.Close() // removes the adapter from Windows
	logf("Done.")
}
