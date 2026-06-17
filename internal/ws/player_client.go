package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

type PlayerClient struct {
	conn     *websocket.Conn
	hub      *RoomHub
	playerID string
	send     chan []byte
}

func NewPlayerClient(conn *websocket.Conn, hub *RoomHub, playerID string) *PlayerClient {
	return &PlayerClient{
		conn:     conn,
		hub:      hub,
		playerID: playerID,
		send:     make(chan []byte, 64),
	}
}

// Send は clientSender インターフェースの実装。チャネルが満杯の場合はドロップする。
func (c *PlayerClient) Send(msg []byte) {
	select {
	case c.send <- msg:
	default:
	}
}

// ReadPump はWS受信ループ。pull/release/pong を hub へ転送する。
func (c *PlayerClient) ReadPump(ctx context.Context) {
	defer func() {
		c.hub.UnregisterPlayer(c.playerID)
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

		switch msg.Type {
		case EventTypePull, EventTypeRelease, EventTypePong:
		default:
			continue
		}

		select {
		case c.hub.gameEvt <- gameEvent{
			Type:            msg.Type,
			PlayerID:        c.playerID,
			ClientTimestamp: msg.ClientTimestamp,
			ServerTimestamp: time.Now().UnixMilli(),
		}:
		default:
		}
	}
}

// WritePump は send チャネルからWSへの書き込みループ。
func (c *PlayerClient) WritePump(ctx context.Context) {
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

// ServePlayer はコントローラー画面のWSハンドシェイクからポンプ起動までを行うハンドラ。
func ServePlayer(hub *RoomHub, playerID string, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	client := NewPlayerClient(conn, hub, playerID)
	hub.RegisterPlayer(playerID, client)

	ctx := r.Context()
	go client.WritePump(ctx)
	client.ReadPump(ctx)
}
