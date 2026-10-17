# OUFT accounts service (cmd/ouft-accounts). Built by docker-compose.yml.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ouft-accounts ./cmd/ouft-accounts
COPY internal/accounts ./internal/accounts
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /ouft-accounts ./cmd/ouft-accounts

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 ouft && mkdir /pb_data && chown ouft /pb_data
COPY --from=build /ouft-accounts /usr/local/bin/ouft-accounts
USER ouft
VOLUME ["/pb_data"]
EXPOSE 8090
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s CMD wget -qO- http://127.0.0.1:8090/api/health >/dev/null || exit 1
ENTRYPOINT ["ouft-accounts", "--dir", "/pb_data"]
CMD ["serve", "--http", "0.0.0.0:8090"]
