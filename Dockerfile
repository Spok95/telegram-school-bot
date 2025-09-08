FROM golang:1.24.4 AS builder
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOTOOLCHAIN=auto
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy
RUN go build -o /bot ./cmd/bot

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=builder /bot /app/bot
ENV TZ=Europe/Moscow
ENTRYPOINT ["/app/bot"]
