package ws

import (
	"context"
	"sync"
)

type hubEntry struct {
	hub    *RoomHub
	cancel context.CancelFunc
}

type HubManager struct {
	mu   sync.RWMutex
	hubs map[string]*hubEntry
}

func NewHubManager() *HubManager {
	return &HubManager{
		hubs: make(map[string]*hubEntry),
	}
}

// CreateHub はルーム用のハブを作成し Run() を goroutine で起動する。
func (m *HubManager) CreateHub(ctx context.Context, roomID string) *RoomHub {
	hub := NewRoomHub(roomID)
	hubCtx, cancel := context.WithCancel(ctx)

	m.mu.Lock()
	m.hubs[roomID] = &hubEntry{hub: hub, cancel: cancel}
	m.mu.Unlock()

	go hub.Run(hubCtx)
	return hub
}

// GetHub は roomID に対応するハブを返す。存在しない場合は false を返す。
func (m *HubManager) GetHub(roomID string) (*RoomHub, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.hubs[roomID]
	if !ok {
		return nil, false
	}
	return entry.hub, true
}

// DeleteHub はハブを停止してマップから削除する。
func (m *HubManager) DeleteHub(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.hubs[roomID]
	if !ok {
		return
	}
	entry.cancel()
	delete(m.hubs, roomID)
}
