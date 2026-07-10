#!/usr/bin/env bash
set -euo pipefail

# Display usage information
function show_usage {
  echo "❗ 사용법: ./build.sh [OPTIONS] [<VERSION>] [<ARCH>] [<REGISTRY>]"
  echo ""
  echo "Version Management Options:"
  echo "  --from-file              버전 파일(version.properties)에서 버전 읽기"
  echo "  --increment-patch        패치 버전 증가 후 빌드 (1.0.0 -> 1.0.1)"
  echo "  --increment-minor        마이너 버전 증가 후 빌드 (1.0.0 -> 1.1.0)"
  echo "  --increment-major        메이저 버전 증가 후 빌드 (1.0.0 -> 2.0.0)"
  echo ""
  echo "Build Parameters:"
  echo "  <VERSION>: 빌드할 버전 (예: 1.0.0) - 옵션과 함께 사용 시 생략 가능"
  echo "  <ARCH>: 빌드할 아키텍처 (옵션: amd64, arm64, all) [기본값: all]"
  echo "  <REGISTRY>: 사용할 레지스트리 (기본값: public.ecr.aws/whatap)"
  echo ""
  echo "예시:"
  echo "  ./build.sh 1.0.0 arm64                    # 수동 버전 지정"
  echo "  ./build.sh --from-file                    # 버전 파일에서 읽기"
  echo "  ./build.sh --increment-patch arm64        # 패치 버전 증가 후 빌드"
  echo "  ./build.sh --increment-minor all docker.io/myuser  # 마이너 버전 증가"
}

# Version management functions
get_version_from_file() {
    if [[ -f "version.properties" ]]; then
        grep "^VERSION=" version.properties | cut -d'=' -f2
    else
        echo "1.0.0"
    fi
}

increment_and_get_version() {
    local increment_type=$1
    if [[ ! -f "version.sh" ]]; then
        echo "❌ version.sh not found. Cannot increment version."
        exit 1
    fi
    
    echo "🔄 Incrementing $increment_type version..."
    ./version.sh increment "$increment_type"
}

# Parse arguments and handle version management options
VERSION=""
VERSION_SOURCE="manual"
ARCH="all"
REGISTRY="public.ecr.aws/whatap"

# Check for help option first
if [ $# -eq 0 ] || [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
  show_usage
  exit 0
fi

# Parse version management options
case "$1" in
    --from-file)
        VERSION=$(get_version_from_file)
        VERSION_SOURCE="file"
        shift
        ;;
    --increment-patch)
        VERSION=$(increment_and_get_version "patch")
        VERSION_SOURCE="increment-patch"
        shift
        ;;
    --increment-minor)
        VERSION=$(increment_and_get_version "minor")
        VERSION_SOURCE="increment-minor"
        shift
        ;;
    --increment-major)
        VERSION=$(increment_and_get_version "major")
        VERSION_SOURCE="increment-major"
        shift
        ;;
    --*)
        echo "❗ Unknown option: $1"
        show_usage
        exit 1
        ;;
    *)
        # Traditional usage: VERSION as first argument
        VERSION=$1
        VERSION_SOURCE="manual"
        shift
        ;;
esac

# Parse remaining arguments
ARCH=${1:-all}  # Default to 'all' if not specified
REGISTRY=${2:-public.ecr.aws/whatap}  # Default registry

# Validate version
if [[ -z "$VERSION" ]]; then
    echo "❌ No version specified or found"
    show_usage
    exit 1
fi

echo "📋 Build Configuration:"
echo "   Version: $VERSION (source: $VERSION_SOURCE)"
echo "   Architecture: $ARCH"
echo "   Registry: $REGISTRY"

# Set the platforms based on the architecture parameter
case $ARCH in
  amd64)
    PLATFORMS="linux/amd64"
    ARCH_MSG="amd64"
    ;;
  arm64)
    PLATFORMS="linux/arm64"
    ARCH_MSG="arm64"
    ;;
  all)
    PLATFORMS="linux/arm64,linux/amd64"
    ARCH_MSG="all architectures (linux/arm64, linux/amd64)"
    ;;
  *)
    echo "❗ 지원하지 않는 아키텍처입니다: $ARCH"
    show_usage
    exit 1
    ;;
esac

# Set image names with the specified registry
export IMG="${REGISTRY}/open_agent:${VERSION}"
export IMG_LATEST="${REGISTRY}/open_agent:latest"

echo "🚀 Building for $ARCH_MSG"
echo "🚀 Building and pushing both tags: ${IMG} and ${IMG_LATEST}"

# Create a temporary Dockerfile.cross for multi-platform build
cat > Dockerfile.cross << 'EOF'
# Build the openagent binary
FROM --platform=${BUILDPLATFORM} golang:1.24.3 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# Copy gointernal directory first since it's referenced as a local replacement in go.mod
COPY gointernal/ gointernal/
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY open/ open/
COPY pkg/ pkg/
COPY util/ util/
COPY tools/ tools/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
ARG VERSION=dev
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -ldflags "-X main.version=${VERSION} -X main.commitHash=${BUILD_TIME} -X main.buildTime=${BUILD_TIME}" \
    -o openagent main.go

