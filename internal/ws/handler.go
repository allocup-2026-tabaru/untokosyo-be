package ws

import (
	"encoding/json"
	"net/http"

	"github.com/allocup-2026-tabaru/untokosyo-be/internal/store"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	store   store.RoomStore
	manager *HubManager
}

func NewHandler(s store.RoomStore, manager *HubManager) *Handler {
	return &Handler{store: s, manager: manager}
}

func (h *Handler) ServeHostWS(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	room, ok := h.store.Get(roomID)
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	hub := h.getOrCreateHub(r, roomID)

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}

	client := NewHostClient(conn, hub)

	players := make([]PlayerSnapshot, 0, len(room.Players))
	for _, p := range room.Players {
		players = append(players, PlayerSnapshot{
			PlayerID:         p.ID,
			Name:             p.Name,
			Status:           string(p.Status),
			IsPulling:        p.IsPulling,
			PullAccumulation: p.PullAccumulation,
		})
	}
	snapshot, _ := json.Marshal(OutgoingMessage{
		Type: EventTypeRoomState,
		Payload: HostRoomStatePayload{
			Status:  string(room.Status),
			Players: players,
			Turnip: TurnipSnapshot{
				TotalPullAccumulation: room.Turnip.TotalPullAccumulation,
				ExtractionProbability: room.Turnip.ExtractionProbability,
			},
		},
	})
	client.Send(snapshot)

	hub.RegisterHost(client)

	ctx := r.Context()
	go client.WritePump(ctx)
	client.ReadPump(ctx)
}

func (h *Handler) ServePlayerWS(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	playerID := r.URL.Query().Get("playerID")
	if playerID == "" {
		http.Error(w, "playerID is required", http.StatusBadRequest)
		return
	}

	room, ok := h.store.Get(roomID)
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	player, ok := room.Players[playerID]
	if !ok {
		http.Error(w, "player not found", http.StatusBadRequest)
		return
	}

	hub := h.getOrCreateHub(r, roomID)

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}

	client := NewPlayerClient(conn, hub, playerID)

	snapshot, _ := json.Marshal(OutgoingMessage{
		Type: EventTypeRoomState,
		Payload: ControllerRoomStatePayload{
			Status:             string(room.Status),
			MyPlayerID:         playerID,
			MyPullAccumulation: player.PullAccumulation,
		},
	})
	client.Send(snapshot)

	hub.RegisterPlayer(playerID, client)

	ctx := r.Context()
	go client.WritePump(ctx)
	client.ReadPump(ctx)
}

func (h *Handler) getOrCreateHub(r *http.Request, roomID string) *RoomHub {
	hub, ok := h.manager.GetHub(roomID)
	if !ok {
		hub = h.manager.CreateHub(r.Context(), roomID)
	}
	return hub
}
