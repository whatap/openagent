# Docker 이미지 빌드 가이드

이 문서는 OpenAgent의 Docker 이미지를 빌드하고 레지스트리에 푸시하는 방법을 설명합니다.

## 사전 요구 사항

- 로컬 머신에 Docker 설치
- (선택 사항) Docker 이미지 레지스트리 접근 권한

## 기본 Docker 이미지 빌드

1. 저장소 클론:
   ```bash
   git clone <repository-url>
   cd openagent
   ```

2. Docker 이미지 빌드:
   ```bash
   docker build -t openagent:latest .
   ```

3. (선택 사항) 이미지를 컨테이너 레지스트리에 푸시:
   ```bash
   docker tag openagent:latest <registry>/<username>/openagent:latest
   docker push <registry>/<username>/openagent:latest
   ```

   비공개 레지스트리에 푸시하는 경우, deployment.yaml 파일의 이미지 참조를 업데이트하고 이미지를 가져오기 위한 Kubernetes 시크릿을 생성해야 합니다.

## build-docker.sh 스크립트 사용 (권장)

OpenAgent는 다양한 아키텍처에 대한 이미지 빌드를 자동화하는 `build-docker.sh` 스크립트를 제공합니다.

```bash
./build-docker.sh --tag <TAG> --registry <REGISTRY> [--push] [--arch <ARCH>] [--version <VERSION>] [--commit <HASH>]
```

### 옵션

- `--tag`, `-t`: 이미지 태그 설정 (기본값: latest)
- `--registry`, `-r`: 레지스트리 설정 (예: docker.io/username)
- `--push`, `-p`: 이미지를 레지스트리에 푸시
- `--arch`, `-a`: 대상 아키텍처 설정: amd64, arm64, 또는 all (기본값: all)
- `--version`, `-v`: 에이전트 버전 설정 (기본값: 태그와 동일)
- `--commit`, `-c`: 커밋 해시 설정 (기본값: 현재 git 커밋)

### 예시

```bash
# AMD64 아키텍처용 이미지 빌드
./build-docker.sh --tag v1.0.0 --arch amd64

# 모든 아키텍처용 이미지 빌드 및 레지스트리에 푸시
./build-docker.sh --tag v1.0.0 --registry whatap --push

# 특정 버전 및 커밋 해시로 이미지 빌드
./build-docker.sh --tag latest --version 1.2.3 --commit abc123
```

## 멀티 아키텍처 빌드

`build-docker.sh` 스크립트는 Docker Buildx를 사용하여 여러 아키텍처(AMD64 및 ARM64)에 대한 이미지를 빌드합니다. 이를 통해 다양한 하드웨어 플랫폼에서 OpenAgent를 실행할 수 있습니다.

멀티 아키텍처 빌드를 사용하려면 Docker Buildx가 설치되어 있어야 합니다. 대부분의 최신 Docker 설치에는 Buildx가 포함되어 있습니다.