package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

type HostClient struct {
	conn *websocket.Conn
	hub  *RoomHub
	send chan []byte
}

func NewHostClient(conn *websocket.Conn, hub *RoomHub) *HostClient {
	return &HostClient{
		conn: conn,
		hub:  hub,
		send: make(chan []byte, 64),
	}
}

// Send は clientSender インターフェースの実装。チャネルが満杯の場合はドロップする。
func (c *HostClient) Send(msg []byte) {
	select {
	case c.send <- msg:
	default:
	}
}

// ReadPump はWS受信ループ。pong メッセージのみ hub へ転送する。
func (c *HostClient) ReadPump(ctx context.Context) {
	defer func() {
		c.hub.UnregisterHost()
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}

		var msg IncomingMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Type != EventTypePong {
			continue
		}

		select {
		case c.hub.gameEvt <- gameEvent{
			Type:            EventTypePong,
			ServerTimestamp: time.Now().UnixMilli(),
			ClientTimestamp: msg.ClientTimestamp,
		}:
		default:
		}
	}
}

// WritePump は send チャネルからWSへの書き込みループ。
func (c *HostClient) WritePump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		}
	}
}

// ServeHost はホスト画面のWSハンドシェイクからポンプ起動までを行うハンドラ。
func ServeHost(hub *RoomHub, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	client := NewHostClient(conn, hub)
	hub.RegisterHost(client)

	ctx := r.Context()
	go client.WritePump(ctx)
	client.ReadPump(ctx)
}
