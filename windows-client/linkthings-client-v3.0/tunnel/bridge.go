package tunnel

import "encoding/binary"

// Address-family header values for the OpenSSH tun@openssh.com channel
// framing: a 4-byte big-endian address family prefix ahead of each raw IP
// packet. This is a wire contract with the server side (see docs/PROTOCOL.md)
// and must stay byte-for-byte identical across every platform.
const (
	afInet  = uint32(2)
	afInet6 = uint32(10)
)

// startBridge runs the two packet-forwarding goroutines between the tunnel
// device and the SSH tun channel. It is written once, platform-independent:
// every Platform's TunDevice is a plain io.ReadWriteCloser, so the framing
// logic here never needs to know which OS it's running on.
func (at *activeTunnel) startBridge() {
	go func() {
		buf := make([]byte, 65535)
		for {
			if at.closed.Load() {
				return
			}
			n, err := at.dev.Read(buf)
			if err != nil {
				return
			}
			pkt := buf[:n]

			af := afInet
			if len(pkt) > 0 && pkt[0]>>4 == 6 {
				af = afInet6
			}

			frame := make([]byte, 4+n)
			binary.BigEndian.PutUint32(frame[0:4], af)
			copy(frame[4:], pkt)
			at.txBytes.Add(uint64(n))

			if _, err := at.tunCh.Write(frame); err != nil {
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, 4+65535)
		for {
			if at.closed.Load() {
				return
			}
			n, err := at.tunCh.Read(buf)
			if err != nil {
				return
			}
			if n <= 4 {
				continue
			}
			ipPkt := buf[4:n]
			at.rxBytes.Add(uint64(len(ipPkt)))
			if at.closed.Load() {
				return
			}
			if _, err := at.dev.Write(ipPkt); err != nil {
				return
			}
		}
	}()
}
