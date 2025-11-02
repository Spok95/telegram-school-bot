FROM golang:1.23 AS builder
# ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOTOOLCHAIN=auto
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Версия и коммит пробрасываем через build-args
ARG VERSION=dev
ARG COMMIT=none

RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /bot ./cmd/bot

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
COPY --from=builder /bot /app/bot
USER nonroot:nonroot
ENV TZ=Europe/Moscow
ENTRYPOINT ["/app/bot"]
