#!/usr/bin/env python3
"""
Simple Metrics Server for OpenAgent Testing

This FastAPI application simulates azure-metrics-exporter functionality
to test OpenAgent's URL parameter support as specified in the PRD.

Based on: /Users/jaeyoung/work/go_project/openagent/prd/cloud_monitoring_prd.md
"""

from fastapi import FastAPI, Query, HTTPException
from fastapi.responses import PlainTextResponse
import time
import random
import uvicorn
from typing import Optional, List

app = FastAPI(
    title="OpenAgent Test Metrics Server",
    description="FastAPI server that simulates azure-metrics-exporter for OpenAgent parameter testing",
    version="1.0.0"
)

# Global metrics for simulated data
server_start_time = int(time.time())

def generate_prometheus_metrics(subscription: Optional[str] = None, 
                               target: Optional[str] = None,
                               metric: Optional[str] = None,
                               interval: Optional[str] = None,
                               aggregation: Optional[str] = None) -> str:
    """Generate Prometheus-format metrics based on parameters"""
    
    timestamp = int(time.time())
    metrics_lines = []
    
    # Add help and type headers
    metrics_lines.extend([
        "# HELP up Server status (1=up, 0=down)",
        "# TYPE up gauge",
        f"up 1 {timestamp}",
        "",
        "# HELP server_start_time Server start timestamp",
        "# TYPE server_start_time gauge", 
        f"server_start_time {server_start_time} {timestamp}",
        ""
    ])
    
    # Basic system metrics (always included)
    metrics_lines.extend([
        "# HELP system_cpu_usage CPU usage percentage",
        "# TYPE system_cpu_usage gauge",
        f"system_cpu_usage {random.uniform(10.0, 90.0):.2f} {timestamp}",
        "",
        "# HELP system_memory_used_bytes Memory usage in bytes", 
        "# TYPE system_memory_used_bytes gauge",
        f"system_memory_used_bytes {random.randint(1000000000, 8000000000)} {timestamp}",
        "",
        "# HELP http_requests_total HTTP requests counter",
        "# TYPE http_requests_total counter",
        f'http_requests_total{{method="GET",status="200"}} {random.randint(100, 1000)} {timestamp}',
        f'http_requests_total{{method="POST",status="200"}} {random.randint(50, 500)} {timestamp}',
        f'http_requests_total{{method="GET",status="404"}} {random.randint(1, 50)} {timestamp}',
        ""
    ])
    
    # Parameter-based metrics (simulating Azure exporter behavior)
    if subscription and target and metric:
        # Extract resource type from target (similar to Azure resource paths)
        resource_type = "unknown"
        if "Microsoft.Sql/managedInstances" in target:
            resource_type = "sql_managed_instance"
        elif "Microsoft.Compute/virtualMachines" in target:
            resource_type = "virtual_machine"
        elif "Microsoft.Storage/storageAccounts" in target:
            resource_type = "storage_account"
        
        # Process multiple metrics
        metric_names = metric.split(",") if metric else []
        
        for metric_name in metric_names:
            metric_name = metric_name.strip()
            
            if metric_name == "avg_cpu_percent":
                metrics_lines.extend([
                    "# HELP azure_sql_avg_cpu_percent Average CPU percentage from Azure API",
                    "# TYPE azure_sql_avg_cpu_percent gauge",
                    f'azure_sql_avg_cpu_percent{{subscription="{subscription}",resource_type="{resource_type}",aggregation="{aggregation}",interval="{interval}"}} {random.uniform(20.0, 80.0):.2f} {timestamp}',
                    ""
                ])
            elif metric_name == "virtual_core_count":
                metrics_lines.extend([
                    "# HELP azure_sql_virtual_core_count Virtual core count from Azure API",
                    "# TYPE azure_sql_virtual_core_count gauge", 
                    f'azure_sql_virtual_core_count{{subscription="{subscription}",resource_type="{resource_type}",aggregation="{aggregation}",interval="{interval}"}} {random.randint(2, 16)} {timestamp}',
                    ""
                ])
            elif metric_name == "memory_usage_percent":
                metrics_lines.extend([
                    "# HELP azure_sql_memory_usage_percent Memory usage percentage from Azure API",
                    "# TYPE azure_sql_memory_usage_percent gauge",
                    f'azure_sql_memory_usage_percent{{subscription="{subscription}",resource_type="{resource_type}",aggregation="{aggregation}",interval="{interval}"}} {random.uniform(40.0, 85.0):.2f} {timestamp}',
                    ""
                ])
            elif "CPU" in metric_name or "cpu" in metric_name.lower():
                # Generic CPU metrics for VMs
                metrics_lines.extend([
                    f"# HELP azure_vm_cpu_percent {metric_name} from Azure API",
                    f"# TYPE azure_vm_cpu_percent gauge",
                    f'azure_vm_cpu_percent{{subscription="{subscription}",resource_type="{resource_type}",metric_name="{metric_name}",aggregation="{aggregation}",interval="{interval}"}} {random.uniform(15.0, 75.0):.2f} {timestamp}',
                    ""
                ])
            else:
                # Generic unknown metrics
                metrics_lines.extend([
                    f"# HELP azure_unknown_metric Unknown metric {metric_name} from Azure API",
                    f"# TYPE azure_unknown_metric gauge",
                    f'azure_unknown_metric{{subscription="{subscription}",resource_type="{resource_type}",metric_name="{metric_name}",aggregation="{aggregation}",interval="{interval}"}} {random.uniform(0, 100):.2f} {timestamp}',
                    ""
                ])
        
        # Add exporter metadata
        metrics_lines.extend([
            "# HELP azure_exporter_scrape_duration_seconds Time spent scraping Azure API",
            "# TYPE azure_exporter_scrape_duration_seconds gauge",
            f'azure_exporter_scrape_duration_seconds{{subscription="{subscription}"}} {random.uniform(0.1, 2.0):.3f} {timestamp}',
            "",
            "# HELP azure_exporter_scrape_success Whether the Azure API scrape was successful",
            "# TYPE azure_exporter_scrape_success gauge",
            f'azure_exporter_scrape_success{{subscription="{subscription}"}} 1 {timestamp}',
            ""
        ])
    
    return "\n".join(metrics_lines)

