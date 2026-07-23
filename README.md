# CarrierPigeon

YouTube チャンネルの新着動画・ライブ配信(予約/開始)を Discord に通知する Bot。

**外部からのインバウンドアクセスが不要**な設計です。YouTube の検知は RSS + Data API のポーリング、Discord へは Gateway への WebSocket 接続(アウトバウンド)のみで動作するため、ポート公開なしで運用できます。設計の詳細は [docs/PLAN.md](docs/PLAN.md) を参照してください。

## 必要なもの

1. **Discord Bot トークン**
   1. [Discord Developer Portal](https://discord.com/developers/applications) にアクセスしてログイン
   2. 右上の「New Application」でアプリケーションを作成
   3. 左メニューの「Bot」を開き、「Reset Token」(初回は表示されるトークン)を「Copy」する
      - トークンは一度しか表示されないため、その場でコピーして保管する
      - 特権インテント(Presence/Server Members/Message Content)は不要
2. **YouTube Data API v3 キー**
   1. [Google Cloud Console](https://console.cloud.google.com/) にアクセスしてログイン
   2. 新しいプロジェクトを作成(または既存プロジェクトを選択)
   3. 「APIとサービス」→「ライブラリ」で「YouTube Data API v3」を検索し「有効にする」
   4. 「APIとサービス」→「認証情報」→「認証情報を作成」→「APIキー」でキーを発行
      - 発行後「APIキーを制限」から「YouTube Data API v3」のみに制限しておくと安全
3. **Bot をサーバーに招待**
   1. Developer Portal の「OAuth2」→「URL Generator」を開く
   2. SCOPES で `bot` `applications.commands` にチェック
   3. BOT PERMISSIONS で `Send Messages` `Embed Links` にチェック
   4. 生成された URL をブラウザで開き、招待先サーバーを選択

## Dockerのインストール(未導入の場合)

WSL2(Ubuntu)環境であれば、以下のスクリプトでDocker Engineを直接インストールできます。

```console
$ ./setup/install-docker.sh
```

実行後、グループ変更を反映するためWSLを再起動(Windows側で `wsl --shutdown` してから再度開く)し、以下でデーモンを起動・確認します。

```console
$ sudo service docker start
$ docker run hello-world
```

WSL2以外、またはGUIでの管理を好む場合は [Docker Desktop for Windows](https://www.docker.com/products/docker-desktop/) をインストールし、設定の「WSL Integration」で対象ディストリビューションを有効化する方法もあります。

## 起動

```console
$ cp .env.example .env
$ vi .env               # DISCORD_TOKEN と YOUTUBE_API_KEY を設定
$ docker compose up -d --build
$ docker compose logs -f
```

## 使い方(スラッシュコマンド)

| コマンド | 説明 |
|---|---|
| `/subscribe channel:<URL・UC…ID・@ハンドル> [notify_channel:#チャンネル]` | 監視対象を追加(通知先省略時は実行チャンネル) |
| `/unsubscribe channel:<URL・UC…ID・@ハンドル>` | 監視対象を削除 |
| `/list` | 監視中チャンネルの一覧 |

`/subscribe` `/unsubscribe` には「サーバー管理」権限が必要です。登録直後は既存動画を「通知済み」として初期化するため、過去動画が一斉通知されることはありません。以後、新着動画・配信予約・配信開始がそれぞれ 1 回ずつ通知されます。

## 環境変数

| 変数 | 既定値 | 説明 |
|---|---|---|
| `DISCORD_TOKEN` | (必須) | Bot トークン |
| `YOUTUBE_API_KEY` | (必須) | Data API v3 キー |
| `POLL_INTERVAL_SECONDS` | `300` | ポーリング間隔(秒) |
| `DB_PATH` | `/data/carrierpigeon.db` | SQLite ファイルパス |
| `HEARTBEAT_PATH` | `/data/heartbeat` | ヘルスチェック用ハートビートファイル |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |

## 開発

```console
$ go test ./...
$ go build ./cmd/carrierpigeon
```

## 制限事項

- 検知遅延は最大でポーリング間隔 + YouTube の RSS 反映ラグ(数分)程度
- メンバー限定・限定公開の動画/配信は RSS・API に現れないため通知されません
