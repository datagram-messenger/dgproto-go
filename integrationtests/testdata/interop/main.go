// Test-only synthetic-session loopback peer. No user keys or handshake bypass API.
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return err
	}
	fmt.Println(listener.Addr())
	conn, err := listener.AcceptTCP()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return err
	}
	secrets := dgproto.HandshakeSecrets{}
	for i := range secrets.SessionID {
		secrets.SessionID[i] = 1
	}
	for i := range secrets.SendKey {
		secrets.SendKey[i] = 2
		secrets.ReceiveKey[i] = 3
	}
	session, err := dgproto.NewSession(dgproto.CipherChaCha20Poly1305, secrets)
	if err != nil {
		return err
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	transport := dgproto.NewTCPTransport(conn)
	write := func(frame dgproto.Frame) error { return transport.WriteFrame(ctx, frame) }
	for _, tc := range []struct {
		code uint16
		text string
	}{{3, "hi"}, {255, "Ошибка"}} {
		var frame dgproto.Frame
		if tc.code == 3 {
			frame, err = session.SendPayload(dgproto.MessageTypeError, []byte{3, 0, 'h', 'i'}, 7)
		} else {
			frame, err = session.Send(dgproto.ErrorMessage{Code: tc.code, Context: tc.text}, 0)
		}
		if err != nil {
			return err
		}
		if err := write(frame); err != nil {
			return err
		}
		inbound, err := transport.ReadFrame(ctx)
		if err != nil {
			return err
		}
		value, err := session.Receive(inbound)
		if err != nil {
			return err
		}
		report, ok := value.(*dgproto.ErrorMessage)
		if !ok || report.Code != tc.code || report.Context != tc.text {
			return fmt.Errorf("Rust Error meaning mismatch: %#v", value)
		}
	}
	ping, err := session.Send(dgproto.PingPong{Nonce: 42}, 0)
	if err != nil {
		return err
	}
	if err := write(ping); err != nil {
		return err
	}
	inbound, err := transport.ReadFrame(ctx)
	if err != nil {
		return err
	}
	value, err := session.Receive(inbound)
	if err != nil {
		return err
	}
	pong, ok := value.(*dgproto.PingPong)
	if !ok || !pong.IsResponse || pong.Nonce != 42 {
		return fmt.Errorf("control mismatch: %#v", value)
	}
	closeFrame, err := session.Send(dgproto.SessionClose{Code: 3, Reason: "hi"}, 0)
	if err != nil {
		return err
	}
	if err := write(closeFrame); err != nil {
		return err
	}
	inbound, err = transport.ReadFrame(ctx)
	if err != nil {
		return err
	}
	value, err = session.Receive(inbound)
	if err != nil {
		return err
	}
	closeValue, ok := value.(*dgproto.SessionClose)
	if !ok || closeValue.Code != 3 || closeValue.Reason != "hi" {
		return fmt.Errorf("Close mismatch: %#v", value)
	}
	fmt.Println("PASS: Go/Rust encrypted Session Error codes 3,255 both directions; raw oracle, padding, Ping/Pong and TLV Close")
	return nil
}
