#!/usr/bin/env sh
set -eu

BASE_URL="${MIRAGE_BASE_URL:-http://localhost:18025}"
SMTP_URL="${MIRAGE_SMTP_URL:-smtp://localhost:11025}"
PAYLOAD_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/../payloads" && pwd)"

"$(dirname -- "$0")/start.sh"

printf 'Waiting for Mirage...\n'
ready=0
for _ in $(seq 1 30); do
  if curl -fsS "$BASE_URL/healthz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  printf 'Mirage did not become healthy in time.\n' >&2
  exit 1
fi

curl -fsS -X DELETE "$BASE_URL/api/v1/inbox" >/dev/null

curl -fsS --url "$SMTP_URL" \
  --mail-from "smtp-sender@example.com" \
  --mail-rcpt "smtp-user@example.com" \
  --upload-file "$PAYLOAD_DIR/smtp.eml"

"$(dirname -- "$0")/seed.sh"

inbox="$(curl -fsS "$BASE_URL/api/v1/inbox?limit=10")"
printf '%s\n' "$inbox"

case "$inbox" in
  *'"total":8'* ) ;;
  * ) printf 'Expected 8 seeded messages in inbox response.\n' >&2; exit 1 ;;
esac

case "$inbox" in
  *'Integration: Gmail-style mixed message'* ) ;;
  * ) printf 'Inbox response did not include Gmail-style MIME fixture.\n' >&2; exit 1 ;;
esac

case "$inbox" in
  *'Integration: related message with inline files'* ) ;;
  * ) printf 'Inbox response did not include related files MIME fixture.\n' >&2; exit 1 ;;
esac

case "$inbox" in
  *'Integration: SMTP capture'* ) ;;
  * ) printf 'Inbox response did not include SMTP fixture.\n' >&2; exit 1 ;;
esac

# The newest message is the last seeded fixture (with-files.eml), which is
# expected to carry attachments; keep it last in seed.sh.
message_id="$(printf '%s' "$inbox" | awk 'match($0, /"id":"[^"]+"/) { print substr($0, RSTART + 6, RLENGTH - 7); exit }')"
if [ -z "$message_id" ]; then
  printf 'Could not find message id in inbox response.\n' >&2
  exit 1
fi

detail="$(curl -fsS "$BASE_URL/api/v1/message/$message_id")"
case "$detail" in
  *'"name":"mirage-mark.svg"'* ) ;;
  * ) printf 'Message detail did not include the inline SVG attachment.\n' >&2; exit 1 ;;
esac

curl -fsS "$BASE_URL/api/v1/message/$message_id/body/raw" >/dev/null
curl -fsS "$BASE_URL/api/v1/message/$message_id/attachment/0" >/dev/null
curl -fsS "$BASE_URL/api/v1/message/$message_id/attachment/0?format=json" >/dev/null

printf 'Smoke check passed for message %s\n' "$message_id"
