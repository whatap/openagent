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

## 설치 및 실행

### 필수 환경 변수

OpenAgent를 실행하려면 다음 환경 변수를 설정해야 합니다:

- `WHATAP_LICENSE`: 와탭 라이센스 키
- `WHATAP_HOST`: 와탭 서버 호스트 주소
- `WHATAP_PORT`: 와탭 서버 포트 (기본값: 6600)

## 설정

에이전트는 `$WHATAP_HOME/scrape_config.yaml` 위치의 YAML 파일을 통해 설정됩니다. 

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
        # enabled: true  # 타겟 활성화 여부 (기본값: true, 생략 가능)
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
            metricRelabelConfigs:  # 스크래핑 후 메트릭 재라벨링 설정
              - source_labels: [__name__]
                regex: "http_requests_total"
                action: keep
              - source_labels: [method]
                target_label: http_method
                replacement: "${1}"
                action: replace

      # 2. ServiceMonitor: Service 레이블 셀렉터를 이용한 동적 디스커버리
      - targetName: my-service-metrics
        type: ServiceMonitor
        # enabled: true  # 타겟 활성화 여부 (기본값: true, 생략 가능)
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
            metricRelabelConfigs:
              - source_labels: [__name__]
                regex: "jvm_.*"
                action: keep
              - source_labels: [area]
                target_label: memory_area
                replacement: "${1}"
                action: replace

      # 3. StaticEndpoints: 고정된 IP 주소와 포트를 직접 입력
      - targetName: my-external-db-metrics
        type: StaticEndpoints
        # enabled: true  # 타겟 활성화 여부 (기본값: true, 생략 가능)
        scheme: "http"  # http 또는 https, 기본값 http
        addresses:  # 대상의 주소 목록 (IP:PORT 또는 HOSTNAME:PORT)
          - "192.168.1.100:9100"
          - "external-node-exporter.example.com:9100"
        labels:  # 이 타겟들에 공통적으로 추가될 레이블
          environment: "staging"
          component: "database-exporter"
        path: "/metrics"  # 모든 addresses에 적용될 기본 path
        interval: "60s"   # 모든 addresses에 적용될 기본 interval
        metricRelabelConfigs:   # 모든 addresses에 적용될 기본 metricRelabelConfigs
          - source_labels: [__name__]
            regex: "node_(cpu|memory).*"
            action: keep
          - source_labels: [instance]
            target_label: server
            replacement: "${1}"
            action: replace

      # 비활성화된 타겟 예시 (스크래핑 시 건너뜀)
      - targetName: disabled-target-example
        type: StaticEndpoints
        # 타겟을 비활성화하려면 enabled를 false로 설정
        enabled: false
        addresses:
          - "disabled-example.com:9100"
        path: "/metrics"
        interval: "60s"
```

#### CR 형식 공통 설정 요소

- **globalInterval**: 모든 타겟에 적용되는 기본 스크래핑 간격 (타겟 또는 엔드포인트에서 재정의 가능)
- **globalPath**: 모든 타겟에 적용되는 기본 메트릭 경로 (타겟 또는 엔드포인트에서 재정의 가능)

#### 타겟 공통 설정 요소

- **targetName**: 타겟의 이름 (필수)
- **type**: 타겟의 유형 (PodMonitor, ServiceMonitor, StaticEndpoints) (필수)
- **enabled**: 타겟 활성화 여부 (기본값: true, 생략 가능). false로 설정하면 해당 타겟은 스크래핑 시 건너뜀
- **interval**: 타겟의 스크래핑 간격 (기본값: globalInterval)
- **path**: 타겟의 메트릭 경로 (기본값: globalPath)
- **metricRelabelConfigs**: 메트릭 재라벨링 설정

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
  - `metricRelabelConfigs`: 스크래핑 후 메트릭 재라벨링 설정 (프로메테우스의 metric_relabel_configs와 유사)

#### StaticEndpoints 설정 요소

- **targetName**: 타겟의 이름 (로깅 및 식별용)
- **type**: 타겟 유형 ("StaticEndpoints")
- **scheme**: 스크래핑 프로토콜 (http 또는 https, 기본값 http)
- **addresses**: 스크래핑할 대상 주소 목록 (IP:PORT 또는 HOSTNAME:PORT)
- **labels**: 모든 타겟에 추가할 레이블
- **path**: 메트릭 경로 (기본값은 globalPath, 필요시 재정의)
- **interval**: 스크래핑 간격 (기본값은 globalInterval, 필요시 재정의)
- **metricRelabelConfigs**: 스크래핑 후 메트릭 재라벨링 설정 (프로메테우스의 metric_relabel_configs와 유사)

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
      metricRelabelConfigs:
        - source_labels: [__name__]
          regex: "apiserver_request_total"
          action: keep
        - source_labels: [verb]
          target_label: http_verb
          replacement: "${1}"
          action: replace
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
  metricRelabelConfigs:
    - source_labels: [__name__]
      regex: "http_requests_total"
      action: keep
    - source_labels: [method]
      target_label: http_method
      replacement: "${1}"
      action: replace
```

