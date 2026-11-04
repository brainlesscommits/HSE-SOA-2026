FROM golang:1.26-alpine AS build
RUN apk add --no-cache protobuf-dev
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12 \
 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
WORKDIR /src
COPY go.mod go.sum ./
RUN for i in 1 2 3 4 5; do go mod download && exit 0; sleep $((i * 2)); done; exit 1
COPY . .
RUN rm -rf gen && mkdir -p gen && \
    protoc -I proto -I /usr/include \
      --go_out=gen --go_opt=paths=source_relative \
      --go-grpc_out=gen --go-grpc_opt=paths=source_relative \
      proto/flight/v1/flight.proto
ARG SERVICE
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/app /usr/local/bin/app
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/app"]
