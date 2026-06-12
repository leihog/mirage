# Mirage Integration Test Harness

This folder is not a Go package. It contains Docker config, sample payloads, and scripts for running Mirage and seeding messages through the public HTTP and SMTP endpoints.

## Start Mirage

```sh
./tests/scripts/start.sh
```

Mirage will be available at <http://localhost:18025>. The SMTP listener is available at `localhost:11025`.

## Seed Messages

```sh
./tests/scripts/seed.sh
```

The script sends:

- Mailgun form message with text, HTML, a custom variable, and repeated header and tag fields.
- Form message with CC and BCC recipients.
- HTML-only form message.
- One-click unsubscribe form message. The unsubscribe URL is intentionally unreachable; clicking unsubscribe in the UI demonstrates the one-click POST flow and shows the failed request details.
- Raw `.eml` MIME message with nested multipart/alternative plus an attachment (`nested-alternative.eml`).
- Raw `.eml` Gmail-style mixed message with an HTML attachment (`gmail-style.eml`).
- Raw `.eml` related message with an inline image and an attachment (`with-files.eml`).

## Smoke Check

```sh
./tests/scripts/smoke.sh
```

This starts Mirage if needed, sends one `.eml` through SMTP, seeds messages, and verifies the `/api/v1` inbox, body, and attachment endpoints.

## Stop Mirage

```sh
./tests/scripts/stop.sh
```