# Use alpine as base image to package the openagent binary
FROM alpine:3.19
WORKDIR /app
RUN apk --no-cache add ca-certificates bash curl

# Copy the binary and configuration files
COPY --from=builder /workspace/openagent /app/openagent
COPY scrape_config.yaml /app/scrape_config.yaml
COPY whatap.conf /app/whatap.conf

RUN mkdir -p /app/logs && \
    chmod 666 /app/whatap.conf && \
    chmod 777 /app/logs
ENV WHATAP_HOME=/app

ENTRYPOINT ["/app/openagent"]
CMD ["foreground"]
EOF

# Create or use existing buildx builder
if ! docker buildx inspect openagent-builder &>/dev/null; then
  docker buildx create --name openagent-builder
fi
docker buildx use openagent-builder

# Build with both tags in a single command
docker buildx build --push \
  --platform=${PLATFORMS} \
  --build-arg VERSION=${VERSION} \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  --tag ${IMG} \
  --tag ${IMG_LATEST} \
  -f Dockerfile.cross .

# Clean up
rm Dockerfile.cross

# Build history tracking functions
update_build_history() {
    local status=$1
    local start_time=$2
    local end_time=$3
    local duration=$((end_time - start_time))
    
    # Check if jq is available for JSON manipulation
    if ! command -v jq &> /dev/null; then
        echo "⚠️  jq not found. Build history tracking disabled."
        return 0
    fi
    
    # Create build-history.json if it doesn't exist
    if [[ ! -f "build-history.json" ]]; then
        cat > build-history.json << 'EOF'
{
  "project": "openagent",
  "created": "",
  "builds": [],
  "statistics": {
    "total_builds": 0,
    "successful_builds": 0,
    "failed_builds": 0,
    "last_successful_build": null,
    "last_failed_build": null,
    "versions": {}
  }
}
EOF
    fi
    
    # Get build metadata
    local build_id=$(jq -r '.builds | length + 1' build-history.json)
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    local git_commit=$(git rev-parse --short HEAD 2>/dev/null || echo "")
    local git_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
    local git_tag=$(git describe --tags --exact-match 2>/dev/null || echo "")
    local build_user=$(whoami)
    local build_host=$(hostname)
    
    # Create new build entry
    local new_build=$(cat << EOF
{
  "id": $build_id,
  "version": "$VERSION",
  "timestamp": "$timestamp",
  "status": "$status",
  "architecture": "$ARCH",
  "platforms": $(echo "$PLATFORMS" | jq -R 'split(",")'),
  "registry": "$REGISTRY",
  "version_source": "$VERSION_SOURCE",
  "git": {
    "commit": "$git_commit",
    "branch": "$git_branch",
    "tag": "$git_tag"
  },
  "build": {
    "user": "$build_user",
    "host": "$build_host",
    "duration": $duration,
    "docker_images": [
      "$IMG",
      "$IMG_LATEST"
    ],
    "s3_uploads": {
      "enabled": false,
      "paths": []
    }
  },
  "notes": "Build via $VERSION_SOURCE"
}
EOF
)
    
    # Update build history
    local temp_file=$(mktemp)
    jq --argjson new_build "$new_build" '
        .builds += [$new_build] |
        .statistics.total_builds += 1 |
        if $new_build.status == "success" then
            .statistics.successful_builds += 1 |
            .statistics.last_successful_build = $new_build.timestamp
        else
            .statistics.failed_builds += 1 |
            .statistics.last_failed_build = $new_build.timestamp
        end |
        .statistics.versions[$new_build.version] = (.statistics.versions[$new_build.version] // 0) + 1
    ' build-history.json > "$temp_file" && mv "$temp_file" build-history.json
    
    echo "📊 Build history updated (Build #$build_id)"
}

# Record build start time
BUILD_START_TIME=$(date +%s)

echo "✅ 빌드 및 푸시 완료: $ARCH_MSG"

# Record build completion
BUILD_END_TIME=$(date +%s)
update_build_history "success" "$BUILD_START_TIME" "$BUILD_END_TIME"

# Enhanced S3 upload function with robust error handling
function setup_aws_auth() {
    echo "🔐 Setting up AWS authentication..."
    
    # Check if tsh command exists
    if ! command -v tsh &> /dev/null; then
        echo "⚠️  tsh command not found. Skipping Teleport authentication."
        echo "💡 Assuming AWS credentials are already configured (via AWS CLI, IAM role, etc.)"
        return 0
    fi
    
    # Check if user wants to use Teleport authentication
    echo "🤔 Do you want to use Teleport authentication for AWS access?"
    echo "   Choose 'n' if you already have AWS credentials configured"
    read -p "   Use Teleport authentication? (y/N): " -n 1 -r
    echo
    
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "⏭️  Skipping Teleport authentication. Using existing AWS credentials."
        return 0
    fi
    
    # Attempt Teleport login
    echo "🔑 Attempting Teleport login..."
    if ! tsh login --proxy=teleport.whatap.io:443 teleport.whatap.io --auth=github; then
        echo "❌ Teleport login failed. You can:"
        echo "   1. Configure AWS credentials manually (aws configure)"
        echo "   2. Use IAM roles if running on EC2"
        echo "   3. Set AWS environment variables"
        read -p "   Continue without Teleport? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "🛑 S3 upload cancelled."
            return 1
        fi
        return 0
    fi
    
    # Attempt AWS IAM app login
    echo "🔗 Connecting to AWS IAM..."
    if ! tsh apps login --aws-role s3-admin aws-iam; then
        echo "⚠️  AWS IAM app login failed, but continuing with existing credentials..."
        return 0
    fi
    
    echo "✅ AWS authentication setup complete!"
    return 0
}

function extract_and_upload_binary() {
    local arch=$1
    
    local s3_arch_name=$2
    
    echo "📦 Extracting ${arch} binary from Docker image..."
    
    # Create temporary directory for binary extraction
    local temp_dir=$(mktemp -d)
    local binary_name="openagent-${arch}"
    
    # Extract binary from Docker image using docker create/cp approach
    # This avoids the "exec format error" when extracting cross-architecture binariesㅁ    echo "🔧 Creating temporary container for ${arch} binary extraction..."
    local container_id=$(docker create --platform "linux/${arch}" "${IMG}")
    
    if [[ -z "$container_id" ]]; then
        echo "❌ Failed to create temporary container for ${arch}"
        rm -rf "${temp_dir}"
        return 1
    fi
    
    # Copy binary from container to host
    if ! docker cp "${container_id}:/app/openagent" "${temp_dir}/${binary_name}"; then
        echo "❌ Failed to extract ${arch} binary from Docker image"
        docker rm "${container_id}" &>/dev/null
        rm -rf "${temp_dir}"
        return 1
    fi
    
    # Clean up temporary container
    docker rm "${container_id}" &>/dev/null
    
    # Verify binary was extracted
    if [[ ! -f "${temp_dir}/${binary_name}" ]]; then
        echo "❌ Binary not found after extraction: ${temp_dir}/${binary_name}"
        rm -rf "${temp_dir}"
        return 1
    fi
    
    echo "📤 Uploading ${arch} binary to S3..."
    
    # Upload to versioned path
    echo "📍 Uploading to: s3://repo.whatap.io/openagent/${VERSION}/${s3_arch_name}/openagent"
    if ! aws s3 cp "${temp_dir}/${binary_name}" "s3://repo.whatap.io/openagent/${VERSION}/${s3_arch_name}/openagent"; then
        echo "❌ Failed to upload to versioned S3 path"
        rm -rf "${temp_dir}"
        return 1
    fi
    
    # Upload to latest path
    echo "📍 Uploading to: s3://repo.whatap.io/openagent/latest/${s3_arch_name}/openagent"
    if ! aws s3 cp "${temp_dir}/${binary_name}" "s3://repo.whatap.io/openagent/latest/${s3_arch_name}/openagent"; then
        echo "❌ Failed to upload to latest S3 path"
        rm -rf "${temp_dir}"
        return 1
    fi
    
    # Clean up
    rm -rf "${temp_dir}"
    echo "✅ ${arch} binary uploaded successfully!"
    return 0
}

function upload_to_s3() {
    echo ""
    echo "🚀 Starting automatic S3 upload process..."
    
    # Setup AWS authentication
    if ! setup_aws_auth; then
        echo "❌ AWS authentication setup failed. Skipping S3 upload."
        return 1
    fi
    
    # Check if AWS CLI is available
    if ! command -v aws &> /dev/null; then
        echo "❌ AWS CLI not found. Please install AWS CLI first."
        echo "   Install: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"
        return 1
    fi
    
    # Test AWS credentials
    echo "🔍 Testing AWS credentials..."
    if ! aws sts get-caller-identity &> /dev/null; then
        echo "❌ AWS credentials not properly configured."
        echo "💡 Please ensure one of the following:"
        echo "   - AWS CLI configured (aws configure)"
        echo "   - IAM role attached (if running on EC2)"
        echo "   - Environment variables set (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)"
        echo "   - Teleport authentication completed successfully"
        return 1
    fi
    
    echo "✅ AWS credentials verified!"
    
    # Upload based on architecture
    case $ARCH in
        amd64)
            extract_and_upload_binary "amd64" "amd"
            ;;
        arm64)
            extract_and_upload_binary "arm64" "arm"
            ;;
        all)
            echo "📦 Uploading both AMD64 and ARM64 binaries..."
            if extract_and_upload_binary "amd64" "amd"; then
                extract_and_upload_binary "arm64" "arm"
            else
                echo "❌ AMD64 upload failed, skipping ARM64"
                return 1
            fi
            ;;
    esac
    
    echo "🎉 S3 upload completed successfully!"
    echo "📍 Uploaded to:"
    echo "   - s3://repo.whatap.io/openagent/${VERSION}/"
    echo "   - s3://repo.whatap.io/openagent/latest/"
}

# Call the S3 upload function
upload_to_s3
