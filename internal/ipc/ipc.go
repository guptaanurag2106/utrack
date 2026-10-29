package ipc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
)

const (
	Magic = "i3-ipc"

	MsgTypeRunCommand = 0
	MsgTypeSubscribe  = 2
	MsgTypeGetTree    = 4

	// Event window has highest bit set: (1 << 31) | 3 = 0x80000003
	EventWindowMask = 0x80000003
)

// WindowProperties holds XWayland/X11 window properties if available.
type WindowProperties struct {
	Class    string `json:"class"`
	Instance string `json:"instance"`
	Title    string `json:"title"`
}

// Node represents a Sway window container node.
type Node struct {
	ID               int64             `json:"id"`
	Name             string            `json:"name"`
	AppID            *string           `json:"app_id"`
	WindowProperties *WindowProperties `json:"window_properties"`
	Focused          bool              `json:"focused"`
	Nodes            []*Node           `json:"nodes"`
	FloatingNodes    []*Node           `json:"floating_nodes"`
}

// WindowEvent is received when subscribing to ["window"].
type WindowEvent struct {
	Change    string `json:"change"`
	Container Node   `json:"container"`
}

// Client represents a connection to Sway IPC.
type Client struct {
	conn net.Conn
}

// GetSocketPath returns the Sway IPC socket path from environment.
func GetSocketPath() (string, error) {
	if sock := os.Getenv("SWAYSOCK"); sock != "" {
		return sock, nil
	}
	if sock := os.Getenv("I3SOCK"); sock != "" {
		return sock, nil
	}
	return "", errors.New("SWAYSOCK or I3SOCK environment variable not set (is Sway running?)")
}

// Connect establishes a connection to the Sway IPC socket.
func Connect() (*Client, error) {
	path, err := GetSocketPath()
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", path, err)
	}
	return &Client{conn: conn}, nil
}

// Close closes the IPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// SendMessage sends an IPC message with payload and reads the response payload.
func (c *Client) SendMessage(msgType uint32, payload []byte) (uint32, []byte, error) {
	header := make([]byte, 14)
	copy(header[0:6], Magic)
	binary.LittleEndian.PutUint32(header[6:10], uint32(len(payload)))
	binary.LittleEndian.PutUint32(header[10:14], msgType)

	if _, err := c.conn.Write(header); err != nil {
		return 0, nil, fmt.Errorf("write header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := c.conn.Write(payload); err != nil {
			return 0, nil, fmt.Errorf("write payload: %w", err)
		}
	}

	return c.ReadMessage()
}

// ReadMessage reads a single message/event frame from the IPC stream.
func (c *Client) ReadMessage() (uint32, []byte, error) {
	header := make([]byte, 14)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return 0, nil, err
	}

	if string(header[0:6]) != Magic {
		return 0, nil, fmt.Errorf("invalid magic in IPC stream: %q", header[0:6])
	}

	payloadLen := binary.LittleEndian.Uint32(header[6:10])
	respType := binary.LittleEndian.Uint32(header[10:14])

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return 0, nil, fmt.Errorf("read payload of %d bytes: %w", payloadLen, err)
	}

	return respType, payload, nil
}

// SubscribeWindows sends a subscription request for ["window"] events.
func (c *Client) SubscribeWindows() error {
	payload := []byte(`["window"]`)
	respType, resp, err := c.SendMessage(MsgTypeSubscribe, payload)
	if err != nil {
		return err
	}
	if respType != MsgTypeSubscribe {
		return fmt.Errorf("unexpected subscribe response type %d: %s", respType, string(resp))
	}
	return nil
}

// GetFocusedWindow queries the current tree and returns the currently focused window node.
func (c *Client) GetFocusedWindow() (*Node, error) {
	_, resp, err := c.SendMessage(MsgTypeGetTree, nil)
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	var root Node
	if err := json.Unmarshal(resp, &root); err != nil {
		return nil, fmt.Errorf("unmarshal tree: %w", err)
	}

	focused := findFocused(&root)
	return focused, nil
}

func findFocused(n *Node) *Node {
	if n == nil {
		return nil
	}
	if n.Focused && (n.AppID != nil || n.WindowProperties != nil || n.Name != "") {
		return n
	}
	for _, child := range n.Nodes {
		if f := findFocused(child); f != nil {
			return f
		}
	}
	for _, child := range n.FloatingNodes {
		if f := findFocused(child); f != nil {
			return f
		}
	}
	return nil
}

// AppName extracts the canonical application identifier from a node.
func (n *Node) AppName() string {
	if n == nil {
		return "unknown"
	}
	if n.AppID != nil && *n.AppID != "" {
		return *n.AppID
	}
	if n.WindowProperties != nil && n.WindowProperties.Class != "" {
		return n.WindowProperties.Class
	}
	if n.Name != "" {
		return n.Name
	}
	return "unknown"
}
