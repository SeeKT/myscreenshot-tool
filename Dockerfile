# Copyright (c) 2025 SeeKT
# This source code is licensed under the MIT license found in the LICENSE file in the root directory of this source tree.
FROM golang:1.24.4-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    mingw-w64 \
    gcc-mingw-w64-x86-64 \
    git \ 
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

RUN go mod tidy

COPY . .
