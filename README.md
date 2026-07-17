# CarrierPigeon

YouTube チャンネルの新着動画・ライブ配信(予約/開始)を Discord に通知する Bot。

**外部からのインバウンドアクセスが不要**な設計です。YouTube の検知は RSS + Data API のポーリング、Discord へは Gateway への WebSocket 接続(アウトバウンド)のみで動作するため、ポート公開なしで運用できます。設計の詳細は [docs/PLAN.md](docs/PLAN.md) を参照してください。

## 必要なもの

1. **Discord Bot トークン** — [Discord Developer Portal](https://discord.com/developers/applications) でアプリケーションを作成し、Bot トークンを発行。特権インテントは不要
2. **YouTube Data API v3 キー** — [Google Cloud Console](https://console.cloud.google.com/) でプロジェクトを作成し、YouTube Data API v3 を有効化して API キーを発行
3. Bot をサーバーに招待(OAuth2 URL Generator で scope: `bot` `applications.commands`、権限: Send Messages / Embed Links)

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
