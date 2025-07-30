#!/usr/bin/env bash
set -euo pipefail

# Display usage information
function show_usage {
  echo "❗ 사용법: ./build.sh <VERSION> [<ARCH>] [<REGISTRY>]"
  echo "  <VERSION>: 빌드할 버전 (예: 1.0.0)"
  echo "  <ARCH>: 빌드할 아키텍처 (옵션: amd64, arm64, all) [기본값: all]"
  echo "  <REGISTRY>: 사용할 레지스트리 (기본값: public.ecr.aws/whatap)"
  echo "예: ./build.sh 1.0.0 arm64"
  echo "    ./build.sh 1.0.0 all docker.io/myuser"
}

# Check for help option first
if [ $# -eq 0 ] || [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
  show_usage
  exit 0
fi

VERSION=$1
ARCH=${2:-all}  # Default to 'all' if not specified
REGISTRY=${3:-public.ecr.aws/whatap}  # Default registry

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
    go build -ldflags "-X main.version=${VERSION} -X main.commitHash=${BUILD_TIME}" \
    -o openagent main.go

# Use alpine as base image to package the openagent binary
FROM alpine:3.19
WORKDIR /app
RUN apk --no-cache add ca-certificates bash curl

# Copy the binary and configuration files
COPY --from=builder /workspace/openagent /app/openagent
COPY scrape_config.yaml /app/scrape_config.yaml
COPY whatap.conf /app/whatap.conf

RUN mkdir -p /app/logs
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

echo "✅ 빌드 및 푸시 완료: $ARCH_MSG"

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
    local binary_path=$2
    local s3_arch_name=$3
    
    echo "📦 Extracting ${arch} binary from Docker image..."
    
    # Create temporary directory for binary extraction
    local temp_dir=$(mktemp -d)
    local binary_name="openagent-${arch}"
    
    # Extract binary from Docker image
    if ! docker run --rm --platform "linux/${arch}" -v "${temp_dir}:/output" "${IMG}" sh -c "cp /app/openagent /output/${binary_name}"; then
        echo "❌ Failed to extract ${arch} binary from Docker image"
        rm -rf "${temp_dir}"
        return 1
    fi
    
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
    echo "🤔 S3에 바이너리를 업로드하시겠습니까?"
    echo "   This will extract binaries from the Docker images and upload them to S3"
    read -p "   Upload to S3? (y/N): " -n 1 -r
    echo
    
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "⏭️  S3 업로드를 건너뜁니다."
        return 0
    fi
    
    echo "🚀 Starting S3 upload process..."
    
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
            extract_and_upload_binary "amd64" "${temp_dir}/openagent-amd64" "amd"
            ;;
        arm64)
            extract_and_upload_binary "arm64" "${temp_dir}/openagent-arm64" "arm"
            ;;
        all)
            echo "📦 Uploading both AMD64 and ARM64 binaries..."
            if extract_and_upload_binary "amd64" "${temp_dir}/openagent-amd64" "amd"; then
                extract_and_upload_binary "arm64" "${temp_dir}/openagent-arm64" "arm"
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