@app.get("/", response_class=PlainTextResponse)
async def root():
    """Welcome message"""
    return """OpenAgent Test Metrics Server (FastAPI)

This server simulates azure-metrics-exporter functionality for testing OpenAgent's URL parameter support.

Available endpoints:
- GET /metrics - Prometheus format metrics (with optional URL parameters)
- GET /probe/metrics/resource - Azure-style metrics endpoint (requires parameters)
- GET /health - Health check
- GET / - This welcome message

Example with parameters:
/probe/metrics/resource?subscription=test-sub&target=Microsoft.Sql/test&metric=avg_cpu_percent,virtual_core_count&interval=PT1M&aggregation=average
"""

@app.get("/health", response_class=PlainTextResponse)
async def health():
    """Health check endpoint"""
    return "OK"

@app.get("/metrics", response_class=PlainTextResponse)
async def metrics():
    """Standard Prometheus metrics endpoint (no parameters required)"""
    return generate_prometheus_metrics()

@app.get("/probe/metrics/resource", response_class=PlainTextResponse) 
async def azure_metrics(
    subscription: Optional[str] = Query(None, description="Azure subscription ID"),
    target: Optional[str] = Query(None, description="Azure resource target path"), 
    metric: Optional[str] = Query(None, description="Comma-separated metric names"),
    interval: Optional[str] = Query("PT1M", description="Time interval (e.g., PT1M, PT5M)"),
    aggregation: Optional[str] = Query("average", description="Aggregation method"),
    name: Optional[str] = Query(None, description="Custom metric name"),
    metricNamespace: Optional[str] = Query(None, description="Azure metric namespace")
):
    """
    Azure-style metrics endpoint that requires URL parameters
    Simulates the behavior described in the PRD
    """
    
    # Log the request for debugging
    print(f"Request received - subscription: {subscription}, target: {target}, metric: {metric}, interval: {interval}, aggregation: {aggregation}")
    
    # Return error if required parameters are missing
    if not subscription or not target or not metric:
        error_timestamp = int(time.time())
        error_response = f"""# HELP azure_exporter_error Error in Azure exporter
# TYPE azure_exporter_error gauge
azure_exporter_error{{reason="missing_required_parameters",subscription="{subscription or 'missing'}",target_provided="{bool(target)}",metric_provided="{bool(metric)}"}} 1 {error_timestamp}

# HELP azure_exporter_request_info Information about the request
# TYPE azure_exporter_request_info gauge  
azure_exporter_request_info{{subscription="{subscription or 'none'}",has_target="{bool(target)}",has_metric="{bool(metric)}",interval="{interval}",aggregation="{aggregation}"}} 0 {error_timestamp}
"""
        return error_response
    
    return generate_prometheus_metrics(subscription, target, metric, interval, aggregation)

@app.get("/debug/params")
async def debug_params(
    subscription: Optional[str] = Query(None),
    target: Optional[str] = Query(None),
    metric: Optional[str] = Query(None),
    interval: Optional[str] = Query(None),
    aggregation: Optional[str] = Query(None)
):
    """Debug endpoint to see what parameters were received"""
    return {
        "received_parameters": {
            "subscription": subscription,
            "target": target, 
            "metric": metric,
            "interval": interval,
            "aggregation": aggregation
        },
        "parameter_count": sum(1 for p in [subscription, target, metric, interval, aggregation] if p is not None),
        "required_params_present": all([subscription, target, metric]),
        "timestamp": int(time.time())
    }

if __name__ == "__main__":
    print("Starting OpenAgent Test Metrics Server (FastAPI)")
    print("=" * 50)
    print("Available endpoints:")
    print("  - GET / (Welcome message)")
    print("  - GET /health (Health check)")
    print("  - GET /metrics (Standard Prometheus metrics)")
    print("  - GET /probe/metrics/resource (Azure-style with parameters)")
    print("  - GET /debug/params (Debug parameter parsing)")
    print("")
    print("Test with curl:")
    print('curl "http://localhost:9090/probe/metrics/resource?subscription=test&target=Microsoft.Sql/test&metric=avg_cpu_percent"')
    print("")
    print("Starting server on http://localhost:9090")
    print("=" * 50)
    
    uvicorn.run(
        "simple_metrics_server:app",
        host="0.0.0.0",
        port=9090,
        reload=False,
        log_level="info"
    )