# OpenAgent (오픈에이전트)

프로메테우스 엔드포인트에서 메트릭을 수집하고 와탭 서버로 전송하는 Go 기반 에이전트입니다.

## 개요

OpenAgent는 프로메테우스 엔드포인트에서 메트릭을 스크래핑하고, 이를 처리하여 와탭 서버로 전송하는 역할을 합니다.

## 아키텍처

에이전트는 다음과 같은 주요 컴포넌트로 구성되어 있습니다:

- **스크래퍼(Scraper)**: 대상 시스템에서 메트릭을 수집합니다.
- **프로세서(Processor)**: 수집된 메트릭을 처리하고 OpenMx 형식으로 변환합니다.
- **센더(Sender)**: 처리된 메트릭을 와탭 서버로 전송합니다.
- **설정 관리자(Config Manager)**: 에이전트의 설정을 관리합니다.
- **HTTP 클라이언트(HTTP Client)**: 대상 시스템에 HTTP 요청을 보내 메트릭을 수집합니다.
- **변환기(Converter)**: 프로메테우스 메트릭을 OpenMx 형식으로 변환합니다.
- **쿠버네티스 클라이언트(Kubernetes Client)**: 쿠버네티스 API 서버와 통신하여 Pod, Service, Endpoint 정보를 수집합니다.

## 디렉토리 구조

```
openagent/
├── gointernal/           # 와탭 내부 라이브러리 (네트워크 통신, 보안 등)
├── logs/                 # 로그 파일 디렉토리
├── main.go               # 메인 애플리케이션 진입점
├── open/                 # 에이전트 부트스트랩 및 관리
├── pkg/
│   ├── client/           # HTTP 요청을 위한 클라이언트
│   ├── common/           # 공통 유틸리티 및 데이터 구조
│   ├── config/           # 설정 관리
│   ├── converter/        # 프로메테우스 메트릭 변환기
│   ├── k8s/              # 쿠버네티스 클라이언트 및 인포머
│   ├── model/            # 데이터 모델 (OpenMx, OpenMxHelp 등)
│   ├── processor/        # 수집된 메트릭 처리기
│   ├── scraper/          # 메트릭 스크래퍼
│   └── sender/           # 처리된 메트릭 전송기
├── scrape_config.yaml    # 스크래핑 설정 파일
├── test/
│   └── integration/      # 통합 테스트 및 샘플 코드
├── go.mod                # Go 모듈 정의
└── README.md             # 현재 파일
```

## 동작 모드

OpenAgent는 두 가지 모드로 동작합니다:

1. **수퍼바이저 모드(Supervisor Mode)**: 로컬 실행 시 기본 모드로, 워커 프로세스를 관리하고 모니터링합니다. 워커 프로세스가 종료되면 자동으로 재시작합니다.
2. **워커 모드(Worker Mode)**: "foreground" 인자와 함께 실행되며, 실제 메트릭 수집 및 전송 작업을 수행합니다. Docker 컨테이너에서는 이 모드가 기본값입니다.

일반적으로 로컬에서는 수퍼바이저 모드로 에이전트를 실행하며, 수퍼바이저가 워커 프로세스를 자동으로 관리합니다. Docker 컨테이너에서는 워커 모드가 기본값이므로 별도의 인자 없이 실행됩니다.

### 디버그 모드

OpenAgent는 whatap.conf 파일을 통해 디버그 모드를 활성화할 수 있습니다. 디버그 모드가 활성화되면 수집된 메트릭 데이터가 표준 출력(stdout)으로 출력됩니다.

디버그 모드를 활성화하려면 WHATAP_HOME 디렉토리(기본값: /app)에 whatap.conf 파일을 생성하고 다음 내용을 추가합니다:

```
debug=true
```

OpenAgent는 whatap.conf 파일의 변경 사항을 자동으로 감지하고 적용합니다. 에이전트를 재시작하지 않고도 디버그 모드를 활성화하거나 비활성화할 수 있습니다. 파일이 변경되면 5초 이내에 새로운 설정이 적용됩니다.

whatap.conf 파일은 Docker 이미지에 직접 포함되어 있습니다. 디버그 모드를 변경하려면 소스 코드의 whatap.conf 파일을 수정한 후 Docker 이미지를 다시 빌드해야 합니다.

## 설치 및 실행

OpenAgent를 설치하고 실행하는 방법에 대한 자세한 내용은 [DEVELOPMENT.md](DEVELOPMENT.md) 파일을 참조하세요.

### 필수 환경 변수

OpenAgent를 실행하려면 다음 환경 변수를 설정해야 합니다:

