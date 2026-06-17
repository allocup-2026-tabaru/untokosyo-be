package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/allocup-2026-tabaru/untokosyo-be/internal/domain"
	"github.com/allocup-2026-tabaru/untokosyo-be/internal/store"
	"github.com/allocup-2026-tabaru/untokosyo-be/internal/ws"
	"github.com/go-chi/chi/v5"
)

type RoomHandler struct {
	store   store.RoomStore
	manager *ws.HubManager
	judge   domain.ExtractionJudge
	ctx     context.Context
}

func NewRoomHandler(ctx context.Context, s store.RoomStore, m *ws.HubManager, j domain.ExtractionJudge) *RoomHandler {
	return &RoomHandler{store: s, manager: m, judge: j, ctx: ctx}
}

func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("uuid generation failed: %w", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant RFC4122
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *RoomHandler) Create(w http.ResponseWriter, r *http.Request) {
	roomID := newUUID()
	hostPlayerID := newUUID()
	now := time.Now()

	room := &domain.Room{
		ID:           roomID,
		HostPlayerID: hostPlayerID,
		Status:       domain.RoomStatusWaiting,
		Players: map[string]*domain.Player{
			hostPlayerID: {
				ID:       hostPlayerID,
				Name:     "host",
				Status:   domain.PlayerStatusActive,
				JoinedAt: now,
			},
		},
		Turnip:    domain.TurnipState{},
		Rounds:    []domain.RoundResult{},
		CreatedAt: now,
	}

	if err := h.store.Create(room); err != nil {
		http.Error(w, "failed to create room", http.StatusInternalServerError)
		return
	}

	h.manager.CreateHub(h.ctx, roomID)

	writeJSON(w, http.StatusCreated, map[string]string{
		"roomID":       roomID,
		"hostPlayerID": hostPlayerID,
	})
}

func (h *RoomHandler) Get(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	room, ok := h.store.Get(roomID)
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandler) Join(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	room, ok := h.store.Get(roomID)
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	if room.Status != domain.RoomStatusWaiting {
		http.Error(w, "game already started", http.StatusConflict)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	playerID := newUUID()
	room.Players[playerID] = &domain.Player{
		ID:       playerID,
		Name:     req.Name,
		Status:   domain.PlayerStatusActive,
		JoinedAt: time.Now(),
	}

	if hub, ok := h.manager.GetHub(roomID); ok {
		hub.NotifyPlayerJoined(playerID, req.Name)
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"playerID": playerID,
		"name":     req.Name,
	})
}

func (h *RoomHandler) Start(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	room, ok := h.store.Get(roomID)
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	var req struct {
		PlayerID string `json:"playerID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.PlayerID != room.HostPlayerID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if room.Status != domain.RoomStatusWaiting {
		http.Error(w, "game already started", http.StatusConflict)
		return
	}

	hub, ok := h.manager.GetHub(roomID)
	if !ok {
		http.Error(w, "hub not found", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	room.Status = domain.RoomStatusPlaying
	room.StartedAt = &now

	loop := domain.NewGameLoop(room, h.judge, hub.TickC())
	hub.SetGameLoop(loop)
	go loop.Run(h.ctx)

	hub.BroadcastGameStart(now.UnixMilli())

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *RoomHandler) Delete(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	h.store.Delete(roomID)
	h.manager.DeleteHub(roomID)
	w.WriteHeader(http.StatusNoContent)
}
