# kuchikomi-ai-api-go

[![CI](https://github.com/akiijauto/kuchikomi-ai-api-go/actions/workflows/ci.yml/badge.svg)](https://github.com/akiijauto/kuchikomi-ai-api-go/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8.svg)](https://go.dev/)

稼働中の個人開発サービス **クチコミ返信AI**（口コミへの返信文をAIが下書きするSaaS）の API を、
**Go の標準ライブラリだけ**で実装したもの。Web フレームワークは使っていない。

同じ API を Next.js・Ruby on Rails でも実装しており、**3つの実装が同一のデータベース・同一のスキーマを見て
AWS 上で同時に稼働する**ことを実測している。その比較の全体像は元リポジトリ
[kuchikomi-ai-multi-stack](https://github.com/akiijauto/kuchikomi-ai-multi-stack) にある。

---

## 何をするサービスか

店舗事業者が受け取った口コミに対して、返信文の案を3つ生成する。無料プランは月5件まで。

| メソッド | パス | 内容 |
|---|---|---|
| `GET` | `/api/health` | 死活監視。認証基盤にもDBにも依存しない |
| `POST` | `/api/generate` | 認証 → 入力検証 → 利用回数の上限チェック → 返信文の生成 |
| `POST` | `/api/demo/token` | デモ用トークン発行。`DEMO_MODE=1` のときだけ**経路そのものが存在する** |

ステータスコードとエラー文言は Next.js版・Rails版に合わせてある。同じ画面から呼び先を差し替えるだけで動く。

---

## 設計判断

このリポジトリの読みどころは実装量ではなく、**なぜそう書かなかったか**のほうにある。

### 1. ルータのライブラリを入れなかった

Gin も chi も使わず、標準の `net/http` だけで書いた。
Go 1.22 以降の `http.ServeMux` は `"POST /api/generate"` のように**メソッド込みのパターン**を扱えるため、
この規模ならそれで足りる。外部依存は3つだけ。

```go
mux.HandleFunc("GET /api/health", s.handleHealth)
mux.HandleFunc("POST /api/generate", s.withAuth(s.handleGenerate))
if cfg.DemoMode {
    mux.HandleFunc("POST /api/demo/token", s.handleDemoToken)
}
```

**依存を足さないと決めることも設計判断**なので、記録として残している。

### 2. 利用回数の上限判定を、アプリ側に持ってこなかった

「今月すでに何件使ったか調べる → 5件未満なら1件足す」と書くのが素直だが、これには穴が2つある。

- **同時に来ると数え間違える**：「調べる」と「書く」の間に別のリクエストが割り込むと、両方が「まだ4件だ」と判断して両方通る
- **実装が増えると別々に数える**：入り口を3つ作った瞬間に、実質の上限が3倍になる

そこで数える仕事をアプリに持たせず、**データベース側の関数（`public.increment_usage`）に1か所だけ置いた**。
そこでは判定と加算が1文で行われるので、割り込む隙がない。

```go
// internal/store/store.go
// increment_usage の中の auth.uid() が読む値を立てる。
// 第3引数 true は「このトランザクションの中だけ有効」という意味で、
// プールの接続を使い回しても他のリクエストへ漏れない。
tx.Exec(ctx, `select set_config('request.jwt.claim.sub', $1, true)`, userID)
tx.QueryRow(ctx, `select public.increment_usage($1)`, month).Scan(&count)
```

**実測**：

- 同時10リクエストを投げて、成功が**ちょうど5件**・429が**ちょうど5件**（`TestConcurrentGenerateStopsAtLimit`）
- AWS 上で Go版と Rails版を交互に呼び、`used` が 1→5 と進んで**6回目はどちらから呼んでも429**

### 3. 文字数を `len()` で数えなかった

口コミ本文の長さ制限（5〜2000文字）は `utf8.RuneCountInString` で数えている。
Go の `len()` は**バイト数**なので、`len("美味")` は 6 になり、2文字なのに「5文字以上」を通ってしまう。

TypeScript版も Ruby版も文字数で数えるため、バイト数で数えると**同じ入力に3実装が別の答えを返す**。
この落とし穴はテストに残してある。

### 4. JWT で受け入れる署名方式を、こちら側で固定した

```go
_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) {
    return []byte(secret), nil
}, jwt.WithValidMethods([]string{"HS256"}))
```

`WithValidMethods` が無いと、トークンのヘッダに書かれた `alg` をライブラリが信じる経路が生まれる
（`alg: none` を渡すと署名なしで通る、いわゆる alg 混同）。
**署名なしトークンが拒否されることをテストに入れてある。**

### 5. 実行イメージに OS を入れなかった

`CGO_ENABLED=0` で静的リンクし、実行ステージは `gcr.io/distroless/static-debian12:nonroot`。
シェルもパッケージマネージャも入っていない。`scratch` にしなかったのは、
Anthropic API を HTTPS で呼ぶのに CA 証明書が要るため。

代償もある。**`docker exec app id -u` のような確認ができない**ので、
非root確認は `docker inspect` で設定を見る形にし、死活監視は
**バイナリ自身に自分を叩かせる**形にした。

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/app/api", "-healthcheck"]
```

これは AWS ECS 上でも `HEALTHY` になることを確認している。

| 実装 | ローカルの実行イメージ | ECR上（圧縮後） |
|---|---|---|
| Next.js | 202MB | 66.5MB |
| Rails | 未計測 | 155.3MB |
| **Go（本リポジトリ）** | **17.9MB** | **5.7MB** |

### 6. マイグレーションを持たない

テーブル定義の正本は元リポジトリの `web/supabase/schema.sql` のままにしてある。
**同じ定義に別言語の実装を載せる**のがこの演習の主旨なので、定義を複製する理由がない。

CI もこの方針に従い、**毎回その正本を取りに行って**テストを流している（`.github/workflows/ci.yml`）。

---

## 構成

```
cmd/api/main.go        起動・設定読み込み・graceful shutdown・-healthcheck
internal/app/          ルーティング / 認証middleware / 入力検証 / 応答
internal/auth/         Supabaseが発行したJWTの検証と、デモ用の発行
internal/store/        PostgreSQLアクセス（pgx）。表の定義は作らない
internal/reply/        返信生成（Claude / APIキーが無いときのデモ返信）
public/demo.html       ブラウザから試せるデモ画面
```

外部依存は3つだけ。

| 依存 | 用途 |
|---|---|
| `github.com/jackc/pgx/v5` | PostgreSQL 接続 |
| `github.com/golang-jwt/jwt/v5` | JWT の検証・発行 |
| `github.com/anthropics/anthropic-sdk-go` | 返信文の生成 |

---

## 環境変数

| 変数 | 必須 | 既定 | 内容 |
|---|---|---|---|
| `DATABASE_URL` | ✅ | — | `postgresql://user:pass@host:5432/db` |
| `SUPABASE_JWT_SECRET` | ✅ | — | トークンの検証鍵。未設定だと認証つきの口は500を返す |
| `PORT` | | `8080` | 待ち受けポート |
| `PUBLIC_DIR` | | `public` | 静的ファイルの場所 |
| `DEMO_MODE` | | 空 | `1` のときだけデモ用トークンの口が生える |
| `LOAD_SCHEMA` | | 空 | `1` のとき、起動前に `BOOTSTRAP_DIR` の `*.sql` を適用する |
| `BOOTSTRAP_DIR` | | `db/bootstrap` | 適用するSQLの置き場所 |
| `ANTHROPIC_API_KEY` | | 空 | 無ければデモ返信（`mock: true`）を返す |
| `GENERATION_MODEL` | | `claude-sonnet-4-6` | 他の2実装と同じ既定値 |

---

## 動かす

```bash
export DATABASE_URL=postgresql://postgres:postgres@localhost:5432/kuchikomi
export SUPABASE_JWT_SECRET=dev-secret
export DEMO_MODE=1
go run ./cmd/api
# → http://localhost:8080/demo.html
```

スキーマは元リポジトリから取得して流し込む。

```bash
BASE=https://raw.githubusercontent.com/akiijauto/kuchikomi-ai-multi-stack/main
curl -fsSL "$BASE/db/init/00_supabase_compat.sql" | psql "$DATABASE_URL"
curl -fsSL "$BASE/web/supabase/schema.sql"        | psql "$DATABASE_URL"
```

AWS 上では RDS に外から接続できない（`publicly_accessible = false`、踏み台もALBも無い）ため、
`LOAD_SCHEMA=1` で起動すると**アプリ自身が**起動前に `BOOTSTRAP_DIR` の `*.sql` を適用する。

---

## テスト

```bash
go test ./...                                   # DBを使わないテストだけ走る
DATABASE_URL=postgresql://... go test ./... -race   # 全部走る
```

**20件（サブテスト込み46件）。** DBを使うテストは `DATABASE_URL` が無ければ **SKIP と表示して飛ばす**。
黙って成功扱いにはしない（「通った」と「試していない」を取り違えないため）。

確認している内容の一部:

- 認証なし / 別の鍵で署名 / 期限切れ / 壊れたトークン → すべて401
- **`alg=none`（署名なし）のトークン → 拒否**
- 2文字の日本語を弾く（バイト数で数えていないこと）
- 無料プランの上限5件で429、6件目は加算されない
- **同時10本でも成功はちょうど5本**
- 他人の利用回数に影響しない
- `DEMO_MODE` 無効時にデモ用の口が存在しない（404）

---

## コンテナ

```bash
docker build -t kuchikomi-go .
docker run --rm -p 8080:8080 -e DATABASE_URL=... -e SUPABASE_JWT_SECRET=... kuchikomi-go
```

`db/bootstrap/*.sql` は git 管理外。CI とデプロイのワークフローが `docker build` の直前に取得して置く。

---

## ライセンス

MIT License（[LICENSE](LICENSE)）
