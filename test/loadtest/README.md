# OpenAgent Load Testing

This directory contains load testing tools for the OpenAgent.

## Available Load Tests

1. **Single User Load Test** (`promax_load_test.go`): Simulates a single user sending 1000+ metrics with high cardinality.
2. **Multi-User Load Test** (`multi_user_load.go`): Simulates 100 concurrent users, each sending 1000 metrics with high cardinality every 30 seconds.

## Running the Load Tests

### Prerequisites

Before running the load tests, make sure you have the following environment variables set:

```bash
export WHATAP_LICENSE=your_license_key
export WHATAP_HOST=your_host
export WHATAP_PORT=your_port
```

### Running the Single User Load Test

```bash
cd /Users/jaeyoung/work/go_project/openagent
go run test/loadtest/promax_load_test.go
```

This will simulate a single user sending 1000+ metrics with high cardinality every 10 seconds.

### Running the Multi-User Load Test

```bash
cd /Users/jaeyoung/work/go_project/openagent
go run test/loadtest/multi_user_load.go
```

This will simulate 100 concurrent users, each sending 1000 metrics with high cardinality every 30 seconds.

## Configuration

You can modify the following constants in the `multi_user_load.go` file to adjust the load test parameters:

```go
const (
    // Configuration constants
    NUM_USERS           = 100  // Number of simulated users
    METRICS_PER_USER    = 1000 // Number of metrics per user
    SAMPLING_PERIOD_SEC = 30   // Sampling period in seconds
)
```

## Load Test Scenario

The multi-user load test simulates the following scenario:

- 100 concurrent users
- Each user sends 1000 metrics with high cardinality
- Sampling period is 30 seconds
- Each metric has a unique user ID label to differentiate between users
- Metrics are sent to the Whatap server using the secure communication protocol

This scenario is designed to test the performance and scalability of the OpenAgent and the Whatap server under high load conditions.

## Expected Data Volume

Based on the load test parameters, the expected data volume is:

- **Data points per sampling period**: 100 users × 1000 metrics = 100,000 data points
- **Data points per minute**: 100,000 × (60/30) = 200,000 data points
- **Data points per hour**: 200,000 × 60 = 12,000,000 data points
- **Data points per day**: 12,000,000 × 24 = 288,000,000 data points

Assuming each data point is approximately 18 bytes (time | HASH | metric value), the expected data volume is:

- **Data volume per sampling period**: 100,000 × 18 bytes = 1.8 MB
- **Data volume per minute**: 1.8 MB × (60/30) = 3.6 MB
- **Data volume per hour**: 3.6 MB × 60 = 216 MB
- **Data volume per day**: 216 MB × 24 = 5.2 GB

## Monitoring the Load Test

During the load test, you can monitor the following:

1. CPU and memory usage on the machine running the load test
2. Network traffic between the load test machine and the Whatap server
3. CPU and memory usage on the Whatap server
4. Disk I/O on the Whatap server
5. Response times and error rates from the Whatap server

This will help identify any bottlenecks in the system and determine if the Whatap server can handle the expected load.
