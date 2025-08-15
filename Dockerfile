FROM golang:1.24.4 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bot ./cmd/bot

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=builder /bot /app/bot
ENV TZ=Europe/Bucharest
ENTRYPOINT ["/app/bot"]
