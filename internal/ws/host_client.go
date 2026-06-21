package ws

import (
	"context"
	"encoding/json"
	"time"

	"github.com/allocup-2026-tabaru/untokosyo-be/internal/auth"
	"github.com/allocup-2026-tabaru/untokosyo-be/internal/domain"
	"github.com/coder/websocket"
)

type HostClient struct {
	conn         *websocket.Conn
	hub          *RoomHub
	room         *domain.Room
	roomID       string
	hostPlayerID string
	connID       int64
	jwtSecret    string
	send         chan []byte
}

func NewHostClient(conn *websocket.Conn, hub *RoomHub, room *domain.Room, roomID, hostPlayerID, jwtSecret string) *HostClient {
	return &HostClient{
		conn:         conn,
		hub:          hub,
		room:         room,
		roomID:       roomID,
		hostPlayerID: hostPlayerID,
		jwtSecret:    jwtSecret,
		send:         make(chan []byte, 64),
	}
}

// Send は clientSender インターフェースの実装。チャネルが満杯の場合はドロップする。
func (c *HostClient) Send(msg []byte) {
	select {
	case c.send <- msg:
	default:
	}
}

// ReadPump は認証済みWSの受信ループ。pong を hub へ転送する。
func (c *HostClient) ReadPump(ctx context.Context) {
	defer func() {
		c.room.DisconnectPlayer(c.hostPlayerID, c.connID)
		c.hub.UnregisterHostClient(c)
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

func (c *HostClient) SetConnectionID(connID int64) {
	c.connID = connID
}

// Authenticate はWS接続後の最初のメッセージでJWTを検証する。5秒以内に認証しなければ false を返す。
func (c *HostClient) Authenticate(ctx context.Context) bool {
	authCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, data, err := c.conn.Read(authCtx)
	if err != nil {
		return false
	}

	var msg AuthMessage
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != EventTypeAuth {
		return false
	}

	claims, err := auth.VerifyToken(msg.Token, c.jwtSecret)
	if err != nil {
		return false
	}

	return claims.PlayerID == c.hostPlayerID && claims.RoomID == c.roomID
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
