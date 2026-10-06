package notifications

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestSOCKSEndpointAndTransportSecurity(t *testing.T) {
	for _, endpoint := range []string{"localhost:5310", "192.168.1.1:5310", "0.0.0.0:5310", "127.0.0.1:0", "[::1%lo]:5310", "https://127.0.0.1:5310", "127.0.0.1:65536"} {
		if _, err := NewServiceWithSOCKS(endpoint); err != Error("unsafe-destination") {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
	for _, endpoint := range []string{"", "127.0.0.1:5310", "[::1]:5310"} {
		s, err := NewServiceWithSOCKS(endpoint)
		if err != nil || s.socksAddress != endpoint {
			t.Fatal("valid endpoint rejected")
		}
		transport := s.transport.client.Transport.(*http.Transport)
		if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.ServerName != telegramHost {
			t.Fatal("TLS or environment-proxy boundary weakened")
		}
		if s.transport.client.CheckRedirect(nil, nil) != Error("provider-rejected") {
			t.Fatal("redirect accepted")
		}
	}
}

func TestSOCKSOnlyReceivesValidatedNumericTelegramDestination(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			result <- nil
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		greeting := make([]byte, 3)
		if _, err = io.ReadFull(conn, greeting); err != nil {
			result <- nil
			return
		}
		_, _ = conn.Write([]byte{5, 0})
		request := make([]byte, 10)
		if _, err = io.ReadFull(conn, request); err != nil {
			result <- nil
			return
		}
		_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1})
		result <- append(greeting, request...)
	}()
	lookup := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	dial, err := telegramTransportDial(listener.Addr().String(), lookup)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := dial(ctx, "tcp", "other.invalid:443"); err != Error("unsafe-destination") {
		t.Fatal("other destination accepted")
	}
	conn, err := dial(ctx, "tcp", telegramAddress)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	packet := <-result
	if len(packet) != 13 || packet[0] != 5 || packet[3] != 5 || packet[4] != 1 || packet[6] != 1 || !net.IP(packet[7:11]).Equal(net.ParseIP("8.8.8.8")) || binary.BigEndian.Uint16(packet[11:]) != 443 {
		t.Fatalf("invalid SOCKS connect packet: %v", packet)
	}
	unsafeLookup := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	dial, _ = telegramTransportDial(listener.Addr().String(), unsafeLookup)
	if _, err = dial(ctx, "tcp", telegramAddress); err != Error("unsafe-destination") {
		t.Fatal("private destination accepted")
	}
}

func TestSOCKSCancelledHandshakeDoesNotFallback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	lookup := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	dial, err := telegramTransportDial(listener.Addr().String(), lookup)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if conn, err := dial(ctx, "tcp", telegramAddress); err == nil {
		conn.Close()
		t.Fatal("stalled proxy succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("proxy handshake ignored cancellation")
	}
}
