# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/atsap-api ./cmd/atsap-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/atsap-api /usr/local/bin/atsap-api

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/atsap-api"]
