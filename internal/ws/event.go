package ws

// EventType はWSメッセージの type フィールドで使う定数。
type EventType string

const (
	// クライアント → サーバー
	EventTypeAuth    EventType = "auth"
	EventTypePull    EventType = "pull"
	EventTypeRelease EventType = "release"
	EventTypePong    EventType = "pong"

	// サーバー → クライアント（ホスト・コントローラー共通）
	EventTypeRoomState    EventType = "room_state"
	EventTypeGameStart    EventType = "game_start"
	EventTypePlayerUpdate EventType = "player_update"
	EventTypeGameFinished EventType = "game_finished"
	EventTypePing         EventType = "ping"

	// サーバー → ホスト専用
	EventTypePlayerJoined EventType = "player_joined"
	EventTypeTurnipUpdate EventType = "turnip_update"
	EventTypeExtracted    EventType = "extracted"

	// サーバー → コントローラー専用
	EventTypeEliminated EventType = "eliminated"
	EventTypeError      EventType = "error"
)

// ─── クライアント → サーバー ────────────────────────────────────────────────

// AuthMessage はWS接続後に最初に送るべき認証メッセージ。
type AuthMessage struct {
	Type  EventType `json:"type"`
	Token string    `json:"token"`
}

// IncomingMessage はコントローラー画面からサーバーへ届くメッセージの共通型。
// Type で pull / release / pong を判別する。
type IncomingMessage struct {
	Type            EventType `json:"type"`
	PlayerID        string    `json:"playerID,omitempty"`
	ClientTimestamp int64     `json:"clientTimestamp,omitempty"`
	ServerTimestamp int64     `json:"serverTimestamp,omitempty"`
}

// ─── サーバー → クライアント（基底型）────────────────────────────────────────

// OutgoingMessage はサーバーからクライアントへ送るメッセージの基底型。
// Payload には各イベント固有の構造体を入れる。
type OutgoingMessage struct {
	Type    EventType `json:"type"`
	Payload any       `json:"payload,omitempty"`
}

// PingMessage は ping イベント専用。
// WSイベント仕様では payload なしでトップレベルに serverTimestamp を持つ。
type PingMessage struct {
	Type            EventType `json:"type"`
	ServerTimestamp int64     `json:"serverTimestamp"`
}

// ─── ペイロード共通スナップショット型 ────────────────────────────────────────

// PlayerSnapshot はホスト画面の room_state / player_update などで使うプレイヤー情報。
type PlayerSnapshot struct {
	PlayerID         string            `json:"playerID"`
	Name             string            `json:"name"`
	Status           string            `json:"status"`
	IsPulling        bool              `json:"isPulling"`
	PullAccumulation float64           `json:"pullAccumulation"`
	AvatarModel      string            `json:"avatarModel"`
	MaterialColors   map[string]string `json:"materialColors"`
}

// TurnipSnapshot はホスト画面の room_state で使うカブ状態情報。
type TurnipSnapshot struct {
	TotalPullAccumulation float64 `json:"totalPullAccumulation"`
	ExtractionProbability float64 `json:"extractionProbability"`
}

// ─── ペイロード型: サーバー → ホスト画面 ─────────────────────────────────────

// HostRoomStatePayload は room_state（ホスト）のペイロード。
type HostRoomStatePayload struct {
	Status  string           `json:"status"`
	Players []PlayerSnapshot `json:"players"`
	Turnip  TurnipSnapshot   `json:"turnip"`
}

// PlayerJoinedPayload は player_joined のペイロード。
type PlayerJoinedPayload struct {
	PlayerID       string            `json:"playerID"`
	Name           string            `json:"name"`
	AvatarModel    string            `json:"avatarModel"`
	MaterialColors map[string]string `json:"materialColors"`
}

// GameStartPayload は game_start のペイロード（ホスト・コントローラー共通）。
type GameStartPayload struct {
	StartedAt int64 `json:"startedAt"`
}

// TurnipUpdatePayload は turnip_update のペイロード。
type TurnipUpdatePayload struct {
	TotalPullAccumulation float64 `json:"totalPullAccumulation"`
	ExtractionProbability float64 `json:"extractionProbability"`
}

// PlayerUpdatePayload は player_update のペイロード（ホスト・コントローラー共通）。
// Status はコントローラー向けエコーバック時のみ使用する。
type PlayerUpdatePayload struct {
	PlayerID  string `json:"playerID"`
	IsPulling bool   `json:"isPulling"`
	Status    string `json:"status,omitempty"`
}

// ExtractedPayload は extracted のペイロード。
type ExtractedPayload struct {
	EliminatedPlayerIDs []string `json:"eliminatedPlayerIDs"`
}

// StandingEntry は game_finished の standings 1件分。
type StandingEntry struct {
	PlayerID         string  `json:"playerID"`
	Name             string  `json:"name"`
	PullAccumulation float64 `json:"pullAccumulation"`
	Rank             int     `json:"rank"`
}

// HostGameFinishedPayload は game_finished（ホスト）のペイロード。
type HostGameFinishedPayload struct {
	WinnerPlayerID string         `json:"winnerPlayerID"`
	WinnerName     string         `json:"winnerName"`
	Standings      []StandingEntry `json:"standings"`
}

// ─── ペイロード型: サーバー → コントローラー画面 ──────────────────────────────

// ControllerRoomStatePayload は room_state（コントローラー）のペイロード。
type ControllerRoomStatePayload struct {
	Status             string  `json:"status"`
	MyPlayerID         string  `json:"myPlayerID"`
	MyPullAccumulation float64 `json:"myPullAccumulation"`
}

// EliminatedPayload は eliminated のペイロード。
type EliminatedPayload struct {
	PlayerID string `json:"playerID"`
}

// ControllerGameFinishedPayload は game_finished（コントローラー）のペイロード。
type ControllerGameFinishedPayload struct {
	WinnerPlayerID     string  `json:"winnerPlayerID"`
	MyRank             int     `json:"myRank"`
	MyPullAccumulation float64 `json:"myPullAccumulation"`
}

// ErrorPayload は error のペイロード。
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
