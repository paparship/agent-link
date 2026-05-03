FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY vendor vendor
COPY go.mod go.sum ./
COPY . .
RUN CGO_ENABLED=0 go build -mod=vendor -o /server ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /server /server
EXPOSE 8080
CMD ["/server"]
