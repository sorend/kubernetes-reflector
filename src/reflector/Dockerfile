FROM docker.io/library/golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /reflector ./cmd/reflector

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /reflector /reflector
EXPOSE 8080
ENTRYPOINT ["/reflector"]
