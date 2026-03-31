# Stage 1: Build
FROM golang:1.26.1-alpine AS builder
WORKDIR /app
# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire project
COPY . .

# Build the Go binary
RUN go build -o chukuago ./cmd/main.go

# Stage 2: Run
FROM alpine:3.19
WORKDIR /root/
COPY --from=builder /app/chukuago .

EXPOSE 8080
CMD ["./chukuago"]
