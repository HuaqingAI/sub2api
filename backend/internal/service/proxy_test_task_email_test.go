package service

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestProxyTaskSMTPGreetingAndTLSHandshakeHonorCancellation(t *testing.T) {
	for _, useTLS := range []bool{false, true} {
		t.Run(strconv.FormatBool(useTLS), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = listener.Close() }()
			accepted := make(chan net.Conn, 1)
			go func() {
				conn, err := listener.Accept()
				if err == nil {
					accepted <- conn
				}
			}()
			_, portString, _ := net.SplitHostPort(listener.Addr().String())
			port, _ := strconv.Atoi(portString)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			started := time.Now()
			_, _, err = connectProxyTaskSMTP(ctx, &SMTPConfig{Host: "127.0.0.1", Port: port, UseTLS: useTLS})
			if err == nil || time.Since(started) > 2*time.Second {
				t.Fatalf("SMTP phase ignored context cancellation: elapsed=%s err=%v", time.Since(started), err)
			}
			select {
			case conn := <-accepted:
				_ = conn.Close()
			case <-time.After(time.Second):
				t.Fatal("test SMTP connection was not accepted")
			}
		})
	}
}
