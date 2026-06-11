FROM golang:1.26 AS builder

WORKDIR /src

COPY go.mod go.sum ./
COPY third_party/acp-go/go.mod third_party/acp-go/go.sum ./third_party/acp-go/
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agenthub-gateway ./main.go

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/agenthub-gateway /usr/local/bin/agenthub-gateway

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/agenthub-gateway"]
CMD ["-config", "/etc/agenthub-gateway/config.yaml"]
