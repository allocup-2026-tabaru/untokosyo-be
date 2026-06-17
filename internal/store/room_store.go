package store

import (
	"fmt"
	"sync"

	"github.com/allocup-2026-tabaru/untokosyo-be/internal/domain"
)

type RoomStore interface {
	Create(room *domain.Room) error
	Get(roomID string) (*domain.Room, bool)
	Delete(roomID string)
	List() []*domain.Room
}

type MemoryRoomStore struct {
	mu    sync.RWMutex
	rooms map[string]*domain.Room
}

func NewMemoryRoomStore() *MemoryRoomStore {
	return &MemoryRoomStore{
		rooms: make(map[string]*domain.Room),
	}
}

func (s *MemoryRoomStore) Create(room *domain.Room) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rooms[room.ID]; exists {
		return fmt.Errorf("room %s already exists", room.ID)
	}
	s.rooms[room.ID] = room
	return nil
}

func (s *MemoryRoomStore) Get(roomID string) (*domain.Room, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	room, ok := s.rooms[roomID]
	return room, ok
}

func (s *MemoryRoomStore) Delete(roomID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rooms, roomID)
}

func (s *MemoryRoomStore) List() []*domain.Room {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*domain.Room, 0, len(s.rooms))
	for _, room := range s.rooms {
		result = append(result, room)
	}
	return result
}
