# ゲーム設計仕様: 大きなかぶ引っこ抜きチキンレース

## ゲーム概要

複数プレイヤーがカブを引っ張り合うチキンレースゲーム。
カブを引っ張り続けるほど貢献度が積み上がるが、カブが抜けた瞬間に引っ張っていたプレイヤーは全員脱落する。
いつ手を離すか、の読み合いがゲームの核心。

---

## 画面構成

| 画面 | WSエンドポイント | 役割 |
|---|---|---|
| ホスト画面 | `GET /ws/rooms/{roomID}/host` | カブのアニメーション表示・ルーム管理UI |
| コントローラー画面 | `GET /ws/rooms/{roomID}/player?playerID=xxx` | pull/release操作UI |

ホスト画面は1ルームにつき1接続を想定（ホストデバイスのブラウザ）。
コントローラーはプレイヤー各自がスマホ等から接続。

---

## ゲームフロー

```
ルーム作成 (POST /rooms)
    ↓
プレイヤー参加 (POST /rooms/{roomID}/players)  ← 名前のみ、DBなし
    ↓  ← ホスト画面にリアルタイム反映 (player_joined イベント)
ホストがゲーム開始 (POST /rooms/{roomID}/start)
    ↓
各プレイヤーがpull/releaseイベントをWSで送信
    ↓
tick毎に累積量が増加 → カブが抜ける確率が上昇
    ↓
確率ヒット → カブが抜ける
    ↓
pull中のプレイヤーが全員脱落 (eliminated)
    ↓
残存者の中でPullAccumulation最大のプレイヤーが勝利
    ↓
Room.Status = finished  ← ルームはまだ残る
    ↓
ルーム削除 (DELETE /rooms/{roomID})  ← 明示的な別工程
```

---

## カブが抜けるロジック

### ExtractionJudge インターフェース（差し替え可能）

```go
type JudgeInput struct {
    TotalPullAccumulation float64
    ActivePlayerCount     int
    PullingPlayerCount    int
    ElapsedSeconds        float64
}

type ExtractionJudge interface {
    Judge(input JudgeInput) (probability float64, extracted bool)
}
```

### デフォルト: SigmoidJudge

```
P(x) = 1 / (1 + exp(-steepness * (x - midpoint)))

x         = TotalPullAccumulation（ゲーム全体の累積pull量）
midpoint  = 100.0  → x=100 のとき P=50%
steepness = 0.05   → 曲線の急峻さ
```

- pullしているプレイヤーが0人のときは `extracted=false`（確率は更新する）
- tick毎に `rand.Float64() < P(x)` で判定

### pull蓄積の計算（TickRate=100ms）

```
毎tick（ラグ補正後のタイムスタンプを使用）:
  pull中のアクティブプレイヤー: PullAccumulation += PullDeltaPerTick (1.0)
  room.Turnip.TotalPullAccumulation += 1.0 × (pull中プレイヤー数)
```

### 将来の差し替え実装候補

| 実装名 | 数式 | 特徴 |
|---|---|---|
| `ExponentialJudge` | `1 - exp(-λ*x)` | 最初から緩やかに上昇 |
| `StepJudge` | 固定確率 | テスト・デバッグ用 |

---

## 勝利条件

- カブが抜けた瞬間にpull中の全アクティブプレイヤーが `eliminated`
- 残存アクティブプレイヤーの中で `PullAccumulation` 最大が勝者
- 残存者0人 → 引き分け（winner=nil）

---

## ラグ対策

ping/pong によるクロック同期 + クライアントタイムスタンプによるイベント補正の2段構え。

### 1. ping/pong クロック同期

```jsonc
// サーバー → クライアント（定期送信, 5秒毎）
{ "type": "ping", "serverTimestamp": 1718425600000 }

// クライアント → サーバー（即時返却）
{ "type": "pong", "serverTimestamp": 1718425600000, "clientTimestamp": 1718425600045 }
```

サーバーは pong 受信時に:
```
rtt = now - serverTimestamp
estimatedOneWayLatency = rtt / 2
clockOffset = serverTimestamp + rtt/2 - clientTimestamp  // クライアント時計のズレ
```

これをプレイヤーごとに `Player.LatencyMs` と `Player.ClockOffsetMs` として記録する。

### 2. イベントタイムスタンプ補正

pull/release イベントにクライアントタイムスタンプを付与:

```jsonc
{ "type": "pull", "playerID": "uuid", "clientTimestamp": 1718425600123 }
```

サーバーはイベント受信時に補正:
```
serverEquivTime = clientTimestamp + clockOffset
lag = serverNow - serverEquivTime
```

補正された時刻を使って pull 蓄積量を計算することで、ラグによる不公平を緩和する。

---

