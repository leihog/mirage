#!/usr/bin/env sh
set -eu

BASE_URL="${MIRAGE_BASE_URL:-http://localhost:18025}"
DOMAIN="${MIRAGE_DOMAIN:-example.com}"
PAYLOAD_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/../payloads" && pwd)"

post_form() {
  curl -fsS --user 'api:any-key' "$BASE_URL/v3/$DOMAIN/messages" "$@"
  printf '\n'
}

printf 'Sending form message with text, HTML, variables, and repeated fields...\n'
post_form \
  --form-string 'from=Sender <sender@example.com>' \
  --form-string 'to=User <user@example.com>' \
  --form-string 'subject=form text and html' \
  --form-string 'text=Plain text body from integration seed.' \
  --form-string 'html=<h1>Integration</h1><p>HTML body from integration seed.</p>' \
  --form-string 'h:X-Integration=form' \
  --form-string 'h:X-Tag=first' \
  --form-string 'h:X-Tag=second' \
  --form-string 'v:user-id=42' \
  --form-string 'o:tag=integration' \
  --form-string 'o:tag=seed'


printf 'Sending form message with CC and BCC...\n'
post_form \
  --form-string 'from=Sender <sender@example.com>' \
  --form-string 'to=User <user@example.com>' \
  --form-string 'cc=CC User <cc@example.com>' \
  --form-string 'bcc=BCC User <bcc@example.com>, BCC2 User <bcc2@example.com>' \
  --form-string 'subject=form cc and bcc' \
  --form-string 'text=Plain text body from integration seed.' \
  --form-string 'html=<h1>Integration</h1><p>HTML body from integration seed.</p>' \
  --form-string 'h:X-Integration=cc-bcc'

printf 'Sending HTML-only form message...\n'
post_form \
  --form-string 'from=Sender <sender@example.com>' \
  --form-string 'to=HTML User <html@example.com>' \
  --form-string 'subject=html only' \
  --form-string 'html=<p>This message intentionally has no text variant.</p>' \
  --form-string 'h:X-Integration=html-only'

# The unsubscribe target is a realistic newsletter-style URL that does not
# resolve. Clicking unsubscribe in the UI still demonstrates the one-click
# POST flow and shows the failed request details.
printf 'Sending one-click unsubscribe form message...\n'
post_form \
  --form-string 'from=Newsletter <news@example.com>' \
  --form-string 'to=Subscriber <subscriber@example.com>' \
  --form-string 'subject=unsubscribe headers' \
  --form-string 'text=Newsletter body.' \
  --form-string 'html=<p>Newsletter body.</p>' \
  --form-string 'h:List-Unsubscribe=<mailto:unsubscribe@news.example.com>, <https://news.example.com/unsubscribe/one-click>' \
  --form-string 'h:List-Unsubscribe-Post=List-Unsubscribe=One-Click'

for payload in \
  nested-alternative.eml \
  gmail-style.eml \
  with-files.eml
do
  printf 'Sending MIME fixture %s...\n' "$payload"
  curl -fsS --user 'api:any-key' "$BASE_URL/v3/$DOMAIN/messages.mime" \
    -F "message=@$PAYLOAD_DIR/$payload"
  printf '\n'
done
