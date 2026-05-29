# syntax=docker/dockerfile:1

###############################################################################
# admin-bot — Telegram bot for managing Piscine announcements & defense tables
#
# Deployment target: Railway (auto-detects this Dockerfile in the repo root,
# no railway.toml required).
#
# ---------------------------------------------------------------------------
# Environment variables (inject via Railway → Variables, NOT via a .env file)
# ---------------------------------------------------------------------------
# Required:
#   TELEGRAM_TOKEN          Telegram bot token (from @BotFather)
#   ONEEDU_BASE_URL         01-edu platform URL (e.g. learn.tomorrow-school.com)
#   PLATFORM_ACCESS_TOKEN   Access token for the 01-edu API
#   CHAT_IDS                Comma-separated chat IDs (e.g. -100123,-100987)
#
# Optional:
#   TIMEZONE                IANA tz for cron (default: Asia/Almaty)
#   TEMPLATES_PATH          Path to message templates (default: messages)
#   GOOGLE_CREDENTIALS_FILE Path to the Google service-account JSON key.
#                           Do NOT bake this into the image — mount it via a
#                           Railway volume or inject the file at runtime.
#   SHEET_GO_WEEK1 .. WEEK3 Pre-configured Google Sheets URLs (Piscine Go)
#   SHEET_JS_WEEK1 .. WEEK3 Pre-configured Google Sheets URLs (Piscine JS)
#   SHEET_AI_WEEK1 .. WEEK2 Pre-configured Google Sheets URLs (Piscine AI)
###############################################################################


###############################################################################
# Stage 1 — builder
###############################################################################
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Download dependencies first so this layer is cached unless go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source.
COPY . .

# Build a fully static binary (CGO_ENABLED=0) so it runs on bare alpine
# without glibc. -trimpath strips local paths; -s -w drops the symbol table
# and DWARF debug info to shrink the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o bot ./cmd/bot/main.go


###############################################################################
# Stage 2 — final runtime image
###############################################################################
FROM alpine:latest

# ca-certificates  — TLS verification for the 01-edu API and Google Sheets API.
# tzdata           — IANA zoneinfo so time.LoadLocation("Asia/Almaty") resolves.
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Run as an unprivileged user rather than root.
RUN adduser -D -u 10001 appuser

# Compiled binary from the builder stage.
COPY --from=builder /src/bot ./bot

# Message templates, read at runtime via TEMPLATES_PATH (default "messages").
COPY --from=builder /src/messages ./messages

USER appuser

# No EXPOSE: this is a long-polling Telegram bot, it opens no inbound port.

CMD ["./bot"]