# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

# DEV_LICENCE_PUBKEY is empty for every real build, so the binary trusts
# only the vendor key. `make dev-licence` sets it for the local dev rig
# alone (pbx-call-placement design D8), and `make images-push` refuses to
# publish while it is set.
ARG DEV_LICENCE_PUBKEY=""
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X atsap-api/internal/licensing/domain.developmentPublicKeyHex=${DEV_LICENCE_PUBKEY}" \
    -o /out/atsap-api ./cmd/atsap-api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/atsap-api /usr/local/bin/atsap-api
# postgres.MigrateUp is called with the relative path "migrations" —
# WORKDIR here must match api/'s own layout (mise run run's cwd) so the
# same relative path resolves in both contexts with no code branching.
COPY migrations ./migrations

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/atsap-api"]
