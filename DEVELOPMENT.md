# OpenAgent 개발 가이드

이 문서는 OpenAgent의 개발자를 위한 가이드입니다. 에이전트를 빌드하고, 테스트하고, 배포하는 방법에 대한 정보를 제공합니다.

## 사전 요구사항

- Go 1.16 이상
- 쿠버네티스 환경에서 PodMonitor 및 ServiceMonitor를 사용하려면 쿠버네티스 클러스터에 대한 접근 권한이 필요합니다.
- Docker 이미지 빌드를 위한 Docker 설치
- 다중 아키텍처(AMD64, ARM64) 이미지 빌드를 위한 Docker Buildx 설치

## 빌드 방법

```bash
# 의존성 다운로드
go mod tidy

# 에이전트 빌드
go build -o openagent

# 버전 정보를 포함한 빌드 (권장)
go build -ldflags "-X main.version=1.0.0 -X main.commitHash=$(git rev-parse HEAD)" -o openagent
```

빌드 시 버전 정보를 포함하지 않은 경우, 실행 시 환경 변수 `WHATAP_AGENT_VERSION`을 설정하여 버전을 지정할 수 있습니다:

```bash
export WHATAP_AGENT_VERSION=1.0.0
```

## 실행 방법

```bash
# WHATAP_HOME 환경 변수 설정 (선택사항)
export WHATAP_HOME=/path/to/whatap/home

# WHATAP_LICENSE 환경 변수 설정 (필수)
export WHATAP_LICENSE=x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502

# WHATAP_HOST 환경 변수 설정 (필수)
export WHATAP_HOST=15.165.146.117

# WHATAP_PORT 환경 변수 설정 (필수)
export WHATAP_PORT=6600

# 쿠버네티스 환경에서 실행할 경우 KUBECONFIG 환경 변수 설정 (선택사항)
# 설정하지 않으면 in-cluster config 또는 ~/.kube/config를 사용합니다.
export KUBECONFIG=/path/to/kubeconfig

# 에이전트 실행 (수퍼바이저 모드)
./openagent

# 또는 직접 워커 모드로 실행 (일반적으로 필요하지 않음)
./openagent foreground
```

## Docker 이미지 빌드

OpenAgent는 Docker 컨테이너로 실행할 수 있습니다. 프로젝트 루트 디렉토리에 있는 Dockerfile을 사용하여 이미지를 빌드할 수 있습니다. OpenAgent는 Linux AMD64와 ARM64 아키텍처를 모두 지원합니다.

```bash
# Docker 이미지 빌드 (기본적으로 현재 시스템 아키텍처용으로 빌드)
docker build -t openagent:latest .

# 버전 정보를 포함한 Docker 이미지 빌드 (권장)
docker build --build-arg VERSION=1.0.0 --build-arg COMMIT_HASH=$(git rev-parse HEAD) -t openagent:1.0.0 .

# (선택사항) 이미지를 컨테이너 레지스트리에 푸시
docker tag openagent:latest <registry>/<username>/openagent:latest
docker push <registry>/<username>/openagent:latest
```

Docker 컨테이너 실행 시 환경 변수를 통해 버전을 설정할 수도 있습니다:

```bash
docker run -e WHATAP_LICENSE=<license> -e WHATAP_HOST=<host> -e WHATAP_PORT=<port> -e WHATAP_AGENT_VERSION=1.0.0 openagent:latest
```

또는 제공된 스크립트를 사용하여 더 쉽게 빌드하고 푸시할 수 있습니다. 이 스크립트는 Docker Buildx를 사용하여 다중 아키텍처 이미지를 빌드할 수 있습니다:

```bash
# 기본 빌드 (모든 아키텍처용 openagent:latest)
./build-docker.sh

# 특정 아키텍처(AMD64)로 빌드
./build-docker.sh --arch amd64

# 특정 아키텍처(ARM64)로 빌드
./build-docker.sh --arch arm64

# 모든 아키텍처(AMD64, ARM64)로 빌드
./build-docker.sh --arch all

# 특정 태그로 빌드
./build-docker.sh --tag v1.0.0

# 특정 버전 정보로 빌드
./build-docker.sh --version 1.0.0 --commit abc1234

# 레지스트리 지정 및 푸시 (다중 아키텍처 이미지 푸시 시 필요)
./build-docker.sh --registry docker.io/username --tag v1.0.0 --push

# 버전 정보를 포함한 빌드 및 푸시
./build-docker.sh --registry docker.io/username --tag v1.0.0 --version 1.0.0 --commit $(git rev-parse --short HEAD) --push

# 도움말 보기
./build-docker.sh --help
```

> **참고**: 다중 아키텍처 이미지를 빌드하려면 Docker Buildx가 필요합니다. 다중 아키텍처 이미지를 로컬에서 사용하려면 이미지를 레지스트리에 푸시해야 합니다.

## 샘플 데이터 전송

테스트 목적으로 샘플 데이터를 와탭 서버로 직접 전송할 수 있습니다. 샘플 코드는 `test/integration` 디렉토리에 있습니다:

```bash
# 환경 변수 설정 (필수)
export WHATAP_LICENSE=x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502
export WHATAP_HOST=15.165.146.117
export WHATAP_PORT=6600

# 샘플 데이터 전송 실행
go run test/integration/direct_sample_sender.go  # 기본 샘플 메트릭 전송
go run test/integration/promax.go                # 다양한 메트릭 및 레이블 전송
go run test/integration/tag_data_sender.go       # 태그 데이터 전송
```

각 샘플 프로그램의 기능:

1. **direct_sample_sender.go**: 기본적인 메트릭 데이터와 도움말 정보를 전송합니다.
2. **promax.go**: 다양한 메트릭과 레이블을 포함한 데이터를 주기적으로 전송합니다.
3. **tag_data_sender.go**: 태그 데이터를 전송하는 예제입니다.

## Kubernetes 배포 개발

OpenAgent를 Kubernetes 클러스터에 배포하기 위한 매니페스트 파일이 `k8s` 디렉토리에 제공됩니다. 자세한 배포 방법은 `k8s/README.md` 파일을 참조하세요.

배포하려면 다음 단계를 따르세요:

1. WHATAP 자격 증명을 위한 Secret 생성
   ```bash
   # 제공된 스크립트 사용
   ./k8s/create-whatap-secret.sh <WHATAP_LICENSE> <WHATAP_HOST> <WHATAP_PORT>

   # 또는 직접 kubectl 명령어 사용
   export WHATAP_LICENSE=<WHATAP_LICENSE>
   export WHATAP_HOST=<WHATAP_HOST>
   export WHATAP_PORT=<WHATAP_PORT>
   kubectl create secret generic whatap-credentials --from-literal=license=$WHATAP_LICENSE --from-literal=host=$WHATAP_HOST --from-literal=port=$WHATAP_PORT
   ```

2. 배포 매니페스트 적용
   ```bash
   kubectl apply -f k8s/deployment.yaml
   ```

3. 배포 상태 확인
   ```bash
   kubectl get pods -l app=whatap-open-agent
   ```
