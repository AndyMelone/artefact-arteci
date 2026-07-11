# Build
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o arteci-go .

FROM scratch
COPY --from=builder /app/arteci-go /arteci-go
EXPOSE 3001
ENTRYPOINT ["/arteci-go"]
