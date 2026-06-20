package domain

import (
	"sync"
	"time"
)

type PlayerStatus string

const (
	PlayerStatusActive     PlayerStatus = "active"
	PlayerStatusEliminated PlayerStatus = "eliminated"
)

type RoomStatus string

const (
	RoomStatusWaiting   RoomStatus = "waiting"
	RoomStatusCountdown RoomStatus = "countdown"
	RoomStatusPlaying   RoomStatus = "playing"
	RoomStatusFinished  RoomStatus = "finished"
)

type Player struct {
	ID               string
	Name             string
	Status           PlayerStatus
	IsPulling        bool
	PullAccumulation float64
	LatencyMs        int64
	ClockOffsetMs    int64
	JoinedAt         time.Time
	AvatarModel      string
	MaterialColors   map[string]string
}

type TurnipState struct {
	TotalPullAccumulation float64
	ExtractionProbability float64
	IsExtracted           bool
	ExtractedAt           *time.Time
}

type RoundResult struct {
	RoundNumber         int
	EliminatedPlayerIDs []string
	Contributions       map[string]float64
}

type Room struct {
	mu           sync.RWMutex
	ID           string
	HostPlayerID string
	Status       RoomStatus
	Players      map[string]*Player
	Turnip       TurnipState
	Rounds       []RoundResult
	Winner       *Player
	CreatedAt        time.Time
	ScheduledStartAt *time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
}

// MaxLatencyMs は接続中プレイヤーの LatencyMs の最大値を返す。
func (r *Room) MaxLatencyMs() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var max int64
	for _, p := range r.Players {
		if p.LatencyMs > max {
			max = p.LatencyMs
		}
	}
	return max
}

// ActivePlayers は Status が active なプレイヤー一覧を返す。
func (r *Room) ActivePlayers() []*Player {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Player, 0, len(r.Players))
	for _, p := range r.Players {
		if p.Status == PlayerStatusActive {
			result = append(result, p)
		}
	}
	return result
}

// PullingPlayers は active かつ IsPulling なプレイヤー一覧を返す。
func (r *Room) PullingPlayers() []*Player {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Player, 0)
	for _, p := range r.Players {
		if p.Status == PlayerStatusActive && p.IsPulling {
			result = append(result, p)
		}
	}
	return result
}
