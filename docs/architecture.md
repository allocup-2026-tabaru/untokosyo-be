# バックエンドアーキテクチャ設計

## ディレクトリ構成（実装後の目標状態）

```
untokosyo-be/
├── cmd/
│   └── main.go                   # chi router, DI組み立て
├── internal/
│   ├── domain/                   # ゲームドメイン層（HTTP/WSに依存しない）
│   │   ├── model.go              # Player, Room, TurnipState, RoundResult 構造体
│   │   ├── judge.go              # ExtractionJudge interface + SigmoidJudge等
│   │   └── game.go              # GameLoop（tick処理・状態遷移）
│   ├── store/
│   │   └── room_store.go         # RoomStore interface + MemoryRoomStore実装
│   ├── ws/
│   │   ├── room_hub.go           # RoomHub（ルーム単位のWSハブ）
│   │   ├── hub_manager.go        # 全ルームのHub管理
│   │   ├── host_client.go        # HostClient（ホスト画面接続）
│   │   ├── player_client.go      # PlayerClient（コントローラー接続）
│   │   ├── event.go              # WSメッセージ型定義
│   │   └── handler.go            # ServeHostWS / ServePlayerWS
│   └── api/
│       └── room_handler.go       # HTTPハンドラー
└── docs/
    ├── game-design.md            # ゲームルール・WSイベント仕様
    └── architecture.md           # 本ファイル
```

### 依存方向

```
api → domain ← ws
api → store
ws  → domain
ws  → store
main → (全て)
```

`domain` は他パッケージを一切 import しない（純粋なGoロジック）。

---

## データモデル

```go
// internal/domain/model.go

type PlayerStatus string
const (
    PlayerStatusActive     PlayerStatus = "active"
    PlayerStatusEliminated PlayerStatus = "eliminated"
)

type Player struct {
    ID               string
    Name             string
    Status           PlayerStatus
    IsPulling        bool
    PullAccumulation float64
    LatencyMs        int64   // 推定片道ラグ(ms)、ping/pongで計測
    ClockOffsetMs    int64   // クライアント時計とサーバー時計のズレ(ms)
    JoinedAt         time.Time
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
    Contributions       map[string]float64 // playerID -> pull蓄積量
}

type RoomStatus string
const (
    RoomStatusWaiting  RoomStatus = "waiting"
    RoomStatusPlaying  RoomStatus = "playing"
    RoomStatusFinished RoomStatus = "finished"
)

type Room struct {
    mu           sync.RWMutex
    ID           string
    HostPlayerID string
    Status       RoomStatus
    Players      map[string]*Player
    Turnip       TurnipState
    Rounds       []RoundResult  // 将来のラウンド制対応。現在は最大1要素
    Winner       *Player        // nil = 引き分け
    CreatedAt    time.Time
    StartedAt    *time.Time
    FinishedAt   *time.Time
}

// ヘルパーメソッド
func (r *Room) ActivePlayers() []*Player    // statusがactive
func (r *Room) PullingPlayers() []*Player   // active && isPulling
```

---

## ExtractionJudge

```go
// internal/domain/judge.go

type JudgeInput struct {
    TotalPullAccumulation float64
    ActivePlayerCount     int
    PullingPlayerCount    int
    ElapsedSeconds        float64
}

type ExtractionJudge interface {
    Judge(input JudgeInput) (probability float64, extracted bool)
}

// SigmoidJudge: P(x) = 1 / (1 + exp(-steepness * (x - midpoint)))
type SigmoidJudge struct {
    Midpoint  float64 // default: 100.0
    Steepness float64 // default: 0.05
}

// ファクトリパターン（main.goで1回だけ呼び出してGameLoopにDI）
type JudgeType string
const (
    JudgeTypeSigmoid     JudgeType = "sigmoid"
    JudgeTypeExponential JudgeType = "exponential"
    JudgeTypeStep        JudgeType = "step"
)

type JudgeConfig struct {
    Type   JudgeType
    Params map[string]float64
}

func NewExtractionJudge(cfg JudgeConfig) ExtractionJudge
```

---

## GameLoop

```go
// internal/domain/game.go

const (
    TickRate         = 100 * time.Millisecond
    PullDeltaPerTick = 1.0
    PingInterval     = 5 * time.Second
)

type TickResult struct {
    Turnip    TurnipState
    Players   map[string]PlayerSnapshot // 全プレイヤーのスナップショット
    Extracted bool
    Finished  bool
    Winner    *Player
}

type GameLoop struct {
    room   *Room
    judge  ExtractionJudge
    tickC  chan<- TickResult // RoomHubへ通知
}

func (gl *GameLoop) Run(ctx context.Context)
func (gl *GameLoop) HandlePull(playerID string, clientTimestamp int64)    // ラグ補正込みで処理
func (gl *GameLoop) HandleRelease(playerID string, clientTimestamp int64)
func (gl *GameLoop) HandlePong(playerID string, serverTimestamp, clientTimestamp int64) // ラグ計測
```

### ラグ補正の計算

