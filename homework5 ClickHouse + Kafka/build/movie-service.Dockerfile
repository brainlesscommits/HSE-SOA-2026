FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /bin/movie-service ./cmd/movie-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=builder /bin/movie-service /usr/local/bin/movie-service
COPY schemas/movie_event.avsc /app/schemas/movie_event.avsc
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/movie-service"]
