# syntax=docker/dockerfile:1

###############################################################################
# admin-bot — Telegram bot for managing Piscine announcements & defense tables
# Deployment target: Railway.
#
# Environment variables (inject via Railway → Variables, NOT a .env file):
#   Required: TELEGRAM_TOKEN, ONEEDU_BASE_URL (https://), PLATFORM_ACCESS_TOKEN,
#             CHAT_IDS, SUPER_ADMIN_USER_ID
#   Authorization (request/approve flow): SUPER_ADMIN_USER_ID approves access
#             requests via inline buttons; ADMIN_USER_IDS pre-seeds an approved
#             allowlist on first start; approved users work in DMs, and in group
#             chats listed in ADMIN_CHAT_IDS (defaults to CHAT_IDS). Approved
#             users persist in ACCESS_STORE_PATH (default data/access.json).
#   Optional: TIMEZONE, TEMPLATES_PATH, ADMIN_USER_IDS, ACCESS_STORE_PATH,
#             ADMIN_CHAT_IDS, GOOGLE_CREDENTIALS_FILE, GOOGLE_CREDENTIALS_JSON,
#             GOOGLE_FOLDER_ID, SHEET_*_WEEK*, SHEET_UNIVERSAL, REGION_*_EVENT_ID
#             ONEEDU_ALLOW_INSECURE=1 (local dev only; permits http:// upstream)
###############################################################################

###############################################################################
# Stage 1 — builder
###############################################################################
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o bot ./cmd/bot/main.go

###############################################################################
# Stage 2 — final runtime image
###############################################################################
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

RUN adduser -D -u 10001 appuser

# The bot only reads these at runtime (message templates are loaded from disk;
# GraphQL queries are embedded in the binary). Inline Google credentials, when
# provided, are written to os.TempDir() — not /app — so appuser needs no write
# access here. Copy directly as appuser instead of a separate chown -R layer.
COPY --from=builder --chown=appuser:appuser /src/bot ./bot
COPY --from=builder --chown=appuser:appuser /src/messages ./messages

# The access store (ACCESS_STORE_PATH, default data/access.json) is written at
# runtime by the non-root appuser. /app is root-owned, so appuser cannot create
# data/ itself — pre-create it and hand it over. Persist this dir with a volume
# (see docker-compose.yml) so approved admins survive container re-creation.
RUN mkdir -p /app/data && chown appuser:appuser /app/data

USER appuser

# No EXPOSE: long-polling Telegram bot, no inbound port.

CMD ["./bot"]