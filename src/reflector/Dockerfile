FROM docker.io/library/golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /reflector ./cmd/reflector

FROM docker.io/library/alpine:3.21
RUN addgroup -S app && adduser -S app -G app
USER app
WORKDIR /app
COPY --from=builder /reflector /app/reflector
EXPOSE 8080
ENTRYPOINT ["/app/reflector"]
