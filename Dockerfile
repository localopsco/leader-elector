# Build stage
#
# The builder tag deliberately floats on the minor version so that every rebuild
# picks up the latest Go patch release, and with it the latest stdlib security
# fixes. Go supports only the two most recent majors, so this must be bumped
# before the current line goes end-of-life.
FROM golang:1.26-alpine AS builder
WORKDIR /app

# copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags='-s -w -extldflags "-static"' -o elector .

# Runtime stage
#
# Unlike the builder, the runtime base is pinned by digest: these are the bytes
# we ship, so the base should move deliberately via a reviewed Dependabot PR
# rather than silently. Tag at time of pinning: gcr.io/distroless/static:nonroot
FROM gcr.io/distroless/static:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7
USER nonroot:nonroot
WORKDIR /app
COPY --from=builder --chown=nonroot:nonroot /app/elector /app/
ENTRYPOINT ["./elector"]
