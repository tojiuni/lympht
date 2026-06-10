FROM golang:1.26 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o lympht ./cmd/lympht/

FROM scratch
COPY --from=builder /app/lympht /lympht
ENTRYPOINT ["/lympht"]
