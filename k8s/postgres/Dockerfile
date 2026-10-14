FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -o /notes-api main.go

FROM alpine:3.22

RUN adduser -D -u 10001 appuser

COPY --from=builder /notes-api /notes-api

USER appuser
EXPOSE 8080

ENTRYPOINT ["/notes-api"]