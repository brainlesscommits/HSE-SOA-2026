FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /bin/aggregation-service ./cmd/aggregation-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=builder /bin/aggregation-service /usr/local/bin/aggregation-service
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/aggregation-service"]
