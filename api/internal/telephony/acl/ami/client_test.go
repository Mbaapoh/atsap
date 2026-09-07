package ami

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestReadMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Message
		wantErr bool
	}{
		{
			name:  "simple event",
			input: "Event: Newchannel\r\nChannel: SIP/1000-1\r\n\r\n",
			want:  Message{"Event": "Newchannel", "Channel": "SIP/1000-1"},
		},
		{
			name:  "trims whitespace around key and value",
			input: "Event:  Newchannel  \r\n  Channel :SIP/1000-1\r\n\r\n",
			want:  Message{"Event": "Newchannel", "Channel": "SIP/1000-1"},
		},
		{
			name:  "skips lines without a colon",
			input: "Event: Newchannel\r\nnotakeyvalue\r\nChannel: SIP/1000-1\r\n\r\n",
			want:  Message{"Event": "Newchannel", "Channel": "SIP/1000-1"},
		},
		{
			name:  "empty message on immediate blank line",
			input: "\r\n",
			want:  Message{},
		},
		{
			name:    "EOF before blank line returns error",
			input:   "Event: Newchannel\r\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tt.input))
			got, err := readMessage(r)
			if tt.wantErr {
				if err == nil {
					t.Fatal("readMessage() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("readMessage() returned error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("readMessage() = %+v, want %+v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("readMessage()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestClient_Send_NotConnected(t *testing.T) {
	c := New("127.0.0.1:0", "u", "p", nil, discardLogger())
	if err := c.Send(Message{"Action": "Ping"}); err == nil {
		t.Fatal("Send() expected error when not connected, got nil")
	}
}

func TestClient_Close_NotConnected(t *testing.T) {
	c := New("127.0.0.1:0", "u", "p", nil, discardLogger())
	if err := c.Close(); err != nil {
		t.Errorf("Close() on unconnected client returned error: %v", err)
	}
}

// fakeAsterisk starts a TCP listener that speaks just enough AMI to drive
// Client.Connect: it sends the banner, waits for the Login action, then
// responds according to loginSuccess. On success it also emits one event.
func fakeAsterisk(t *testing.T, loginSuccess bool) (addr string, done <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		_, _ = conn.Write([]byte("Asterisk Call Manager/9.0.0\r\n"))

		reader := bufio.NewReader(conn)
		if _, err := readMessage(reader); err != nil {
			return
		}

		if loginSuccess {
			_, _ = conn.Write([]byte("Response: Success\r\nMessage: Authentication accepted\r\n\r\n"))
			_, _ = conn.Write([]byte("Event: FullyBooted\r\nStatus: Fully Booted\r\n\r\n"))
		} else {
			_, _ = conn.Write([]byte("Response: Error\r\nMessage: Authentication failed\r\n\r\n"))
			return
		}

		// Keep the connection open briefly so the client's read loop has a
		// chance to dispatch the event before the test tears things down.
		time.Sleep(50 * time.Millisecond)
	}()

	return ln.Addr().String(), finished
}

func TestClient_Connect_Success(t *testing.T) {
	addr, done := fakeAsterisk(t, true)

	var mu sync.Mutex
	var events []Message
	c := New(addr, "voipapp", "devpassword123", func(m Message) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, m)
	}, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := c.Connect(ctx)
	<-done
	if err == nil {
		t.Fatal("Connect() expected error once the fake server closes the connection, got nil")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("received %d events, want 1", len(events))
	}
	if events[0]["Event"] != "FullyBooted" {
		t.Errorf("event = %+v, want Event=FullyBooted", events[0])
	}
}

func TestClient_Connect_LoginFailure(t *testing.T) {
	addr, done := fakeAsterisk(t, false)

	c := New(addr, "voipapp", "wrong-password", nil, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := c.Connect(ctx)
	<-done
	if err == nil {
		t.Fatal("Connect() expected login failure error, got nil")
	}
	if !strings.Contains(err.Error(), "login failed") {
		t.Errorf("Connect() error = %q, want it to mention login failed", err.Error())
	}
}

func TestClient_Connect_DialError(t *testing.T) {
	c := New("127.0.0.1:1", "u", "p", nil, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := c.Connect(ctx)
	if err == nil {
		t.Fatal("Connect() expected dial error, got nil")
	}
	if !strings.Contains(err.Error(), "ami: dial") {
		t.Errorf("Connect() error = %q, want it to mention ami: dial", err.Error())
	}
}