```
// pong受信時
rtt = time.Now().UnixMilli() - serverTimestamp
player.LatencyMs = rtt / 2
player.ClockOffsetMs = serverTimestamp + rtt/2 - clientTimestamp

// pull/release受信時の補正
serverEquivTime = clientTimestamp + player.ClockOffsetMs
lag = time.Now().UnixMilli() - serverEquivTime
// lag分だけ遡って蓄積量を加算（ただし最大補正値を設ける: MaxLagCompensation = 200ms）
```

---

## WSハブ設計

### RoomHub

```go
// internal/ws/room_hub.go

type RoomHub struct {
    roomID     string
    host       *HostClient      // 1接続のみ
    mu         sync.RWMutex
    players    map[string]*PlayerClient // playerID -> PlayerClient
    register   chan clientRegistration
    unregister chan clientUnregistration
    tickC      chan domain.TickResult   // GameLoopからのtick通知
    gameEvt    chan gameEvent           // pull/release/pong をGameLoopへ
}

// GameLoopのTickResultを受け取り、適切なクライアントに配信する
// HostClientには全情報を送信
// PlayerClientには自分の状態 + ゲーム状態のみ送信
func (h *RoomHub) Run(ctx context.Context)
func (h *RoomHub) BroadcastToHost(msg []byte)
func (h *RoomHub) SendToPlayer(playerID string, msg []byte)
```

### HubManager

```go
// internal/ws/hub_manager.go

type HubManager struct {
    mu   sync.RWMutex
    hubs map[string]*RoomHub // roomID -> RoomHub
}

func (m *HubManager) CreateHub(ctx context.Context, roomID string) *RoomHub
func (m *HubManager) GetHub(roomID string) (*RoomHub, bool)
func (m *HubManager) DeleteHub(roomID string)
```

### WSハンドラー

```go
// internal/ws/handler.go

// GET /ws/rooms/{roomID}/host
func ServeHostWS(manager *HubManager, w http.ResponseWriter, r *http.Request)

// GET /ws/rooms/{roomID}/player?playerID=xxx
func ServePlayerWS(manager *HubManager, w http.ResponseWriter, r *http.Request)
```

---

## インメモリストア

```go
// internal/store/room_store.go

type RoomStore interface {
    Create(room *domain.Room) error
    Get(roomID string) (*domain.Room, bool)
    Delete(roomID string)
    List() []*domain.Room
}

// MemoryRoomStore は sync.RWMutex で保護されたmap実装
// サーバー再起動でリセット。将来Redisに差し替え時はこのinterfaceを実装するだけ。
type MemoryRoomStore struct { ... }
```

---

## main.go のDI組み立て

```go
// cmd/main.go

func main() {
    roomStore  := store.NewMemoryRoomStore()
    hubManager := ws.NewHubManager()
    judge      := domain.NewExtractionJudge(domain.JudgeConfig{
        Type:   domain.JudgeTypeSigmoid,
        Params: map[string]float64{"midpoint": 100.0, "steepness": 0.05},
    })
    handler := api.NewRoomHandler(roomStore, hubManager, judge)

    r := chi.NewRouter()
    // ... cors, logger, recoverer ...
    r.Get("/healthz", ...)
    r.Post("/rooms", handler.Create)
    r.Get("/rooms/{roomID}", handler.Get)
    r.Post("/rooms/{roomID}/players", handler.Join)
    r.Post("/rooms/{roomID}/start", handler.Start)
    r.Delete("/rooms/{roomID}", handler.Delete)
    r.Get("/ws/rooms/{roomID}/host", func(w http.ResponseWriter, r *http.Request) {
        ws.ServeHostWS(hubManager, w, r)
    })
    r.Get("/ws/rooms/{roomID}/player", func(w http.ResponseWriter, r *http.Request) {
        ws.ServePlayerWS(hubManager, w, r)
    })

    http.ListenAndServe(":"+port, r)
}
```

---

## 実装順序

1. `internal/domain/model.go` - 構造体定義（依存なし）
2. `internal/domain/judge.go` - Interface + SigmoidJudge + ファクトリ
3. `internal/store/room_store.go` - RoomStore interface + MemoryRoomStore
4. `internal/domain/game.go` - GameLoop（tick・ラグ補正・状態遷移）
5. `internal/ws/event.go` - WSメッセージ型定義
6. `internal/ws/room_hub.go` + `hub_manager.go` - RoomHub + HubManager
7. `internal/ws/host_client.go` + `player_client.go` - クライアント実装
8. `internal/ws/handler.go` - ServeHostWS / ServePlayerWS
9. `internal/api/room_handler.go` - HTTPハンドラー
10. `cmd/main.go` - DI・ルーティング更新（既存のhub.go/handler.goは削除）

---

## 検証手順

1. `make dev` でサーバー起動
2. `POST /rooms` → `POST /rooms/{id}/players` × 複数 → `POST /rooms/{id}/start`
3. wscat で `/ws/rooms/{id}/host` に接続 → `room_state` 受信確認
4. wscat で `/ws/rooms/{id}/player?playerID=xxx` × 複数接続
5. pull メッセージ送信 → ホスト画面に `player_update` が来ることを確認
6. `turnip_update` の `extractionProbability` が増加することを確認
7. `extracted` → `game_finished` の順にイベントが届くことを確認
8. ゲーム終了後も `GET /rooms/{id}` でルーム情報が取得できることを確認
9. `DELETE /rooms/{id}` 後に404になることを確認
