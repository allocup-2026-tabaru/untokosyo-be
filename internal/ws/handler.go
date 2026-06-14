package ws

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
)

type Client struct {
	send chan []byte
}

func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	client := &Client{send: make(chan []byte, 64)}
	hub.register <- client

	go writePump(conn, client)
	readPump(r.Context(), conn, hub, client)
}

func readPump(ctx context.Context, conn *websocket.Conn, hub *Hub, client *Client) {
	defer func() {
		hub.unregister <- client
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			return
		}
		hub.Increment()
	}
}

func writePump(conn *websocket.Conn, client *Client) {
	for msg := range client.send {
		ctx := context.Background()
		if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
			return
		}
	}
}
