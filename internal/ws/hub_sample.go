// サンプルコード: グローバルカウンターをブロードキャストする最小実装例。
// ゲーム本体には使用しない。
package ws

import (
	"encoding/json"
	"sync"
	"sync/atomic"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
	count   atomic.Int64

	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()

		case c := <-h.unregister:
			h.mu.Lock()
			delete(h.clients, c)
			h.mu.Unlock()
			close(c.send)

		case msg := <-h.broadcast:
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Increment() {
	n := h.count.Add(1)
	msg, _ := json.Marshal(map[string]int64{"count": n})
	h.broadcast <- msg
}
