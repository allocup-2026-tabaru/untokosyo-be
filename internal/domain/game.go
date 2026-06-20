package domain

import (
	"context"
	"log/slog"
	"time"
)

const (
	TickRate           = 100 * time.Millisecond
	PullDeltaPerTick   = 1.0
	PingInterval       = 5 * time.Second
	MaxLagCompensation = 200 // ms

	CountdownDuration = 5 * time.Second
	CountdownBuffer   = 500 * time.Millisecond
)

type PlayerSnapshot struct {
	ID               string
	Name             string
	Status           PlayerStatus
	IsPulling        bool
	PullAccumulation float64
}

type TickResult struct {
	Turnip    TurnipState
	Players   map[string]PlayerSnapshot
	Extracted bool
	Finished  bool
	Winner    *Player
	IsPing    bool
}

type GameLoop struct {
	room   *Room
	roomID string
	judge  ExtractionJudge
	tickC  chan<- TickResult
}

func NewGameLoop(room *Room, judge ExtractionJudge, tickC chan<- TickResult) *GameLoop {
	return &GameLoop{
		room:   room,
		roomID: room.ID,
		judge:  judge,
		tickC:  tickC,
	}
}

func (gl *GameLoop) Run(ctx context.Context) {
	tickTicker := time.NewTicker(TickRate)
	pingTicker := time.NewTicker(PingInterval)
	defer tickTicker.Stop()
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tickTicker.C:
			result := gl.tick()
			select {
			case gl.tickC <- result:
			default:
			}
		case <-pingTicker.C:
			select {
			case gl.tickC <- TickResult{IsPing: true}:
			default:
			}
		}
	}
}

func (gl *GameLoop) tick() TickResult {
	gl.room.mu.Lock()
	defer gl.room.mu.Unlock()

	if gl.room.Status != RoomStatusPlaying {
		return gl.buildTickResult(false, false)
	}

	// pull中のアクティブプレイヤーの蓄積量を加算
	pullingCount := 0
	for _, p := range gl.room.Players {
		if p.Status == PlayerStatusActive && p.IsPulling {
			p.PullAccumulation += PullDeltaPerTick
			pullingCount++
		}
	}
	gl.room.Turnip.TotalPullAccumulation += PullDeltaPerTick * float64(pullingCount)

	// カブ抜け判定
	elapsed := time.Since(*gl.room.StartedAt).Seconds()
	activeCount := 0
	for _, p := range gl.room.Players {
		if p.Status == PlayerStatusActive {
			activeCount++
		}
	}
	probability, extracted := gl.judge.Judge(JudgeInput{
		TotalPullAccumulation: gl.room.Turnip.TotalPullAccumulation,
		ActivePlayerCount:     activeCount,
		PullingPlayerCount:    pullingCount,
		ElapsedSeconds:        elapsed,
	})
	gl.room.Turnip.ExtractionProbability = probability

	slog.Debug("tick",
		"roomID", gl.roomID,
		"totalPull", gl.room.Turnip.TotalPullAccumulation,
		"probability", probability,
		"pulling", pullingCount,
		"active", activeCount,
	)

	if !extracted {
		return gl.buildTickResult(false, false)
	}

	// カブが抜けた: pull中プレイヤーを eliminated に
	now := time.Now()
	eliminatedCount := 0
	for _, p := range gl.room.Players {
		if p.Status == PlayerStatusActive && p.IsPulling {
			p.Status = PlayerStatusEliminated
			p.IsPulling = false
			eliminatedCount++
		}
	}
	gl.room.Turnip.IsExtracted = true
	gl.room.Turnip.ExtractedAt = &now

	slog.Info("turnip extracted", "roomID", gl.roomID, "eliminatedCount", eliminatedCount)

	// 残存アクティブプレイヤーの中で PullAccumulation 最大を勝者に
	var winner *Player
	for _, p := range gl.room.Players {
		if p.Status == PlayerStatusActive {
			if winner == nil || p.PullAccumulation > winner.PullAccumulation {
				winner = p
			}
		}
	}
	gl.room.Winner = winner
	gl.room.Status = RoomStatusFinished
	gl.room.FinishedAt = &now

	winnerID, winnerName := "", ""
	if winner != nil {
		winnerID = winner.ID
		winnerName = winner.Name
	}
	slog.Info("game finished", "roomID", gl.roomID, "winnerID", winnerID, "winnerName", winnerName)

	return gl.buildTickResult(true, true)
}

func (gl *GameLoop) buildTickResult(extracted, finished bool) TickResult {
	snapshots := make(map[string]PlayerSnapshot, len(gl.room.Players))
	for id, p := range gl.room.Players {
		snapshots[id] = PlayerSnapshot{
			ID:               p.ID,
			Name:             p.Name,
			Status:           p.Status,
			IsPulling:        p.IsPulling,
			PullAccumulation: p.PullAccumulation,
		}
	}
	return TickResult{
		Turnip:    gl.room.Turnip,
		Players:   snapshots,
		Extracted: extracted,
		Finished:  finished,
		Winner:    gl.room.Winner,
	}
}

func (gl *GameLoop) HandlePong(playerID string, serverTimestamp, clientTimestamp int64) {
	gl.room.mu.Lock()
	defer gl.room.mu.Unlock()

	p, ok := gl.room.Players[playerID]
	if !ok {
		return
	}
	rtt := time.Now().UnixMilli() - serverTimestamp
	p.LatencyMs = rtt / 2
	p.ClockOffsetMs = serverTimestamp + rtt/2 - clientTimestamp
	slog.Debug("pong", "roomID", gl.roomID, "playerID", playerID, "latencyMs", p.LatencyMs, "clockOffsetMs", p.ClockOffsetMs)
}

func (gl *GameLoop) HandlePull(playerID string, clientTimestamp int64) {
	gl.room.mu.Lock()
	defer gl.room.mu.Unlock()

	p, ok := gl.room.Players[playerID]
	if !ok || p.Status != PlayerStatusActive {
		return
	}
	p.IsPulling = true

	// ラグ補正: クライアントイベント発生推定時刻との差分を蓄積量に加算
	serverEquivTime := clientTimestamp + p.ClockOffsetMs
	lag := time.Now().UnixMilli() - serverEquivTime
	if lag > MaxLagCompensation {
		lag = MaxLagCompensation
	}
	compensation := 0.0
	if lag > 0 {
		compensation = float64(lag) / float64(TickRate.Milliseconds())
		p.PullAccumulation += compensation
		gl.room.Turnip.TotalPullAccumulation += compensation
	}
	slog.Debug("pull", "roomID", gl.roomID, "playerID", playerID, "compensation", compensation, "accumulation", p.PullAccumulation)
}

func (gl *GameLoop) HandleRelease(playerID string, clientTimestamp int64) {
	gl.room.mu.Lock()
	defer gl.room.mu.Unlock()

	p, ok := gl.room.Players[playerID]
	if !ok || p.Status != PlayerStatusActive {
		return
	}
	p.IsPulling = false
	slog.Debug("release", "roomID", gl.roomID, "playerID", playerID)
}