- `WHATAP_LICENSE`: 와탭 라이센스 키
- `WHATAP_HOST`: 와탭 서버 호스트 주소
- `WHATAP_PORT`: 와탭 서버 포트 (기본값: 6600)

선택적으로 다음 환경 변수를 설정할 수 있습니다:

- `WHATAP_HOME`: 와탭 홈 디렉토리 (기본값: 현재 디렉토리)
- `KUBECONFIG`: 쿠버네티스 설정 파일 경로 (쿠버네티스 환경에서 실행 시)

## Docker 및 Kubernetes 배포

OpenAgent는 Docker 컨테이너로 실행하거나 Kubernetes 클러스터에 배포할 수 있습니다. 

Docker 이미지 빌드 및 Kubernetes 배포에 대한 자세한 내용은 [DEVELOPMENT.md](DEVELOPMENT.md) 파일을 참조하세요.

### Kubernetes 배포

OpenAgent를 Kubernetes 클러스터에 배포하기 위한 매니페스트 파일이 `k8s` 디렉토리에 제공됩니다. 자세한 배포 방법은 `k8s/README.md` 파일을 참조하세요.

#### 디버그 모드 설정

쿠버네티스 환경에서 디버그 모드는 Docker 이미지에 포함된 whatap.conf 파일에 의해 제어됩니다. 기본적으로 디버그 모드는 활성화되어 있습니다.

디버그 모드를 변경하려면 다음 단계를 따르세요:

1. 소스 코드의 whatap.conf 파일을 수정합니다.
2. Docker 이미지를 다시 빌드합니다.
3. 새 이미지를 사용하도록 Kubernetes 배포를 업데이트합니다.

```bash
# 1. whatap.conf 파일 수정 (디버그 모드 비활성화 예시)
echo "# Whatap Agent Configuration" > whatap.conf
echo "# Set debug=true to enable debug output of metrics data" >> whatap.conf
echo "debug=false" >> whatap.conf

# 2. Docker 이미지 다시 빌드
./build-docker.sh

# 3. Kubernetes 배포 업데이트
kubectl apply -f k8s/deployment.yaml
```

#### 에이전트 버전 설정

쿠버네티스 환경에서 에이전트 버전을 설정하려면 deployment.yaml 파일의 환경 변수 섹션에서 `WHATAP_AGENT_VERSION` 값을 수정합니다:

```yaml
env:
- name: WHATAP_LICENSE
  valueFrom:
    secretKeyRef:
      name: whatap-credentials
      key: license
- name: WHATAP_HOST
  valueFrom:
    secretKeyRef:
      name: whatap-credentials
      key: host
- name: WHATAP_PORT
  valueFrom:
    secretKeyRef:
      name: whatap-credentials
      key: port
- name: WHATAP_AGENT_VERSION
  value: "1.0.0"  # 에이전트 버전 설정
```

버전 정보는 로그 및 메트릭에 표시되며, 에이전트 식별 및 문제 해결에 유용합니다.

Kubernetes 배포에 대한 자세한 내용은 [DEVELOPMENT.md](DEVELOPMENT.md) 파일을 참조하세요.

## 설정

에이전트는 `$WHATAP_HOME/scrape_config.yaml` 위치의 YAML 파일을 통해 설정됩니다. 두 가지 형식의 설정을 지원합니다:

### 1. 기본 형식

```yaml
global:
  scrape_interval: 15s  # 스크래핑 간격

scrape_configs:
  - job_name: prometheus  # 작업 이름
    static_config:
      targets:
        - localhost:9090  # 스크래핑 대상 URL
      filter:
        enabled: true     # 필터 활성화 여부
        whitelist:        # 수집할 메트릭 목록
          - http_requests_total
          - http_requests_duration_seconds
```

### 2. CR(Custom Resource) 형식

쿠버네티스 환경에서 사용되는 CR 형식도 지원합니다. 새로운 CR 형식은 다음과 같은 타겟 유형을 지원합니다:

1. **PodMonitor**: Pod 레이블 셀렉터를 이용한 동적 디스커버리 (Prometheus Operator의 PodMonitor와 유사)
2. **ServiceMonitor**: Service 레이블 셀렉터를 이용한 동적 디스커버리 (Prometheus Operator의 ServiceMonitor와 유사)
3. **StaticEndpoints**: 고정된 IP 주소와 포트를 직접 입력 (Prometheus의 static_configs와 유사)

