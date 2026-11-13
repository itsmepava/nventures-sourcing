# Optional container build for future single-instance hosting (see
# docs/deploy/GOOGLE-CLOUD.md). Not built or tested in the local release.
# Do NOT run this on Cloud Run with SQLite: its filesystem is ephemeral.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
ARG VERSION=0.4.0-alpha.1
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/nventures ./cmd/nventures

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/nventures /nventures
ENV NV_DATA_DIR=/data NV_HOST=0.0.0.0 NV_PORT=8765 NV_NO_BROWSER=1 NV_LOG_JSON=1
# NV_ALLOW_REMOTE=1 and NV_BASE_URL must be provided at runtime; the app
# refuses a non-loopback bind without both. Users sign in with accounts.
VOLUME ["/data"]
EXPOSE 8765
ENTRYPOINT ["/nventures", "serve", "--env", ""]
