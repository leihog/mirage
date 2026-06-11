# Mirage

Mirage is a local email capture service for testing applications that send via SMTP or Mailgun-compatible APIs.

It keeps messages in memory only. Restarting the process clears the mailbox. The inbox keeps the most recent 100 messages; when the cap is reached the oldest message is dropped. Tune it with `-max-messages` (`0` disables the cap).

> [!WARNING]
> Mirage has no authentication and is meant for local development and test networks only. Anyone who can reach the HTTP port can read captured mail, delete it, and trigger the unsubscribe feature, which makes Mirage send an HTTP request to a URL taken from a captured message. Do not expose Mirage to untrusted networks.

## Run

With Docker:

```sh
docker run --rm -p 8025:8025 -p 1025:1025 ghcr.io/leihog/mirage:latest
```

With Docker Compose:

```yaml
services:
  mirage:
    image: ghcr.io/leihog/mirage:latest
    ports:
      - "8025:8025"
      - "1025:1025"
```

When another service runs in the same Compose project, use `http://mirage:8025` as the Mailgun base URL.
Use `mirage:1025` as the SMTP host and port.

From source:

```sh
go run ./cmd/mirage
```

Open <http://localhost:8025>.
Configure apps that send through SMTP to use `localhost:1025`.

The web UI refreshes the message list automatically (server-sent events with a polling fallback) and shows each message as HTML, HTML source, text, headers, and raw MIME-style source. Messages that carry RFC 8058 one-click unsubscribe headers (`List-Unsubscribe` plus `List-Unsubscribe-Post`) get an unsubscribe button that sends the one-click POST and shows the response, so you can test your unsubscribe flow end to end.

Use another address or different limits if needed:

```sh
go run ./cmd/mirage -http-addr :8080 -smtp-addr :1026 -max-messages 1000 -max-message-bytes 67108864
```

Messages larger than `-max-message-bytes` (32 MiB by default) are rejected by both the SMTP and Mailgun-compatible endpoints.

Or install the binary with Go:

```sh
go install github.com/leihog/mirage/cmd/mirage@latest
```

## SMTP capture

Point your app's SMTP settings at this process:

```text
host: localhost
port: 1025
TLS: disabled
authentication: disabled
```

Mirage accepts local SMTP messages and stores them in the same in-memory inbox as Mailgun-compatible requests.

Example SMTP request with curl:

```sh
curl --url smtp://localhost:1025 \
  --mail-from sender@example.com \
  --mail-rcpt user@example.com \
  --upload-file ./message.eml
```

## Mailgun-compatible endpoints

Point your app's Mailgun base URL at this process and keep the usual domain path:

```text
http://localhost:8025/v3/YOUR_DOMAIN/messages
```

Supported now:

- `POST /v3/{domain}/messages`
- `POST /v3/{domain}/messages.mime`

Authentication is accepted but ignored, so existing clients can keep sending Basic auth credentials.

Repeated `h:` header and `o:` option fields are preserved as multiple values, matching Mailgun, so messages round-trip faithfully (for example repeated `o:tag` fields or custom headers sent more than once).

Example form request:

```sh
curl -s --user 'api:any-key' \
  http://localhost:8025/v3/example.com/messages \
  --form-string from='Sender <sender@example.com>' \
  --form-string to='User <user@example.com>' \
  --form-string subject='Hello from local Mailgun' \
  --form-string text='Plain text body' \
  --form-string html='<h1>Hello</h1><p>HTML body</p>'
```

Example MIME request:

```sh
curl -s --user 'api:any-key' \
  http://localhost:8025/v3/example.com/messages.mime \
  -F message=@./message.eml
```

## API endpoints

The `/api/v1` endpoints are intended for scripts and automated tests. JSON timestamps are returned in UTC. Header, variable, and option values are JSON arrays (one entry per occurrence) because mail headers and Mailgun fields can repeat.

- `GET /healthz`: health check endpoint. Returns `204 No Content`.
- `GET /api/v1/inbox?limit=50&offset=0`: paginated inbox summary with `total`, `filteredTotal`, and `unreadTotal` counts. Add `unread=true` (or `false`) to filter by read state and `includeHeaders=true` to include message headers in each summary.
- `DELETE /api/v1/inbox`: clear the inbox.
- `GET /api/v1/events`: server-sent event stream with `message-added`, `message-updated`, `message-deleted`, and `inbox-cleared` events. Each event carries a monotonically increasing `revision`.
- `GET /api/v1/message/{id}`: message metadata, body URLs, attachment metadata, and unsubscribe metadata.
- `GET /api/v1/message/{id}?markViewed=true`: fetch metadata and mark the message viewed.
- `PATCH /api/v1/message/{id}` with `{"viewed":true}` or `{"viewed":false}`: set read/unread state.
- `DELETE /api/v1/message/{id}`: delete a message.
- `GET /api/v1/message/{id}/body/{text|html|raw}`: return a body part using its native content type. Add `download=1` to get it as a file download.
- `GET /api/v1/message/{id}/body/{text|html|raw}?format=json`: return a body part wrapped as JSON.
- `GET /api/v1/message/{id}/attachment/{index}`: return attachment bytes using the attachment content type.
- `GET /api/v1/message/{id}/attachment/{index}?format=json`: return attachment metadata plus `bodyBase64`.
