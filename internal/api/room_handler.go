package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/allocup-2026-tabaru/untokosyo-be/internal/auth"
	"github.com/allocup-2026-tabaru/untokosyo-be/internal/domain"
	"github.com/allocup-2026-tabaru/untokosyo-be/internal/store"
	"github.com/allocup-2026-tabaru/untokosyo-be/internal/ws"
	"github.com/go-chi/chi/v5"
)

type RoomHandler struct {
	store     store.RoomStore
	manager   *ws.HubManager
	judge     domain.ExtractionJudge
	ctx       context.Context
	jwtSecret string
}

func NewRoomHandler(ctx context.Context, s store.RoomStore, m *ws.HubManager, j domain.ExtractionJudge, jwtSecret string) *RoomHandler {
	return &RoomHandler{store: s, manager: m, judge: j, ctx: ctx, jwtSecret: jwtSecret}
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
				ID:        hostPlayerID,
				Name:      "host",
				Status:    domain.PlayerStatusActive,
				Connected: false,
				JoinedAt:  now,
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

	token, err := auth.GenerateToken(hostPlayerID, roomID, h.jwtSecret)
	if err != nil {
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	slog.Info("room created", "roomID", roomID, "hostPlayerID", hostPlayerID)

	writeJSON(w, http.StatusCreated, map[string]string{
		"roomID":       roomID,
		"hostPlayerID": hostPlayerID,
		"token":        token,
	})
}

func (h *RoomHandler) Get(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	room, ok := h.store.Get(roomID)
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	type roomPlayerResponse struct {
		ID               string            `json:"ID"`
		Name             string            `json:"Name"`
		Status           string            `json:"Status"`
		Connected        bool              `json:"Connected"`
		IsPulling        bool              `json:"IsPulling"`
		PullAccumulation float64           `json:"PullAccumulation"`
		AvatarModel      string            `json:"AvatarModel,omitempty"`
		MaterialColors   map[string]string `json:"MaterialColors,omitempty"`
		JoinedAt         time.Time         `json:"JoinedAt"`
	}
	type roomResponse struct {
		ID               string                        `json:"ID"`
		HostPlayerID     string                        `json:"HostPlayerID"`
		Status           domain.RoomStatus             `json:"Status"`
		Players          map[string]roomPlayerResponse `json:"Players"`
		Winner           *domain.Player                `json:"Winner"`
		CreatedAt        time.Time                     `json:"CreatedAt"`
		ScheduledStartAt *time.Time                    `json:"ScheduledStartAt"`
		StartedAt        *time.Time                    `json:"StartedAt"`
		FinishedAt       *time.Time                    `json:"FinishedAt"`
	}

	players := make(map[string]roomPlayerResponse)
	for id, p := range room.Players {
		if !p.Connected {
			continue
		}
		players[id] = roomPlayerResponse{
			ID:               p.ID,
			Name:             p.Name,
			Status:           string(p.Status),
			Connected:        p.Connected,
			IsPulling:        p.IsPulling,
			PullAccumulation: p.PullAccumulation,
			AvatarModel:      p.AvatarModel,
			MaterialColors:   p.MaterialColors,
			JoinedAt:         p.JoinedAt,
		}
	}

	writeJSON(w, http.StatusOK, roomResponse{
		ID:               room.ID,
		HostPlayerID:     room.HostPlayerID,
		Status:           room.Status,
		Players:          players,
		Winner:           room.Winner,
		CreatedAt:        room.CreatedAt,
		ScheduledStartAt: room.ScheduledStartAt,
		StartedAt:        room.StartedAt,
		FinishedAt:       room.FinishedAt,
	})
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
		Name           string            `json:"name"`
		AvatarModel    string            `json:"avatarModel"`
		MaterialColors map[string]string `json:"materialColors"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	playerID := newUUID()
	room.Players[playerID] = &domain.Player{
		ID:             playerID,
		Name:           req.Name,
		Status:         domain.PlayerStatusActive,
		Connected:      false,
		JoinedAt:       time.Now(),
		AvatarModel:    req.AvatarModel,
		MaterialColors: req.MaterialColors,
	}

	slog.Info("player joined", "roomID", roomID, "playerID", playerID, "name", req.Name)

	token, err := auth.GenerateToken(playerID, roomID, h.jwtSecret)
	if err != nil {
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"playerID": playerID,
		"name":     req.Name,
		"token":    token,
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

	// ラグ補正: 最大レイテンシ分だけカウントダウンに余裕を持たせ、
	// 全プレイヤーが scheduledStartAt より前にメッセージを受け取れるようにする。
	maxLatency := time.Duration(room.MaxLatencyMs()) * time.Millisecond
	delay := domain.CountdownDuration + maxLatency + domain.CountdownBuffer
	scheduledStart := time.Now().Add(delay)

	room.Status = domain.RoomStatusCountdown
	room.ScheduledStartAt = &scheduledStart

	hub.BroadcastGameCountdown(scheduledStart.UnixMilli())

	slog.Info("game countdown started", "roomID", roomID, "scheduledStartAt", scheduledStart, "maxLatencyMs", room.MaxLatencyMs())

	ctx := h.ctx
	go func() {
		timer := time.NewTimer(time.Until(scheduledStart))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		startedAt := scheduledStart
		room.Status = domain.RoomStatusPlaying
		room.StartedAt = &startedAt

		loop := domain.NewGameLoop(room, h.judge, hub.TickC())
		hub.SetHostPlayerID(room.HostPlayerID)
		hub.SetGameLoop(loop)
		go loop.Run(ctx)

		hub.BroadcastGameStart(startedAt.UnixMilli())

		slog.Info("game started", "roomID", roomID, "playerCount", len(room.Players))
	}()

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *RoomHandler) Delete(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	h.store.Delete(roomID)
	h.manager.DeleteHub(roomID)
	slog.Info("room deleted", "roomID", roomID)
	w.WriteHeader(http.StatusNoContent)
}
