# syntax=docker/dockerfile:1

###############################################################################
# admin-bot — Telegram bot for managing Piscine announcements & defense tables
# Deployment target: Railway.
#
# Environment variables (inject via Railway → Variables, NOT a .env file):
#   Required: TELEGRAM_TOKEN, ONEEDU_BASE_URL (https://), PLATFORM_ACCESS_TOKEN, CHAT_IDS
#   Authorization: ADMIN_CHAT_IDS (optional; defaults to CHAT_IDS) — only these
#                  chats may issue commands / press inline buttons.
#   Optional: TIMEZONE, TEMPLATES_PATH, GOOGLE_CREDENTIALS_FILE,
#             GOOGLE_CREDENTIALS_JSON, SHEET_*_WEEK*
#             ONEEDU_ALLOW_INSECURE=1 (local dev only; permits http:// upstream)
###############################################################################

###############################################################################
# Stage 1 — builder
###############################################################################
# Pin to a specific patch (and ideally a @sha256 digest) for reproducible builds.
FROM golang:1.22.5-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o bot ./cmd/bot/main.go

###############################################################################
# Stage 2 — final runtime image
###############################################################################
# Pin the runtime base image to a concrete version rather than the moving
# `latest` tag. Replace with a @sha256 digest in CI for full supply-chain pinning.
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

RUN adduser -D -u 10001 appuser

COPY --from=builder /src/bot ./bot
COPY --from=builder /src/messages ./messages

USER appuser

# No EXPOSE: long-polling Telegram bot, no inbound port.

CMD ["./bot"]