## 将来拡張: ラウンド制（未実装、設計のみ）

- 各ラウンドでカブが抜けても全員が継続参加
- 各ラウンドの貢献度を `RoundResult` に記録し累積
- ラウンド優勝者にボーナス付与
- 全ラウンド終了後、累積スコアで最終勝者決定
- `Room.Rounds []RoundResult` に蓄積する設計で対応

---

## HTTP API 仕様

```
POST   /rooms                           ルーム作成
GET    /rooms/{roomID}                  ルーム情報取得
POST   /rooms/{roomID}/players          ルーム参加（名前登録）
POST   /rooms/{roomID}/start            ゲーム開始（hostのみ）
DELETE /rooms/{roomID}                  ルーム削除

GET    /ws/rooms/{roomID}/host          ホスト画面用WS
GET    /ws/rooms/{roomID}/player?playerID=xx  コントローラー用WS
```

### リクエスト/レスポンス例

```jsonc
// POST /rooms → 201
{ "roomID": "abc123", "hostPlayerID": "uuid-xxx" }

// POST /rooms/{roomID}/players
// body: { "name": "たろう" }
// → 201
{ "playerID": "uuid-yyy", "name": "たろう" }

// GET /rooms/{roomID} → 200
{
  "roomID": "abc123",
  "status": "waiting",
  "hostPlayerID": "uuid-xxx",
  "players": [{ "playerID": "uuid-xxx", "name": "たろう", "status": "active" }],
  "turnip": { "totalPullAccumulation": 0.0, "extractionProbability": 0.0, "isExtracted": false }
}

// POST /rooms/{roomID}/start
// body: { "playerID": "uuid-xxx" }  ← hostのplayerIDで認証
// → 200
{ "ok": true }
```

---

## WSイベント仕様

### クライアント → サーバー（コントローラー画面）

```jsonc
{ "type": "pull",    "playerID": "uuid", "clientTimestamp": 1718425600123 }
{ "type": "release", "playerID": "uuid", "clientTimestamp": 1718425600456 }
{ "type": "pong",    "serverTimestamp": 1718425600000, "clientTimestamp": 1718425600045 }
```

### サーバー → ホスト画面

```jsonc
// 接続時のスナップショット
{ "type": "room_state", "payload": { "status": "waiting", "players": [...], "turnip": {...} } }

// プレイヤーが参加した（WAITING中でも送信）
{ "type": "player_joined", "payload": { "playerID": "uuid", "name": "たろう" } }

// ゲーム開始
{ "type": "game_start", "payload": { "startedAt": 1718425600000 } }

// tick毎（100ms）のカブ状態更新（アニメーション用）
{ "type": "turnip_update", "payload": { "totalPullAccumulation": 42.0, "extractionProbability": 0.12 } }

// プレイヤーのpull状態変化（アニメーション用）
{ "type": "player_update", "payload": { "playerID": "uuid", "isPulling": true } }

// カブが抜けた
{ "type": "extracted", "payload": { "eliminatedPlayerIDs": ["uuid1", "uuid2"] } }

// ゲーム終了
{
  "type": "game_finished",
  "payload": {
    "winnerPlayerID": "uuid",
    "winnerName": "たろう",
    "standings": [{ "playerID": "uuid", "name": "たろう", "pullAccumulation": 120.0, "rank": 1 }]
  }
}

// ping（クロック同期）
{ "type": "ping", "serverTimestamp": 1718425600000 }
```

### サーバー → コントローラー画面

```jsonc
// 接続時スナップショット（自分の状態 + ゲーム状態）
{ "type": "room_state", "payload": { "status": "waiting", "myPlayerID": "uuid", "myPullAccumulation": 0.0 } }

// ゲーム開始
{ "type": "game_start", "payload": { "startedAt": 1718425600000 } }

// 自分のpull/releaseのエコーバック（受付確認）
{ "type": "player_update", "payload": { "playerID": "uuid", "isPulling": true, "status": "active" } }

// 脱落通知
{ "type": "eliminated", "payload": { "playerID": "uuid" } }

// ゲーム終了
{ "type": "game_finished", "payload": { "winnerPlayerID": "uuid", "myRank": 2, "myPullAccumulation": 80.0 } }

// ping（クロック同期）
{ "type": "ping", "serverTimestamp": 1718425600000 }

// エラー
{ "type": "error", "payload": { "code": "ROOM_NOT_FOUND", "message": "ルームが存在しません" } }
```

---

## 認証方式

DBなし・セッションベースの簡易方式。

- ルーム参加時にサーバーが `playerID`（UUID）を発行して返す
- HTTPリクエストはbodyに `playerID` を含める
- WSはクエリパラメータ `?playerID=xxx` で識別
- ホスト権限は `hostPlayerID` との一致で確認するだけ
