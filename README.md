# OpenAgent

A Go implementation of a metrics collector agent, based on the Java agent.

## Overview

The OpenAgent is designed to scrape metrics from Prometheus endpoints, process them, and send them to a server. It is a Go implementation of the Java agent, with a similar architecture and functionality.

## Architecture

The agent consists of the following components:

- **Scraper**: Responsible for scraping metrics from targets.
- **Processor**: Processes the scraped metrics and converts them to the OpenMx format.
- **Sender**: Sends the processed metrics to the server.
- **Config Manager**: Manages the configuration of the agent.
- **HTTP Client**: Makes HTTP requests to scrape metrics from targets.
- **Converter**: Converts Prometheus metrics to OpenMx format.

## Directory Structure

```
openagent/
├── cmd/
│   └── agent/
│       └── main.go       # Main application entry point
├── pkg/
│   ├── client/           # HTTP client for making requests
│   ├── config/           # Configuration management
│   ├── converter/        # Converter for Prometheus metrics
│   ├── model/            # Data models
│   ├── processor/        # Processor for scraped metrics
│   ├── scraper/          # Scraper for metrics
│   └── sender/           # Sender for processed metrics
├── go.mod                # Go module definition
└── README.md             # This file
```

## Building and Running

### Prerequisites

- Go 1.16 or later

### Building

```bash
# Download dependencies
go mod tidy

# Build the agent
go build -o openagent ./cmd/agent
```

### Running

```bash
# Set the WHATAP_HOME environment variable (optional)
export WHATAP_HOME=/path/to/whatap/home

# Run the agent
./openagent
```

## Configuration

The agent is configured using a YAML file located at `$WHATAP_HOME/scrape_config.yaml`. The file should have the following structure:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: prometheus
    static_config:
      targets:
        - localhost:9090
      filter:
        enabled: true
        whitelist:
          - http_requests_total
          - http_requests_duration_seconds
```

## License

This project is licensed under the MIT License - see the LICENSE file for details.
