# Stage 1: Build pkm-sync Go binary
FROM golang:1.24-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN apt-get update && apt-get install -y --no-install-recommends \
        libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /pkm-sync ./cmd

# Stage 2: Runtime with Python + tools
FROM python:3.12-slim-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        git \
        jq \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /pkm-sync /usr/local/bin/pkm-sync

RUN useradd -m -u 1001 -s /bin/bash pkmsync
USER 1001

WORKDIR /vault
