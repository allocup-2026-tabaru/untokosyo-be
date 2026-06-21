package ws

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/allocup-2026-tabaru/untokosyo-be/internal/store"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	store     store.RoomStore
	manager   *HubManager
	jwtSecret string
}

func NewHandler(s store.RoomStore, manager *HubManager, jwtSecret string) *Handler {
	return &Handler{store: s, manager: manager, jwtSecret: jwtSecret}
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

	client := NewHostClient(conn, hub, room, roomID, room.HostPlayerID, h.jwtSecret)
	ctx := r.Context()
	if !client.Authenticate(ctx) {
		conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}

	connID, ok := room.ConnectPlayer(room.HostPlayerID)
	if !ok {
		conn.Close(websocket.StatusInternalError, "host not found")
		return
	}
	client.SetConnectionID(connID)

	players := make([]PlayerSnapshot, 0, len(room.Players))
	for _, p := range room.Players {
		if !p.Connected {
			continue
		}
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
	slog.Debug("host ws connected", "roomID", roomID)

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

	client := NewPlayerClient(conn, hub, room, playerID, roomID, h.jwtSecret)
	ctx := r.Context()
	if !client.Authenticate(ctx) {
		conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}

	connID, ok := room.ConnectPlayer(playerID)
	if !ok {
		conn.Close(websocket.StatusInternalError, "player not found")
		return
	}
	client.SetConnectionID(connID)

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
	hub.NotifyPlayerJoined(playerID, player.Name)
	slog.Debug("player ws connected", "roomID", roomID, "playerID", playerID)

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
