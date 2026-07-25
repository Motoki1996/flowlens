---
description: GitHub Issue の内容を読み取り、この repo の規約に沿って実装まで進める
argument-hint: <issue番号> [追加の指示]
allowed-tools: Bash(gh issue view:*), Bash(gh issue list:*), Bash(git status:*), Bash(git branch:*), Bash(git checkout:*), Bash(git diff:*), Bash(make *), Bash(cd apps/api && go *), Bash(cd apps/web && npx *), Read, Edit, Write, Grep, Glob
---

## 対象 Issue

!`gh issue view $1 --json number,title,body,labels,comments --jq '"#\(.number) \(.title)\n\nlabels: \(.labels|map(.name)|join(", "))\n\n\(.body)\n\n--- コメント ---\n\(.comments|map("@\(.author.login): \(.body)")|join("\n\n"))"'`

追加指示: $ARGUMENTS

## 実装手順

1. **理解フェーズ** — Issue 本文とコメントから、達成条件（受入基準）を箇条書きで自分の言葉に落とす。曖昧で、解釈によって成果物が変わる点だけ質問する。それ以外は判断して進める。

2. **調査フェーズ** — 触る範囲の既存コードを読む。関連する設計ドキュメントを必ず先に読む：
   - Web UI を触るなら `docs/ui-design.md`（OOUI。オブジェクト起点で設計し、レイアウトから始めない）
   - テストを書くなら `docs/testing.md`（Fake を優先、table-driven）
   - スキーマ・sync 周りなら `docs/plans/issue-sync.md` と `docs/architecture.md`
   - 該当する ADR (`docs/decisions/`)

3. **計画の提示** — 変更するファイルと方針を短く提示する。規模が大きい（3 ファイル超、またはマイグレーションを伴う）場合はここで一度止めて合意を取る。小さければそのまま進める。

4. **実装** — この repo の規約に従う：
   - `.up.sql` または `internal/database/queries/*.sql` を編集したら **必ず `make generate`**
   - `internal/database/db` の生成コードは手で編集しない
   - GitLab CE への呼び出しは必ず `internal/gitlab` の `gitlab.Client` インターフェース経由
   - 秘密情報は env / `.env` のみ。ハードコードしない
   - 周囲のコードのコメント量・命名・イディオムに合わせる

5. **検証** — 変更範囲に応じて実際に走らせ、**出力を貼って報告する**：
   - `make test`（Go + web の unit test）
   - `make lint`
   - マイグレーションを追加したなら `make migrate DATABASE_URL="$DATABASE_URL"`
   - 落ちたら落ちたと言う。通っていないものを通ったと書かない

6. **仕上げ** — 以下を行う前に必ず確認を取る（勝手に push/PR しない）：
   - ブランチ: `claude/issue-$1-<短い説明>`（現在 main 上なら必ず先にブランチを切る）
   - コミットメッセージは既存の履歴の形式に合わせ、末尾に `Co-Authored-By` 行を入れる
   - PR 本文には Issue へのリンク（`Closes #$1`）と、検証したことを書く

## 報告

最後に、実装したこと・検証結果・Issue のうち意図的にやらなかった部分（あれば理由も）をまとめる。
