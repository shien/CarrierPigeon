# CarrierPigeon 実装計画書

Discord 向け YouTube 更新・ライブ配信通知 Bot

## 1. 背景・目的

YouTube チャンネルの新着動画投稿・ライブ配信開始(および配信予約)を検知し、Discord の指定チャンネルへ自動通知する Bot を実装・運用する。

## 2. 前提・制約

| 項目 | 内容 |
|---|---|
| ネットワーク | **外部からのインバウンドアクセス不可**。アウトバウンド(インターネットへの接続)は可能 |
| 運用形態 | Docker Compose によるセルフホスト |
| 対象サービス | YouTube(将来的に他サービスへ拡張可能な設計とする) |

### 制約から導かれる設計方針

YouTube の公式なプッシュ通知手段である **WebSub (PubSubHubbub) はコールバック URL への HTTP POST 受信が必要なため利用できない**。したがって通知の検知は**ポーリング方式**を採用する。

一方 Discord 側は、Bot が Discord Gateway へ **WebSocket をアウトバウンドで張る**方式のため、インバウンド不可の環境でも問題なく動作する(スラッシュコマンドも Gateway 経由で受信可能)。Interactions Endpoint(HTTP 受信)は使用しない。

## 3. 機能要件

### 必須(MVP)

- **新着動画通知**: 監視対象チャンネルの新規動画投稿を検知し、Discord にリッチ Embed(サムネイル・タイトル・リンク)で通知する
- **ライブ配信通知**:
  - 配信予約(upcoming)の検知・通知(配信予定時刻付き)
  - 配信開始(live)の検知・通知
  - 同一配信について「予約→開始」の状態遷移を追跡し、それぞれ 1 回ずつだけ通知する(重複通知の防止)
- **監視チャンネル管理**(スラッシュコマンド):
  - `/subscribe <channel_url_or_id> [#通知先]` — 監視対象の追加
  - `/unsubscribe <channel_id>` — 監視対象の削除
  - `/list` — 監視中チャンネルの一覧表示
- **プレミア公開**の検知(YouTube 上は upcoming/live として扱われるため、ライブ配信と同じ仕組みで対応)

### 任意(将来拡張)

- 通知メッセージのテンプレートカスタマイズ、メンションロール指定
- Twitch・ニコニコ生放送など他サービス対応(Watcher を抽象化して追加)
- 配信終了通知、アーカイブ公開通知

## 4. 非機能要件

- ポーリング間隔: 既定 5 分(環境変数で変更可)。検知遅延は最大でポーリング間隔程度
- YouTube Data API の無料クォータ(10,000 units/日)内で運用できること(後述の試算参照)
- プロセス再起動・ホスト再起動後も通知済み状態を保持し、重複通知・通知漏れを起こさないこと(状態の永続化)
- 単一ホスト・単一コンテナ構成で運用が完結すること

## 5. 検知方式の設計

API クォータを節約するため、**RSS フィードで新着検知 → Data API で種別判定**の 2 段構成とする。

1. **新着検知(RSS)**: `https://www.youtube.com/feeds/videos.xml?channel_id=UCxxxx` を各監視チャンネルに対してポーリング。API キー不要・クォータ消費ゼロ。新規 video ID を抽出する
   - RSS は直近 15 件のみ返すため、初回登録時は既存分を「通知済み」として初期化する(過去分の一斉通知を防ぐ)
2. **種別判定(YouTube Data API v3)**: 新規検知した video ID を `videos.list`(`part=snippet,liveStreamingDetails`)でまとめて照会(1 リクエスト最大 50 件、消費 1 unit)し、以下に分類する
   - `liveBroadcastContent = "none"` → 通常動画 → 新着動画として通知
   - `liveBroadcastContent = "upcoming"` → 配信予約/プレミア公開予約 → 予約通知し、状態を追跡対象に登録
   - `liveBroadcastContent = "live"` → 配信中 → 配信開始通知
3. **状態遷移の追跡**: upcoming として記録済みの動画は、別途 `videos.list` で定期確認し、live に遷移したタイミングで「配信開始」を通知する
   - 配信予定時刻(`scheduledStartTime`)が近い動画は確認間隔を短く、遠い動画は長くする(適応的ポーリング)

### クォータ試算

監視 20 チャンネル・5 分間隔の場合:

