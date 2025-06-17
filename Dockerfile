# ベースイメージとして Go の公式イメージを使用
FROM golang:1.24.4-bookworm

# クロスコンパイルに必要なツール（MinGW-w64）をインストール
RUN apt-get update && apt-get install -y --no-install-recommends \
    mingw-w64 \
    gcc-mingw-w64-x86-64 \
    git \ 
    && rm -rf /var/lib/apt/lists/*

# 作業ディレクトリを設定
WORKDIR /app

# ホストからプロジェクトのファイルをコピー（Dockerfileとgo.modが同じディレクトリにある場合）
# (もし docker-compose.yml で context: . を使っているならこのままでOK)
# (もし context: https://github.com/... を使っているならこのCOPYは不要です。
#  その場合、Gitリポジトリのルートが/appに自動的にコピーされます。)

# go.mod と go.sum を先にコピーしてキャッシュを有効にする
# (ホストでgo.mod/go.sumに変更がない場合、依存関係の再ダウンロードを避ける)
COPY go.mod ./
COPY go.sum ./

# 依存関係をダウンロード
RUN go mod tidy

# 残りのソースコードをコピー
COPY . .
