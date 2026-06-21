package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/allocup-2026-tabaru/untokosyo-be/internal/domain"
)

// clientSender は HostClient / PlayerClient が実装するインターフェース。
type clientSender interface {
	Send(msg []byte)
}

type gameEvent struct {
	Type            EventType
	PlayerID        string
	ClientTimestamp int64
	ServerTimestamp int64
}

type clientRegistration struct {
	host     clientSender
	player   clientSender
	playerID string
}

type clientUnregistration struct {
	host     clientSender
	player   clientSender
	playerID string // "" = ホスト切断
}

type RoomHub struct {
	roomID     string
	host       clientSender
	players    map[string]clientSender
	mu         sync.RWMutex
	register   chan clientRegistration
	unregister chan clientUnregistration
	tickC      chan domain.TickResult
	gameEvt    chan gameEvent
	loop       *domain.GameLoop
}

func NewRoomHub(roomID string) *RoomHub {
	return &RoomHub{
		roomID:     roomID,
		players:    make(map[string]clientSender),
		register:   make(chan clientRegistration, 8),
		unregister: make(chan clientUnregistration, 8),
		tickC:      make(chan domain.TickResult, 1),
		gameEvt:    make(chan gameEvent, 32),
	}
}

// TickC は GameLoop が tick 結果を書き込む write-only チャネルを返す。
func (h *RoomHub) TickC() chan<- domain.TickResult {
	return h.tickC
}

// GameEvtC は PlayerClient がゲームイベントを書き込む write-only チャネルを返す。
func (h *RoomHub) GameEvtC() chan<- gameEvent {
	return h.gameEvt
}

// SetGameLoop はゲーム開始時に api 層から呼ばれる。
func (h *RoomHub) SetGameLoop(loop *domain.GameLoop) {
	h.loop = loop
}

// RegisterHost はホスト接続を登録する。
func (h *RoomHub) RegisterHost(client clientSender) {
	h.register <- clientRegistration{host: client}
}

// RegisterPlayer はプレイヤー接続を登録する。
func (h *RoomHub) RegisterPlayer(playerID string, client clientSender) {
	h.register <- clientRegistration{player: client, playerID: playerID}
}

// UnregisterHost はホスト切断を通知する。
func (h *RoomHub) UnregisterHost() {
	h.unregister <- clientUnregistration{playerID: ""}
}

// UnregisterHostClient は指定ホスト接続の切断を通知する。
func (h *RoomHub) UnregisterHostClient(client clientSender) {
	h.unregister <- clientUnregistration{playerID: "", host: client}
}

// UnregisterPlayer はプレイヤー切断を通知する。
func (h *RoomHub) UnregisterPlayer(playerID string) {
	h.unregister <- clientUnregistration{playerID: playerID}
}

// UnregisterPlayerClient は指定プレイヤー接続の切断を通知する。
func (h *RoomHub) UnregisterPlayerClient(playerID string, client clientSender) {
	h.unregister <- clientUnregistration{playerID: playerID, player: client}
}

// NotifyPlayerJoined は waiting 中のプレイヤー参加をホストへ通知する。
func (h *RoomHub) NotifyPlayerJoined(playerID, name string) {
	msg := h.marshal(OutgoingMessage{
		Type:    EventTypePlayerJoined,
		Payload: PlayerJoinedPayload{PlayerID: playerID, Name: name},
	})
	h.BroadcastToHost(msg)
}

// NotifyPlayerLeft はプレイヤー切断をホストへ通知する。
func (h *RoomHub) NotifyPlayerLeft(playerID string) {
	msg := h.marshal(OutgoingMessage{
		Type:    EventTypePlayerLeft,
		Payload: PlayerLeftPayload{PlayerID: playerID},
	})
	h.BroadcastToHost(msg)
}

// BroadcastGameCountdown はゲーム開始予告をホスト・全プレイヤーへ送信する。
// scheduledStartAt はサーバー絶対時刻（Unix ms）。
func (h *RoomHub) BroadcastGameCountdown(scheduledStartAt int64) {
	msg := h.marshal(OutgoingMessage{
		Type:    EventTypeGameCountdown,
		Payload: GameCountdownPayload{ScheduledStartAt: scheduledStartAt},
	})
	h.broadcastAll(msg)
}

// BroadcastGameStart はゲーム開始をホスト・全プレイヤーへ送信する。
func (h *RoomHub) BroadcastGameStart(startedAt int64) {
	msg := h.marshal(OutgoingMessage{
		Type:    EventTypeGameStart,
		Payload: GameStartPayload{StartedAt: startedAt},
	})
	h.broadcastAll(msg)
}

// Run はメインイベントループ。goroutine で起動すること。
func (h *RoomHub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case reg := <-h.register:
			h.mu.Lock()
			if reg.host != nil {
				h.host = reg.host
				slog.Debug("host registered", "roomID", h.roomID)
			} else if reg.player != nil {
				h.players[reg.playerID] = reg.player
				slog.Debug("player registered", "roomID", h.roomID, "playerID", reg.playerID)
			}
			h.mu.Unlock()

			case unreg := <-h.unregister:
			leftPlayerID := ""
			h.mu.Lock()
			if unreg.playerID == "" {
				if unreg.host == nil || h.host == unreg.host {
					h.host = nil
					slog.Debug("host unregistered", "roomID", h.roomID)
				}
			} else {
				if current, ok := h.players[unreg.playerID]; ok && (unreg.player == nil || current == unreg.player) {
					delete(h.players, unreg.playerID)
					leftPlayerID = unreg.playerID
					slog.Debug("player unregistered", "roomID", h.roomID, "playerID", unreg.playerID)
				}
			}
			h.mu.Unlock()
			if leftPlayerID != "" {
				h.NotifyPlayerLeft(leftPlayerID)
			}

		case result := <-h.tickC:
			h.handleTick(result)

		case evt := <-h.gameEvt:
			h.handleGameEvent(evt)
		}
	}
}

