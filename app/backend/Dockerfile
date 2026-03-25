FROM golang:1.22-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/backend ./cmd/server

FROM alpine:3.20

RUN adduser -D -u 10001 appuser

WORKDIR /app

COPY --from=build /bin/backend /app/backend
COPY --from=build /src/migrations /app/migrations

USER appuser

EXPOSE 8081

ENTRYPOINT ["/app/backend"]
