package ipc

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

func TestIPCFraming(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	client := &Client{conn: clientConn}

	testPayload := []byte(`{"test":true}`)

	go func() {
		// Mock server reading header and sending response
		header := make([]byte, 14)
		_, _ = serverConn.Read(header)
		reqPayload := make([]byte, len(testPayload))
		_, _ = serverConn.Read(reqPayload)

		respPayload := []byte(`[{"id":1}]`)
		respHeader := make([]byte, 14)
		copy(respHeader[0:6], Magic)
		binary.LittleEndian.PutUint32(respHeader[6:10], uint32(len(respPayload)))
		binary.LittleEndian.PutUint32(respHeader[10:14], MsgTypeSubscribe)

		_, _ = serverConn.Write(respHeader)
		_, _ = serverConn.Write(respPayload)
	}()

	respType, resp, err := client.SendMessage(MsgTypeSubscribe, testPayload)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if respType != MsgTypeSubscribe {
		t.Errorf("expected type %d, got %d", MsgTypeSubscribe, respType)
	}
	if !bytes.Equal(resp, []byte(`[{"id":1}]`)) {
		t.Errorf("unexpected payload: %s", string(resp))
	}
}

func TestFindFocused(t *testing.T) {
	appFirefox := "firefox"
	appFoot := "foot"

	root := &Node{
		ID: 1,
		Nodes: []*Node{
			{
				ID: 2,
				Nodes: []*Node{
					{
						ID:      3,
						AppID:   &appFirefox,
						Name:    "YouTube",
						Focused: false,
					},
					{
						ID:      4,
						AppID:   &appFoot,
						Name:    "zsh",
						Focused: true,
					},
				},
			},
		},
	}

	focused := findFocused(root)
	if focused == nil {
		t.Fatalf("expected focused node, got nil")
	}
	if focused.ID != 4 {
		t.Errorf("expected node 4, got %d", focused.ID)
	}
	if focused.AppName() != "foot" {
		t.Errorf("expected app name 'foot', got %s", focused.AppName())
	}
}
