# Builds all Lumen Go services. Select one with --target (api, vault, capture, metadata).

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/... \
 && mkdir -p /data

FROM gcr.io/distroless/static-debian12:nonroot AS api
COPY --from=build /out/api /usr/local/bin/lumen-api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/lumen-api"]

FROM gcr.io/distroless/static-debian12:nonroot AS vault
COPY --from=build /out/vault /usr/local/bin/lumen-vault
COPY --from=build --chown=65532:65532 /data /data
VOLUME /data
EXPOSE 9000
ENTRYPOINT ["/usr/local/bin/lumen-vault"]

FROM chromedp/headless-shell:latest AS capture
COPY --from=build /out/capture /usr/local/bin/lumen-capture
ENV CHROME_PATH=/headless-shell/headless-shell
EXPOSE 9100
ENTRYPOINT ["/usr/local/bin/lumen-capture"]

# ExifTool (Perl) in a minimal Alpine image. Runs as an unprivileged user with
# no network egress; see docker-compose.yml.
FROM alpine:3 AS metadata
RUN apk add --no-cache exiftool && adduser -D -H -u 65532 lumen
COPY --from=build /out/metadata /usr/local/bin/lumen-metadata
USER 65532:65532
EXPOSE 9200
ENTRYPOINT ["/usr/local/bin/lumen-metadata"]
