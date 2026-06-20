# 認証仕様

## 概要

DBなし・セッションなしのステートレスJWT方式。  
トークンはURLクエリパラメータに含めず、**WS接続後の最初のメッセージ**で送信する。  
これによりサーバーログへのトークン露出を防ぐ。

ペイロード: `{ playerID, roomID, expiresAt(24h) }` / 署名: HS256

---

## ホスト: ルーム作成 〜 WS接続

```mermaid
sequenceDiagram
    actor Host as ホスト画面
    participant HTTP as HTTP Server
    participant WS as WS Server (HostClient)

    Host->>HTTP: POST /rooms
    HTTP-->>Host: 200 { roomID, hostPlayerID, token }

    Note over Host: token をローカルに保持

    Host->>WS: GET /ws/rooms/{roomID}/host
    WS-->>Host: WebSocket 接続確立

    Host->>WS: { "type": "auth", "token": "<JWT>" }

    alt JWT検証 OK
        WS-->>Host: { "type": "room_state", ... }
        Note over Host,WS: 以降ゲームイベントを送受信
    else JWT検証 NG (不正・期限切れ・5秒超過)
        WS-->>Host: 切断 (1008 Policy Violation)
    end
```

---

## プレイヤー: ルーム参加 〜 WS接続

```mermaid
sequenceDiagram
    actor Player as コントローラー画面
    participant HTTP as HTTP Server
    participant WS as WS Server (PlayerClient)

    Player->>HTTP: POST /rooms/{roomID}/players { name }
    HTTP-->>Player: 201 { playerID, name, token }

    Note over Player: token をローカルに保持

    Player->>WS: GET /ws/rooms/{roomID}/player?playerID={playerID}
    WS-->>Player: WebSocket 接続確立

    Player->>WS: { "type": "auth", "token": "<JWT>" }

    alt JWT検証 OK
        WS-->>Player: { "type": "room_state", ... }
        Note over Player,WS: 以降 pull / release / pong を送受信
    else JWT検証 NG (不正・期限切れ・5秒超過)
        WS-->>Player: 切断 (1008 Policy Violation)
    end
```

---

## 再接続時のなりすまし防止

```mermaid
sequenceDiagram
    actor Attacker as 攻撃者（別デバイス）
    participant WS as WS Server (PlayerClient)

    Note over Attacker: playerID は傍受できても token は知らない

    Attacker->>WS: GET /ws/rooms/{roomID}/player?playerID={playerID}
    WS-->>Attacker: WebSocket 接続確立

    Attacker->>WS: { "type": "auth", "token": "<偽トークン>" }
    WS-->>Attacker: 切断 (1008 Policy Violation)

    Note over WS: 署名検証失敗 → 正規プレイヤーのセッションは維持される
```

---

## JWT検証ロジック（サーバー側）

```
1. 最初のメッセージを 5秒 のタイムアウト付きで待機
2. { type: "auth", token: "..." } を受信
3. golang-jwt/jwt/v5 で署名・有効期限を検証
4. claims.playerID == URLの playerID（またはroom.hostPlayerID）
5. claims.roomID  == URLの roomID
6. すべて一致 → ゲームイベントループへ
7. いずれか失敗 → 1008 Policy Violation で即切断
```

---

## 環境変数

| 変数名 | 説明 | 生成方法 |
|---|---|---|
| `JWT_SECRET` | HS256署名キー（256bit以上推奨） | `make gen-secret` |

起動時に `JWT_SECRET` が空の場合はサーバーが `Fatal` で終了する。