```yaml
features:
  openAgent:
    enabled: true
    # 전역 기본 설정 (targets 내에서 재정의 가능)
    globalInterval: "60s"
    globalPath: "/metrics"
    targets:
      # 1. PodMonitor: Pod 레이블 셀렉터를 이용한 동적 디스커버리
      - targetName: my-app-pod-metrics
        type: PodMonitor
        namespaceSelector:
          matchNames:
            - "production"
        selector:
          matchLabels:
            app: my-app
        endpoints:
          - port: "web-metrics"  # Pod Spec에 정의된 Port 이름 또는 실제 Port 번호
            path: "/metrics"     # 기본값은 /metrics, 필요시 재정의
            interval: "15s"      # 기본값은 전역 설정, 필요시 재정의
            scheme: "http"
            timeout: "10s"
            honorLabels: false   # 프로메테우스의 honor_labels와 유사한 기능
            metricSelector:      # 수집할 메트릭 목록 (지정하지 않으면 모든 메트릭 수집)
              - http_requests_total
              - http_requests_duration_seconds

      # 2. ServiceMonitor: Service 레이블 셀렉터를 이용한 동적 디스커버리
      - targetName: my-service-metrics
        type: ServiceMonitor
        namespaceSelector:
          matchNames:
            - "default"
        selector:
          matchLabels:
            service: my-backend-service
        endpoints:
          - port: "http-metrics"  # Service Spec에 정의된 Port 이름 또는 실제 Target Port 번호
            path: "/actuator/prometheus"
            interval: "30s"
            metricSelector:
              - jvm_memory_used_bytes
              - jvm_threads_live

      # 3. StaticEndpoints: 고정된 IP 주소와 포트를 직접 입력
      - targetName: my-external-db-metrics
        type: StaticEndpoints
        scheme: "http"  # http 또는 https, 기본값 http
        addresses:  # 대상의 주소 목록 (IP:PORT 또는 HOSTNAME:PORT)
          - "192.168.1.100:9100"
          - "external-node-exporter.example.com:9100"
        labels:  # 이 타겟들에 공통적으로 추가될 레이블
          environment: "staging"
          component: "database-exporter"
        path: "/metrics"  # 모든 addresses에 적용될 기본 path
        interval: "60s"   # 모든 addresses에 적용될 기본 interval
        metricSelector:   # 모든 addresses에 적용될 기본 metricSelector
          - node_cpu_seconds_total
          - node_memory_MemTotal_bytes
```

#### CR 형식 공통 설정 요소

- **globalInterval**: 모든 타겟에 적용되는 기본 스크래핑 간격 (타겟 또는 엔드포인트에서 재정의 가능)
- **globalPath**: 모든 타겟에 적용되는 기본 메트릭 경로 (타겟 또는 엔드포인트에서 재정의 가능)

#### PodMetrics 및 ServiceMetrics 설정 요소

- **targetName**: 타겟의 이름 (로깅 및 식별용)
- **type**: 타겟 유형 ("PodMetrics" 또는 "ServiceMetrics")
- **namespaceSelector**: 스크래핑할 네임스페이스를 선택합니다.
  - `matchNames`: 이름으로 네임스페이스를 선택합니다.
  - `matchLabels`: 레이블로 네임스페이스를 선택합니다.
  - `matchExpressions`: 표현식으로 네임스페이스를 선택합니다.

- **selector**: 스크래핑할 파드 또는 서비스를 선택합니다.
  - `matchLabels`: 레이블로 파드 또는 서비스를 선택합니다.
  - `matchExpressions`: 표현식으로 파드 또는 서비스를 선택합니다.

- **endpoints**: 스크래핑할 엔드포인트를 정의합니다.
  - `port`: 스크래핑할 포트 이름 또는 번호
  - `path`: 메트릭 경로 (기본값은 globalPath, 필요시 재정의)
  - `interval`: 스크래핑 간격 (기본값은 globalInterval, 필요시 재정의)
  - `scheme`: 스크래핑 프로토콜 (http 또는 https, 기본값 http)
  - `timeout`: 스크래핑 타임아웃
  - `honorLabels`: 대상에서 제공하는 레이블을 우선시할지 여부
  - `metricSelector`: 수집할 메트릭 이름 목록 (지정하지 않으면 모든 메트릭 수집)

#### StaticEndpoints 설정 요소

- **targetName**: 타겟의 이름 (로깅 및 식별용)
- **type**: 타겟 유형 ("StaticEndpoints")
- **scheme**: 스크래핑 프로토콜 (http 또는 https, 기본값 http)
- **addresses**: 스크래핑할 대상 주소 목록 (IP:PORT 또는 HOSTNAME:PORT)
- **labels**: 모든 타겟에 추가할 레이블
- **path**: 메트릭 경로 (기본값은 globalPath, 필요시 재정의)
- **interval**: 스크래핑 간격 (기본값은 globalInterval, 필요시 재정의)
- **metricSelector**: 수집할 메트릭 이름 목록 (지정하지 않으면 모든 메트릭 수집)