- RSS ポーリング: 0 units
- 新着分類: 新着があったサイクルのみ 1 unit(50 件までバッチ)→ 高頻度でも数十 units/日
- upcoming 追跡: 追跡中の配信をまとめて 1 unit/確認 → 5 分毎に確認しても約 288 units/日

**合計で数百 units/日程度**であり、無料クォータ 10,000 units に対して十分な余裕がある。

## 6. 技術選定

| 項目 | 選定 | 理由 |
|---|---|---|
| 言語 | Python 3.12 | discord.py の成熟度、非同期処理(asyncio)との親和性 |
| Discord ライブラリ | discord.py 2.x | Gateway 接続・スラッシュコマンド(app_commands)対応 |
| HTTP クライアント | aiohttp | RSS / YouTube API の非同期取得 |
| RSS パース | feedparser | 実績・簡便さ |
| DB | SQLite(aiosqlite) | 単一コンテナ運用に十分。ボリュームで永続化 |
| コンテナ | Docker / Docker Compose | 要件 |

Discord への投稿は Bot アカウント経由とする(Webhook URL 直叩きでも通知自体は可能だが、スラッシュコマンドによる監視チャンネル管理を行うため Bot 化する)。

## 7. アーキテクチャ

```
+--------------------------------------------------+
| carrierpigeon コンテナ (単一プロセス, asyncio)     |
|                                                  |
|  +----------------+     +---------------------+  |
|  | Discord Client |<--->| Command Handler     |  |
|  | (Gateway WS)   |     | /subscribe 等       |  |
|  +-------+--------+     +----------+----------+  |
|          ^                          |            |
|          | 通知                     v            |
|  +-------+--------+     +---------------------+  |
|  | Notifier       |     | SQLite (volume)     |  |
|  | (Embed 生成)   |     | subscriptions/videos|  |
|  +-------+--------+     +----------+----------+  |
|          ^                          ^            |
|  +-------+--------------------------+---------+  |
|  | YouTube Watcher (ポーリングループ)          |  |
|  |  - RSS fetch (新着検知)                     |  |
|  |  - Data API videos.list (種別判定/live遷移) |  |
|  +---------------------------------------------+  |
+--------------------------------------------------+
     |アウトバウンドのみ: Discord Gateway/API,
     |youtube.com (RSS), googleapis.com (Data API)
     v
  インターネット
```

### データモデル(SQLite)

```sql
-- 監視対象チャンネル
CREATE TABLE subscriptions (
  yt_channel_id      TEXT NOT NULL,   -- UCxxxx
  yt_channel_name    TEXT,
  discord_guild_id   INTEGER NOT NULL,
  discord_channel_id INTEGER NOT NULL, -- 通知先
  created_at         TEXT NOT NULL,
  PRIMARY KEY (yt_channel_id, discord_channel_id)
);

-- 検知済み動画と通知状態
CREATE TABLE videos (
  video_id        TEXT PRIMARY KEY,
  yt_channel_id   TEXT NOT NULL,
  title           TEXT,
  state           TEXT NOT NULL,  -- none / upcoming / live / ended
  scheduled_start TEXT,           -- upcoming の配信予定時刻
  notified_upload INTEGER DEFAULT 0,  -- 新着通知済みフラグ
  notified_live   INTEGER DEFAULT 0,  -- 配信開始通知済みフラグ
  updated_at      TEXT NOT NULL
);
```

## 8. ディレクトリ構成(予定)

```
CarrierPigeon/
├── docs/
│   └── PLAN.md              # 本計画書
├── src/carrierpigeon/
│   ├── __main__.py          # エントリポイント
│   ├── config.py            # 環境変数の読み込み・検証
│   ├── bot.py               # Discord クライアント・スラッシュコマンド
│   ├── notifier.py          # Embed 生成・送信
│   ├── store.py             # SQLite アクセス層
│   └── watchers/
│       ├── base.py          # Watcher 抽象クラス(将来の他サービス拡張点)
│       └── youtube.py       # RSS + Data API ポーリング
├── tests/
├── Dockerfile
├── docker-compose.yml
├── .env.example
├── pyproject.toml
└── README.md
```

## 9. Docker Compose 構成

