FROM golang:1.26-alpine
WORKDIR /app
COPY go.mod ./
COPY . .
RUN go mod tidy
CMD ["go", "test", "-v", "-count=1", "./tests/..."]
