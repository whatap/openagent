FROM golang:1.24-alpine AS builder
ARG VERSION=""
ARG COMMIT_HASH=""

WORKDIR /app

COPY go.mod go.sum ./
COPY gointernal/ ./gointernal/

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=${VERSION} -X main.commitHash=${COMMIT_HASH}" -o openagent

FROM alpine:3.19

WORKDIR /app
RUN apk --no-cache add ca-certificates bash curl

COPY --from=builder /app/openagent /app/openagent
COPY --from=builder /app/scrape_config.yaml /app/scrape_config.yaml
COPY --from=builder /app/whatap.conf /app/whatap.conf

RUN mkdir -p /app/logs && \
    chmod 777 /app/whatap.conf && \
    chmod 777 /app/logs
ENV WHATAP_HOME=/app

ENTRYPOINT ["/app/openagent"]
CMD ["foreground"]
