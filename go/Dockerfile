# Build — context = project root
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go/go.mod go/go.sum ./
RUN go mod download
COPY go/ .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o arteci-go .

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/arteci-go /arteci-go
COPY ui/ /ui/
EXPOSE 3001
ENTRYPOINT ["/arteci-go"]
