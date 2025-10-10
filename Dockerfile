# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /app

# copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
RUN go build -ldflags='-s -w -extldflags "-static"' -o elector .

FROM gcr.io/distroless/static:nonroot
USER nonroot:nonroot
WORKDIR /app
COPY --from=builder --chown=nonroot:nonroot /app/elector /app/
ENTRYPOINT ["./elector"]
