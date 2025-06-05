FROM golang:1.24-alpine AS builder-base

# Define build arguments for version information
ARG VERSION=""
ARG COMMIT_HASH=""

WORKDIR /app

# Copy go.mod, go.sum, and gointernal directory
COPY go.mod go.sum ./
COPY gointernal/ ./gointernal/

# Download dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build for AMD64
FROM builder-base AS builder-amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=${VERSION} -X main.commitHash=${COMMIT_HASH}" -o openagent

# Build for ARM64
FROM builder-base AS builder-arm64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-X main.version=${VERSION} -X main.commitHash=${COMMIT_HASH}" -o openagent

# Create a minimal runtime image for AMD64
FROM alpine:3.19 AS runtime-amd64
WORKDIR /app
RUN apk --no-cache add ca-certificates
COPY --from=builder-amd64 /app/openagent /app/openagent
COPY --from=builder-amd64 /app/scrape_config.yaml /app/scrape_config.yaml
COPY --from=builder-amd64 /app/whatap.conf /app/whatap.conf
RUN mkdir -p /app/logs
ENV WHATAP_HOME=/app
ENTRYPOINT ["/app/openagent"]
CMD ["foreground"]

# Create a minimal runtime image for ARM64
FROM alpine:3.19 AS runtime-arm64
WORKDIR /app
RUN apk --no-cache add ca-certificates
COPY --from=builder-arm64 /app/openagent /app/openagent
COPY --from=builder-arm64 /app/scrape_config.yaml /app/scrape_config.yaml
COPY --from=builder-arm64 /app/whatap.conf /app/whatap.conf
RUN mkdir -p /app/logs
ENV WHATAP_HOME=/app
ENTRYPOINT ["/app/openagent"]
CMD ["foreground"]

# Use the appropriate runtime image based on the build argument
FROM runtime-${TARGETARCH:-amd64}
