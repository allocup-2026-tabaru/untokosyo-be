# GCP デプロイ手順

バックエンドを GCP Compute Engine VM 上でコンテナとして動かすための初期セットアップ手順。

> **注意**: 初期セットアップは一度だけ実行する。2回目以降のデプロイは `make deploy` のみで完了する。

---

## 前提条件

- `gcloud` CLI がインストール済みであること
- GCP アカウントにログイン済みであること（`gcloud auth login`）
- `.env` に GCP 設定が記載されていること（`.env.template` を参照）

---

## 環境変数の設定

`.env.template` をコピーして `.env` を作成し、各値を設定する。

```bash
cp .env.template .env
```

| 変数 | 説明 | デフォルト値 |
|------|------|-------------|
| `GCP_PROJECT_ID` | GCP プロジェクト ID | `untokosyo-be` |
| `GCP_REGION` | リージョン | `asia-northeast1` |
| `GCP_ZONE` | ゾーン | `asia-northeast1-a` |
| `VM_NAME` | VM インスタンス名 | `untokosyo-be-vm` |
| `DOMAIN` | 公開ドメイン（sslip.io 形式）| `changeme.sslip.io` |
| `JWT_SECRET` | JWT 認証シークレット（必須）| — |
| `LOG_LEVEL` | ログレベル | `info` |

`DOMAIN` は VM 作成後に外部 IP が確定してから設定する（「手順 3」参照）。  
`JWT_SECRET` は `make gen-secret` で生成してから `.env` に設定すること。

---

## 手順 1: GCP プロジェクトのセットアップ

```bash
# プロジェクト作成
gcloud projects create untokosyo-be --name=untokosyo-be

# 課金アカウントの確認と紐付け
gcloud billing accounts list
gcloud billing projects link untokosyo-be --billing-account=<ACCOUNT_ID>

# プロジェクトを切り替えて API を有効化
gcloud config set project untokosyo-be
gcloud services enable compute.googleapis.com artifactregistry.googleapis.com

# Artifact Registry リポジトリ作成
gcloud artifacts repositories create untokosyo-be \
  --repository-format=docker \
  --location=asia-northeast1 \
  --description="untokosyo-be Docker images"

# Docker 認証設定
gcloud auth configure-docker asia-northeast1-docker.pkg.dev
```

---

## 手順 2: VM インスタンスの作成

```bash
# VM 作成
gcloud compute instances create untokosyo-be-vm \
  --project=untokosyo-be \
  --zone=asia-northeast1-a \
  --machine-type=e2-small \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=20GB \
  --tags=http-server,https-server

# ファイアウォールルール作成（HTTP/HTTPS 許可）
gcloud compute firewall-rules create allow-http-https \
  --project=untokosyo-be \
  --allow=tcp:80,tcp:443 \
  --target-tags=http-server,https-server

# 外部 IP の確認
gcloud compute instances describe untokosyo-be-vm \
  --zone=asia-northeast1-a --project=untokosyo-be \
  --format="get(networkInterfaces[0].accessConfigs[0].natIP)"
```

---

## 手順 3: VM の初期セットアップ

外部 IP が確定したら `.env` の `DOMAIN` を更新する。  
sslip.io の形式: `1.2.3.4` → `1-2-3-4.sslip.io`

```bash
# 例: 外部IPが 34.84.3.130 の場合
# .env の DOMAIN=34-84-3-130.sslip.io に変更する
```

Docker CE をインストールし、設定ファイルを配置する。

```bash
# setup.sh を VM に転送して実行
gcloud compute scp ../infra/vm/setup.sh untokosyo-be-vm:/tmp/setup.sh \
  --zone=asia-northeast1-a --project=untokosyo-be
gcloud compute ssh untokosyo-be-vm \
  --zone=asia-northeast1-a --project=untokosyo-be \
  --command="sudo bash /tmp/setup.sh"

# デプロイ用ディレクトリを作成（初回のみ）
gcloud compute ssh untokosyo-be-vm \
  --zone=asia-northeast1-a --project=untokosyo-be \
  --command="sudo mkdir -p /opt/untokosyo-be"

# 初回デプロイ（compose.yml / Caddyfile / .env の転送も行う）
make deploy
```

---

## 通常デプロイ（2回目以降）

```bash
# infra ルートから
make deploy

# または BE ディレクトリから直接
make deploy
```

内部では `build → push → deploy-vm` の順に実行される。

---

## エンドポイント

| 用途 | URL |
|------|-----|
| ヘルスチェック | `https://<DOMAIN>/healthz` |
| WebSocket | `wss://<DOMAIN>/ws` |