```yaml
# docker-compose.yml
services:
  carrierpigeon:
    build: .
    restart: unless-stopped
    env_file: .env
    volumes:
      - pigeon-data:/data          # SQLite 永続化
    environment:
      - DB_PATH=/data/carrierpigeon.db
    healthcheck:
      test: ["CMD", "python", "-m", "carrierpigeon.healthcheck"]
      interval: 60s
      timeout: 10s
      retries: 3

volumes:
  pigeon-data:
```

```dotenv
# .env.example
DISCORD_TOKEN=            # Discord Developer Portal で発行した Bot トークン
YOUTUBE_API_KEY=          # Google Cloud Console で発行した Data API v3 キー
POLL_INTERVAL_SECONDS=300 # RSS ポーリング間隔(既定 5 分)
LOG_LEVEL=INFO
```

- ポート公開(`ports:`)は一切不要 — すべてアウトバウンド接続のため、インバウンド不可環境の要件と合致
- ヘルスチェックはポーリングループの最終実行時刻をファイル/DB に記録し、それを検査する方式(HTTP エンドポイントを立てない)
- ログは stdout に出力し `docker compose logs` で確認

## 10. 事前準備(手動作業)

1. **Discord Bot の作成**: Discord Developer Portal でアプリケーション作成 → Bot トークン発行 → サーバーへ招待(必要権限: Send Messages, Embed Links。特権インテント不要)
2. **YouTube API キーの発行**: Google Cloud Console でプロジェクト作成 → YouTube Data API v3 を有効化 → API キー発行

## 11. 実装フェーズ

| フェーズ | 内容 | 完了条件 |
|---|---|---|
| 1. 骨格 | プロジェクト雛形、config、Dockerfile / compose、Bot のログイン確認 | コンテナ起動で Bot がオンラインになる |
| 2. 購読管理 | SQLite スキーマ、`/subscribe` `/unsubscribe` `/list` 実装(URL/ハンドルからのチャンネル ID 解決含む) | コマンドで監視対象を CRUD できる |
| 3. 新着通知 | RSS ポーリング、初回登録時の既存動画初期化、新着動画の Embed 通知 | 新規投稿が 1 回だけ通知される |
| 4. ライブ通知 | Data API による種別判定、upcoming 追跡と live 遷移通知、適応的ポーリング | 予約・開始がそれぞれ 1 回だけ通知される |
| 5. 運用性 | ヘルスチェック、リトライ/バックオフ、構造化ログ、README 整備 | compose で常用運用できる |
| 6. テスト | store / watcher の単体テスト(API はモック)、CI 設定 | 主要ロジックにテストがある |

## 12. エラーハンドリング方針

- **RSS / API の一時失敗**: 指数バックオフでリトライし、失敗してもループは継続(次サイクルで回復)
- **YouTube API クォータ枯渇 (403 quotaExceeded)**: Data API 呼び出しを翌日のクォータリセットまで停止し、RSS ベースの新着通知のみ継続(ライブ判定は劣化運転)。警告ログを出力
- **Discord 送信失敗 / レートリミット**: discord.py の内蔵レートリミット処理に委譲。通知失敗時は notified フラグを立てず次サイクルで再送
- **Gateway 切断**: discord.py の自動再接続に委譲。コンテナ異常終了時は `restart: unless-stopped` で復帰

## 13. リスクと対策

| リスク | 影響 | 対策 |
|---|---|---|
| RSS の反映遅延(数分程度) | 通知遅延 | 許容範囲として仕様化。厳格化が必要なら間隔短縮 |
| ゲリラ配信が RSS に載るまでのラグ | ライブ検知の遅れ | 検知後即座に Data API で判定し遅延を最小化 |
| メンバー限定・限定公開動画は RSS/API に出ない | 通知対象外 | 仕様として明記 |
| API 仕様変更・クォータポリシー変更 | 検知不能 | Watcher を抽象化しており検知手段の差し替えが局所化されている |
| SQLite ファイル破損 | 状態喪失 → 重複通知 | ボリューム永続化 + 起動時整合性チェック。最悪でも RSS 直近 15 件分の重複に留まる |

## 14. スコープ外

- 外部からの HTTP アクセスを要する機能全般(WebSub 受信、Web 管理画面の外部公開、Discord Interactions Endpoint)
- 複数ホストへのスケールアウト、外部 DB(PostgreSQL 等)
- YouTube 以外のサービス対応(拡張点のみ用意)
