# Build stage
# Runs on the build machine's own arch and cross-compiles below, so no QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
WORKDIR /app

# copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
ARG TARGETOS TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-s -w -extldflags "-static"' -o elector .

FROM gcr.io/distroless/static:nonroot
USER nonroot:nonroot
WORKDIR /app
COPY --from=builder --chown=nonroot:nonroot /app/elector /app/
ENTRYPOINT ["./elector"]
