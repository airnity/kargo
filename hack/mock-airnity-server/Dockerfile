FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY main.go .
RUN go mod init mock-airnity-server && \
    go build -o mock-airnity-server main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/mock-airnity-server .

EXPOSE 8080

CMD ["./mock-airnity-server"]