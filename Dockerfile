# Build stage: static binary, no CGO (modernc.org/sqlite is pure Go).
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/carrierpigeon ./cmd/carrierpigeon

# Runtime stage: distroless keeps the image tiny and the attack surface
# minimal. The bot only makes outbound connections, so no ports.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/carrierpigeon /carrierpigeon
VOLUME /data
ENTRYPOINT ["/carrierpigeon"]