## 메트릭 재라벨링 설정 (metricRelabelConfigs)

OpenAgent는 프로메테우스의 metric_relabel_configs와 유사한 메트릭 재라벨링 기능을 지원합니다. 이 기능을 사용하면 스크래핑 후 메트릭을 필터링하거나 레이블을 변경할 수 있습니다.

### 재라벨링 설정 요소

- **source_labels**: 소스 레이블 목록 (배열)
- **separator**: 소스 레이블 값을 연결할 때 사용할 구분자 (기본값: `;`)
- **target_label**: 대상 레이블 (결과를 저장할 레이블)
- **regex**: 소스 레이블 값에 적용할 정규식
- **replacement**: 대체 값 (정규식 캡처 그룹 참조 가능, 예: `${1}`)
- **action**: 수행할 작업 (keep, drop, replace, labelmap, labelkeep, labeldrop)

### 지원되는 작업 (action)

- **keep**: 정규식과 일치하는 메트릭만 유지
- **drop**: 정규식과 일치하는 메트릭 제거
- **replace**: 대상 레이블의 값을 대체 값으로 변경
- **labelmap**: 정규식과 일치하는 레이블을 새 레이블로 매핑
- **labelkeep**: 정규식과 일치하는 레이블만 유지
- **labeldrop**: 정규식과 일치하는 레이블 제거

### 특수 레이블

- **__name__**: 메트릭 이름을 나타내는 특수 레이블

### 예제

#### 1. 특정 메트릭만 유지

```yaml
metricRelabelConfigs:
  - source_labels: [__name__]
    regex: "http_requests_total"
    action: keep
```

이 설정은 `http_requests_total` 메트릭만 유지하고 나머지는 모두 제거합니다.

**동작 예시:**

원래 스크랩된 메트릭이 다음과 같다고 가정해 봅시다:

```
http_requests_total{method="GET", status="200"} 100
http_errors_total{method="GET", status="500"} 5
node_cpu_seconds_total{cpu="0", mode="idle"} 1000
```

위 metricRelabelConfigs를 적용하면:

```
http_requests_total{method="GET", status="200"} 100
```

`http_requests_total` 메트릭만 유지되고 다른 메트릭들은 모두 제거됩니다.

#### 2. 정규식을 사용한 메트릭 필터링

```yaml
metricRelabelConfigs:
  - source_labels: [__name__]
    regex: "node_(cpu|memory).*"
    action: keep
```

이 설정은 `node_cpu`나 `node_memory`로 시작하는 메트릭만 유지합니다.

**동작 예시:**

원래 스크랩된 메트릭이 다음과 같다고 가정해 봅시다:

```
node_cpu_seconds_total{cpu="0", mode="idle"} 1000
node_memory_MemTotal_bytes{} 16777216
node_disk_io_time_seconds_total{device="sda"} 100
http_requests_total{method="GET", status="200"} 100
```

위 metricRelabelConfigs를 적용하면:

```
node_cpu_seconds_total{cpu="0", mode="idle"} 1000
node_memory_MemTotal_bytes{} 16777216
```

`node_cpu`나 `node_memory`로 시작하는 메트릭만 유지되고 다른 메트릭들은 모두 제거됩니다. 정규식을 사용하여 여러 메트릭 패턴을 한 번에 필터링할 수 있습니다.

#### 3. 레이블 이름 변경

```yaml
metricRelabelConfigs:
  - source_labels: [method]
    target_label: http_method
    replacement: "${1}"
    action: replace
```

이 설정은 `method` 레이블의 값을 `http_method` 레이블로 복사합니다.

**동작 예시:**

원래 스크랩된 메트릭이 다음과 같다고 가정해 봅시다:

```
http_requests_total{method="GET", path="/api", status="200"} 100
http_requests_total{method="POST", path="/api/users", status="201"} 50
```

위 metricRelabelConfigs를 적용하면:

```
http_requests_total{method="GET", path="/api", status="200", http_method="GET"} 100
http_requests_total{method="POST", path="/api/users", status="201", http_method="POST"} 50
```