func (h *RoomHub) handleTick(result domain.TickResult) {
	if result.IsPing {
		ping := h.marshal(PingMessage{
			Type:            EventTypePing,
			ServerTimestamp: time.Now().UnixMilli(),
		})
		h.broadcastAll(ping)
		return
	}

	// ホストへ turnip_update 送信
	turnipMsg := h.marshal(OutgoingMessage{
		Type: EventTypeTurnipUpdate,
		Payload: TurnipUpdatePayload{
			TotalPullAccumulation: result.Turnip.TotalPullAccumulation,
			ExtractionProbability: result.Turnip.ExtractionProbability,
		},
	})
	h.BroadcastToHost(turnipMsg)

	// ホストへ各プレイヤーの player_update 送信
	for _, snap := range result.Players {
		msg := h.marshal(OutgoingMessage{
			Type: EventTypePlayerUpdate,
			Payload: PlayerUpdatePayload{
				PlayerID:  snap.ID,
				IsPulling: snap.IsPulling,
				Status:    string(snap.Status),
			},
		})
		h.BroadcastToHost(msg)
	}

	// 各プレイヤーへ自分の player_update 送信
	for playerID, snap := range result.Players {
		msg := h.marshal(OutgoingMessage{
			Type: EventTypePlayerUpdate,
			Payload: PlayerUpdatePayload{
				PlayerID:  snap.ID,
				IsPulling: snap.IsPulling,
				Status:    string(snap.Status),
			},
		})
		h.SendToPlayer(playerID, msg)
	}

	if result.Extracted {
		eliminatedIDs := make([]string, 0)
		for _, snap := range result.Players {
			if snap.Status == domain.PlayerStatusEliminated {
				eliminatedIDs = append(eliminatedIDs, snap.ID)
			}
		}

		extractedMsg := h.marshal(OutgoingMessage{
			Type:    EventTypeExtracted,
			Payload: ExtractedPayload{EliminatedPlayerIDs: eliminatedIDs},
		})
		h.BroadcastToHost(extractedMsg)

		eliminatedMsg := h.marshal(OutgoingMessage{
			Type:    EventTypeEliminated,
			Payload: EliminatedPayload{},
		})
		for _, id := range eliminatedIDs {
			h.SendToPlayer(id, eliminatedMsg)
		}
	}

	if result.Finished {
		standings := buildStandings(result.Players)

		winnerID := ""
		winnerName := ""
		if result.Winner != nil {
			winnerID = result.Winner.ID
			winnerName = result.Winner.Name
		}

		hostFinishedMsg := h.marshal(OutgoingMessage{
			Type: EventTypeGameFinished,
			Payload: HostGameFinishedPayload{
				WinnerPlayerID: winnerID,
				WinnerName:     winnerName,
				Standings:      standings,
			},
		})
		h.BroadcastToHost(hostFinishedMsg)

		rankMap := make(map[string]int, len(standings))
		for _, s := range standings {
			rankMap[s.PlayerID] = s.Rank
		}
		for _, snap := range result.Players {
			msg := h.marshal(OutgoingMessage{
				Type: EventTypeGameFinished,
				Payload: ControllerGameFinishedPayload{
					WinnerPlayerID:     winnerID,
					MyRank:             rankMap[snap.ID],
					MyPullAccumulation: snap.PullAccumulation,
				},
			})
			h.SendToPlayer(snap.ID, msg)
		}
	}
}

func (h *RoomHub) handleGameEvent(evt gameEvent) {
	if h.loop == nil {
		return
	}
	switch evt.Type {
	case EventTypePull:
		h.loop.HandlePull(evt.PlayerID, evt.ClientTimestamp)
	case EventTypeRelease:
		h.loop.HandleRelease(evt.PlayerID, evt.ClientTimestamp)
	case EventTypePong:
		h.loop.HandlePong(evt.PlayerID, evt.ServerTimestamp, evt.ClientTimestamp)
	}
}

// BroadcastToHost はホストクライアントへメッセージを送信する。
func (h *RoomHub) BroadcastToHost(msg []byte) {
	h.mu.RLock()
	host := h.host
	h.mu.RUnlock()
	if host != nil {
		host.Send(msg)
	}
}

// SendToPlayer は指定プレイヤーへメッセージを送信する。
func (h *RoomHub) SendToPlayer(playerID string, msg []byte) {
	h.mu.RLock()
	client := h.players[playerID]
	h.mu.RUnlock()
	if client != nil {
		client.Send(msg)
	}
}

func (h *RoomHub) broadcastAll(msg []byte) {
	h.mu.RLock()
	host := h.host
	players := make([]clientSender, 0, len(h.players))
	for _, c := range h.players {
		players = append(players, c)
	}
	h.mu.RUnlock()

	if host != nil {
		host.Send(msg)
	}
	for _, c := range players {
		c.Send(msg)
	}
}

func (h *RoomHub) marshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func buildStandings(players map[string]domain.PlayerSnapshot) []StandingEntry {
	snaps := make([]domain.PlayerSnapshot, 0, len(players))
	for _, s := range players {
		snaps = append(snaps, s)
	}
	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].PullAccumulation > snaps[j].PullAccumulation
	})

	standings := make([]StandingEntry, len(snaps))
	for i, s := range snaps {
		standings[i] = StandingEntry{
			PlayerID:         s.ID,
			Name:             s.Name,
			PullAccumulation: s.PullAccumulation,
			Rank:             i + 1,
		}
	}
	return standings
}