## TLS 설정

OpenAgent는 HTTPS 엔드포인트에 연결할 때 TLS(Transport Layer Security)를 지원합니다. 다음은 TLS 관련 설정 옵션입니다:

### HTTP vs HTTPS 결정 방법

OpenAgent는 다음과 같은 규칙에 따라 HTTP 또는 HTTPS 프로토콜을 사용할지 결정합니다:

1. **PodMonitor 및 ServiceMonitor 타겟**:
   - 포트 이름이 "https"인 경우 기본적으로 HTTPS를 사용합니다.
   - 그 외의 경우 기본적으로 HTTP를 사용합니다.

2. **StaticEndpoints 타겟**:
   - TLS 설정이 존재하는 경우 기본적으로 HTTPS를 사용합니다.
   - 그 외의 경우 기본적으로 HTTP를 사용합니다.

3. **모든 타겟 유형**:
   - 엔드포인트나 타겟에 명시적으로 `scheme` 설정이 있는 경우, 이 설정이 기본값을 재정의합니다.

### TLS 설정 옵션

TLS 설정은 `tlsConfig` 섹션에서 구성할 수 있습니다:

```yaml
endpoints:
  - port: "https"
    path: "/metrics"
    scheme: "https"  # 명시적으로 HTTPS 사용 지정
    tlsConfig:
      insecureSkipVerify: true  # 인증서 검증 건너뛰기
```

#### insecureSkipVerify

`insecureSkipVerify` 옵션은 서버 인증서의 유효성 검사를 건너뛰도록 설정합니다. 이 옵션은 다음과 같은 경우에 유용합니다:

- 자체 서명된 인증서를 사용하는 서버에 연결할 때
- 개발 또는 테스트 환경에서 인증서 검증이 필요하지 않을 때
- 내부 네트워크에서 신뢰할 수 있는 서버에 연결할 때

**주의**: 프로덕션 환경에서는 보안상의 이유로 `insecureSkipVerify: false`를 사용하는 것이 좋습니다. 자체 서명된 인증서를 사용하는 경우, 인증서를 신뢰할 수 있는 인증 기관(CA)으로 추가하는 것이 더 안전한 방법입니다.

### 설정 예제

#### 1. ServiceMonitor에서 TLS 설정 예제

```yaml
- targetName: kube-apiserver
  type: ServiceMonitor
  namespaceSelector:
    matchNames:
      - "default"
  selector:
    matchLabels:
      component: apiserver
      provider: kubernetes
  endpoints:
    - port: "https"  # 포트 이름이 "https"이므로 기본적으로 HTTPS 사용
      path: "/metrics"
      interval: "30s"
      scheme: "https"  # 명시적으로 HTTPS 지정 (선택사항)
      tlsConfig:
        insecureSkipVerify: true  # 인증서 검증 건너뛰기
      metricSelector:
        - apiserver_requests_total
```

#### 2. StaticEndpoints에서 TLS 설정 예제

```yaml
- targetName: external-secure-service
  type: StaticEndpoints
  scheme: "https"  # 명시적으로 HTTPS 지정
  addresses:
    - "secure-service.example.com:443"
  path: "/metrics"
  interval: "60s"
  tlsConfig:
    insecureSkipVerify: true  # 인증서 검증 건너뛰기
  metricSelector:
    - http_requests_total
```

## 쿠버네티스 메트릭 수집 예제

다음은 쿠버네티스 API 서버에서 메트릭을 수집하는 예제입니다:

```yaml
# scrape_config.yaml
features:
  openAgent:
    enabled: true
    globalInterval: "60s"
    globalPath: "/metrics"
    targets:
      - targetName: kube-apiserver
        type: ServiceMonitor
        namespaceSelector:
          matchNames:
            - "kube-system"
        selector:
          matchLabels:
            component: apiserver
            provider: kubernetes
        endpoints:
          - port: "https"
            path: "/metrics"
            interval: "30s"
            metricSelector:
              - apiserver_requests_total
```

이 설정은 kube-system 네임스페이스에서 component=apiserver 및 provider=kubernetes 레이블을 가진 서비스를 찾아 해당 서비스의 엔드포인트에서 메트릭을 수집합니다. metricSelector를 사용하여 apiserver_requests_total 메트릭만 수집하도록 지정할 수 있습니다.

## 라이센스

이 프로젝트는 MIT 라이센스를 따릅니다 - 자세한 내용은 LICENSE 파일을 참조하세요.
