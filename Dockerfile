FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/mirage ./cmd/mirage

FROM alpine:3

LABEL org.opencontainers.image.title="Mirage"
LABEL org.opencontainers.image.description="Mirage is a local email capture service for testing SMTP and Mailgun-compatible integrations."
LABEL org.opencontainers.image.source="https://github.com/leihog/mirage"
LABEL org.opencontainers.image.licenses="MIT"

RUN addgroup -S mirage && adduser -S -G mirage mirage
COPY --from=build /out/mirage /usr/local/bin/mirage
USER mirage

EXPOSE 8025 1025
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:8025/healthz || exit 1

ENTRYPOINT ["mirage"]
CMD ["-http-addr", ":8025", "-smtp-addr", ":1025"]