각 메트릭에 `method` 레이블의 값을 복사한 `http_method` 레이블이 추가됩니다. 원래 레이블은 유지되며, 새 레이블이 추가됩니다. `${1}`은 소스 레이블의 값을 참조합니다.

#### 4. 여러 소스 레이블 조합

```yaml
metricRelabelConfigs:
  - source_labels: [__name__, status]
    regex: "http_requests_total;(200|500)"
    action: keep
```

이 설정은 `http_requests_total` 메트릭 중 `status` 레이블이 `200` 또는 `500`인 메트릭만 유지합니다.

**동작 예시:**

원래 스크랩된 메트릭이 다음과 같다고 가정해 봅시다:

```
http_requests_total{method="GET", path="/api", status="200"} 100
http_requests_total{method="POST", path="/api/users", status="201"} 50
http_requests_total{method="GET", path="/api/error", status="500"} 10
http_requests_total{method="GET", path="/api/error", status="404"} 5
```

위 metricRelabelConfigs를 적용하면:

```
http_requests_total{method="GET", path="/api", status="200"} 100
http_requests_total{method="GET", path="/api/error", status="500"} 10
```

`http_requests_total` 메트릭 중에서 `status` 레이블이 `200` 또는 `500`인 메트릭만 유지됩니다. 여러 소스 레이블을 조합할 때는 기본적으로 `;` 구분자로 연결되며, 이를 `separator` 필드로 변경할 수 있습니다.

#### 5. 정적 레이블 추가

```yaml
metricRelabelConfigs:
  - target_label: metric_src
    replacement: "whatap-open-agent"
    action: replace
```

이 설정은 모든 메트릭에 `metric_src="whatap-open-agent"` 레이블을 추가합니다. 소스 레이블을 지정하지 않으면 `replacement` 값이 직접 레이블 값으로 사용됩니다. 이 방법을 사용하여 모든 메트릭에 환경, 리전, 애플리케이션 이름 등의 정적 레이블을 추가할 수 있습니다.

**동작 예시:**

원래 스크랩된 메트릭이 다음과 같다고 가정해 봅시다:

```
http_requests_total{method="GET", path="/api", status="200"} 100
node_cpu_seconds_total{cpu="0", mode="idle"} 1000
```

위 metricRelabelConfigs를 적용하면:

```
http_requests_total{method="GET", path="/api", status="200", metric_src="whatap-open-agent"} 100
node_cpu_seconds_total{cpu="0", mode="idle", metric_src="whatap-open-agent"} 1000
```

모든 메트릭에 `metric_src="whatap-open-agent"` 레이블이 추가됩니다. 이 방법은 메트릭의 출처를 표시하거나, 환경(예: production, staging), 리전(예: us-east, eu-west), 또는 애플리케이션 이름 등을 표시하는 데 유용합니다.

### 종합적인 동작 예시

원래 스크랩된 메트릭이 다음과 같다고 가정해 봅시다:

```
apiserver_request_total{code="200", resource="pods", verb="GET"} 100
some_other_metric{label="value"} 50
```

위 metricRelabelConfigs를 적용하면:

1. 첫 번째 룰(keep apiserver_request_total) 적용:
   - apiserver_request_total 메트릭은 유지됩니다.
   - some_other_metric 메트릭은 드롭됩니다.

2. 두 번째 룰(replace verb -> http_verb) 적용:
   - 유지된 apiserver_request_total 메트릭에 verb 레이블이 있으므로, 이 레이블의 값(GET)이 http_verb라는 새로운 레이블로 복사됩니다.

따라서 Prometheus에 최종적으로 수집되는 메트릭은 다음과 같을 것입니다:

```
apiserver_request_total{code="200", resource="pods", verb="GET", http_verb="GET"} 100
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
            metricRelabelConfigs:
              - source_labels: [__name__]
                regex: "apiserver_request_total"
                action: keep
              - source_labels: [verb]
                target_label: http_verb
                replacement: "${1}"
                action: replace
              # 정적 레이블 추가
              - target_label: metric_src
                replacement: "whatap-open-agent"
                action: replace
```


이 설정은 kube-system 네임스페이스에서 component=apiserver 및 provider=kubernetes 레이블을 가진 서비스를 찾아 해당 서비스의 엔드포인트에서 메트릭을 수집합니다.<br>
metricRelabelConfigs를 사용하여 apiserver_request_total 메트릭만 수집하고, verb 레이블을 http_verb 레이블로 변환하며, 모든 메트릭에 metric_src="whatap-open-agent" 정적 레이블을 추가하도록 지정할 수 있습니다.
