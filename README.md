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
│   ├── rdsevent/         # AWS RDS 이벤트 수집 (에이전시 aws_rds_event 호환)
│   ├── scraper/          # 메트릭 스크래퍼
│   └── sender/           # 처리된 메트릭 전송기
├── scrape_config.yaml    # 스크래핑 설정 파일
├── examples/
│   ├── metric-exporter-go/  # 교육용 OpenMetrics Exporter 예제 (/metrics, :9529)
│   └── scrape_config_metric_exporter.yaml  # 예제 타겟 설정 샘플
├── test/
│   └── integration/      # 통합 테스트 및 샘플 코드
├── go.mod                # Go 모듈 정의
└── README.md             # 현재 파일
```

### 교육용 예제 빠른 시작
- 예제 실행: `cd examples/metric-exporter-go && go mod tidy && go run .`
- 확인: `curl -s -H "Accept: application/openmetrics-text" http://localhost:9529/metrics | head`
- 스크랩 설정 샘플: `examples/scrape_config_metric_exporter.yaml` 참고 (path: /metrics, address: localhost:9529)

## 설치 및 실행

### 필수 환경 변수

OpenAgent를 실행하려면 다음 환경 변수를 설정해야 합니다:

- `WHATAP_LICENSE`: 와탭 라이센스 키
- `WHATAP_HOST`: 와탭 서버 호스트 주소
- `WHATAP_PORT`: 와탭 서버 포트 (기본값: 6600)

### 선택 설정 (환경 변수 또는 whatap.conf)

- `openagent_enable_protobuf`: Prometheus protobuf 스크랩(콘텐츠 협상)을 활성화합니다.
  - 기본값 `false` — 기존 동작과 동일하게 `Accept: application/json` 으로 스크랩합니다.
  - `true` 로 설정하면 `Accept` 헤더에 protobuf > OpenMetrics > text 우선순위를 광고하고,
    응답 `Content-Type` 에 따라 protobuf/text 디코더를 자동 선택합니다.
    classic 메트릭(counter/gauge/summary/classic histogram)은 기존과 동일한 flat 시리즈로 수집되며,
    native histogram 은 디코딩되지만 OpenMx 변환은 후속 작업(KAZAA-591 step 4)에서 추가됩니다.
- `dcgm_weighted_gpu_util_enabled`: dcgm-exporter 스크랩에서 `DCGM_FI_DEV_WEIGHTED_GPU_UTIL`을 계산해 추가합니다.
  - 기본값 `true`. `false`로 설정하면 계산하지 않습니다.
  - 자세한 동작은 아래 [DCGM 가중 GPU 사용률](#dcgm-가중-gpu-사용률)을 참고하세요.

### DCGM 가중 GPU 사용률

**OpenAgent 1.8.13 이상**은 NVIDIA 공식 dcgm-exporter 스크랩으로도 WhaTap GPU 화면이 쓰는 `DCGM_FI_DEV_WEIGHTED_GPU_UTIL`(물리 GPU 1장의 사용률, 0~1)을 만듭니다. WhaTap dcgm-exporter 이미지처럼 스크랩에 이 지표가 이미 있으면 exporter 값을 그대로 쓰고 계산하지 않습니다.

| GPU | 계산 | `calculation_method` |
|---|---|---|
| 일반 GPU | `DCGM_FI_DEV_GPU_UTIL / 100` (100% 초과는 100%) | `direct` |
| MIG GPU | 인스턴스별 `DCGM_FI_PROF_GR_ENGINE_ACTIVE × (인스턴스 slice 수 / DCGM_FI_DEV_MIG_MAX_SLICES)`의 합. max slices 레이블이 없으면 7 | `weighted_sum` |
| 일반 H100·H200·H800·H20 | `DCGM_FI_PROF_GR_ENGINE_ACTIVE` (없으면 `GPU_UTIL / 100`) | `prof_gr_engine_active` |

레이블은 WhaTap exporter 출력과 같습니다. MIG GPU 행에는 장치 레이블과 `DCGM_FI_*` 레이블만 남기고 인스턴스·Pod 레이블(`GPU_I_*`, `pod`, `namespace`, `container`, Pod 레이블)은 붙이지 않습니다. 계산은 metric relabeling 전에 하므로 `metricRelabelConfigs`와 target 레이블이 exporter 지표와 똑같이 적용됩니다.

### EC2 상태의 다중 프로젝트 전달

[EC2 상태 설정 매뉴얼](docs/ec2-instance-state-setup.md): Exporter 실행, 설정 파일 위치, 최소 설정, 자동 기본값 및 동작 확인 절차.

**OpenAgent 1.8.11 이상**이 필요합니다. 별도 저장소 `whatap/exporters`의 `ec2/` 상태 Exporter(공식 바이너리 `https://repo.whatap.io/exporter/ec2_state_exporter/<version>/{amd64,arm64}/ec2_state_exporter`, 0.1.0 이상, 기본 `127.0.0.1:9107`)를 기존 `scrape_config.yaml` target에 한 번 등록합니다. 기존 CloudWatch 성능 Exporter와는 별도 컴포넌트·포트입니다. 대상 수집서버가 1002를 지원하면(3.1.10 이상) `whatap.conf`에 `ec2_state_forwarding_enabled=true`와 `ec2_state_licenses=<SERVER_LICENSE_A>,<SERVER_LICENSE_B>` 두 키만 설정하면 됩니다.

메트릭 기본 계약은 `aws_ec2_instance_state{instance_id="...",state_name="...",region="..."} <EC2 State.Code>`입니다. 메트릭명·레이블명·URL을 중복 설정할 필요가 없으며, 이미 수집한 원본만 사용하고 추가 URL probe나 AWS 직접 조회를 하지 않습니다. URL allowlist와 custom metric/label/source override도 계속 지원합니다.

상태는 `INFRA_EC2_INSTANCE_STATE` 명령 ID **1002**(Open Agent 할당 범위 `1000~1099`), `ParamPack(oneway)`으로 각 Server 프로젝트에 전달합니다. 기존 `ec2_state_param_id=56` 설정이 있다면 제거해 기본값을 사용하거나 `1002`로 변경해야 합니다. 최초·변경 때 전송하고 같은 상태는 기본 **5분** 후의 성공한 scrape에서 재전송합니다. 기능 기본값은 **OFF**이며 수집 실패나 목록 소실을 terminated로 합성하지 않습니다. 로그의 `locally queued`는 로컬 큐 수락이며 Yard 수신·해지 완료를 뜻하지 않습니다. 최소 설정과 호환성은 [EC2 상태 전달 명세](docs/ec2-instance-state.md)를 참고하세요. Kubernetes Operator 환경에서는 같은 `ec2_state_*` 키를 `spec.features.openAgent.envs`로 지정합니다(값 직접 입력 또는 Secret 참조).

Yard의 최신 계약은 종료 관측의 pcode에서 `meta:instance-id`가 일치하는 Server 등록을 **모두 해지**하는 방식입니다. OpenAgent는 인스턴스 1건당 각 프로젝트에 ParamPack 1개를 보내며, 등록 서버 수를 검사하거나 대상 oid를 지정하지 않습니다.

### Docker 이미지 빌드

#### 기본 Docker 빌드

Docker를 사용하여 OpenAgent 이미지를 빌드할 수 있습니다:

```bash
# 기본 빌드
docker build -t openagent:latest .

# 버전 지정 빌드
docker build -t openagent:1.0.0 --build-arg VERSION="1.0.0" --build-arg COMMIT_HASH="$(git rev-parse --short HEAD)" .
```

`VERSION` 및 `COMMIT_HASH` 빌드 인자는 애플리케이션 내부에서 사용되며, 로그 및 메트릭에 표시됩니다.

#### build-docker.sh 스크립트 사용 (권장)

더 편리한 빌드를 위해 제공된 `build-docker.sh` 스크립트를 사용할 수 있습니다:

```bash
# 버전 지정 빌드
./build-docker.sh --tag 1.0.0 --version "1.0.0" --commit "$(git rev-parse --short HEAD)"

# 레지스트리에 푸시
./build-docker.sh --tag 1.0.0 --registry whatap --push --version "1.0.0"
```

스크립트 옵션:
- `--tag`, `-t`: 이미지 태그 (기본값: latest)
- `--registry`, `-r`: 레지스트리 (예: whatap)
- `--push`, `-p`: 빌드 후 이미지 푸시
- `--arch`, `-a`: 대상 아키텍처 (amd64, arm64, all)
- `--version`, `-v`: 애플리케이션 버전 (기본값: 태그와 동일)
- `--commit`, `-c`: 커밋 해시 (기본값: 현재 git 커밋)

## 설정

일반 호스트 실행에서 scrape 설정은 `$WHATAP_OPEN_HOME/scrape_config.yaml`에서 읽습니다. `WHATAP_OPEN_HOME`이 없으면 프로세스 작업 디렉터리의 파일을 사용합니다. `whatap.conf`의 기준 경로는 별도로 `$WHATAP_HOME`입니다.

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
        endpoints:
          - address: "192.168.1.100:9100"
            path: "/metrics"
            scheme: "http"
            interval: "60s"
            metricRelabelConfigs:
              - source_labels: [__name__]
                regex: "node_(cpu|memory).*"
                action: keep
              - source_labels: [instance]
                target_label: server
                replacement: "${1}"
                action: replace
          - address: "external-node-exporter.example.com:9100"
            path: "/metrics"
            scheme: "http"
            interval: "60s"
            metricRelabelConfigs:
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
        endpoints:
          - address: "disabled-example.com:9100"
            path: "/metrics"
            interval: "60s"
```

#### 타겟 공통 설정 요소

- **targetName**: 타겟의 이름 (필수)
- **type**: 타겟의 유형 (PodMonitor, ServiceMonitor, StaticEndpoints) (필수)
- **enabled**: 타겟 활성화 여부 (기본값: true, 생략 가능). false로 설정하면 해당 타겟은 스크래핑 시 건너뜀

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
  - `path`: 메트릭 경로 (기본값: /metrics)
  - `interval`: 스크래핑 간격 (기본값: 60s)
  - `scheme`: 스크래핑 프로토콜 (http 또는 https, 기본값 http)
  - `timeout`: 스크래핑 타임아웃
  - `addNodeLabel`: PodMonitor 타입에서 노드 라벨 추가 여부 (기본값: false)
  - `metricRelabelConfigs`: 스크래핑 후 메트릭 재라벨링 설정 (프로메테우스의 metric_relabel_configs와 유사)

#### PodMonitor의 addNodeLabel 기능

PodMonitor 타입에서는 `addNodeLabel` 옵션을 사용하여 대상 파드가 스케줄링된 노드의 이름을 메트릭에 라벨로 추가할 수 있습니다.

- **설정 위치**: 엔드포인트 레벨에서만 설정 가능
- **기본값**: `false`
- **동작**: `true`로 설정하면 모든 메트릭에 `node` 라벨이 추가되며, 값은 파드가 실행 중인 노드의 이름입니다

**사용 예제:**

```yaml
# 엔드포인트 레벨에서 addNodeLabel 설정
- targetName: node-exporter
  type: PodMonitor
  namespaceSelector:
    matchNames:
      - "monitoring"
  selector:
    matchLabels:
      app: node-exporter
  endpoints:
    - port: "metrics"
      path: "/metrics"
      interval: "30s"
      addNodeLabel: true  # 이 엔드포인트에 노드 라벨 추가
      metricRelabelConfigs:
        # 노드 라벨을 사용한 재라벨링 예제
        - source_labels: [node]
          target_label: kubernetes_node
          action: replace

- targetName: dcgm-exporter
  type: PodMonitor
  namespaceSelector:
    matchNames:
      - "kube-system"
  selector:
    matchLabels:
      app: dcgm-exporter
  endpoints:
    - port: "metrics"
      path: "/metrics"
      interval: "30s"
      addNodeLabel: true  # 이 엔드포인트에 노드 라벨 추가
      metricRelabelConfigs:
        - source_labels: [node]
          target_label: gpu_node
          action: replace
```

이 기능은 특히 DaemonSet으로 배포된 파드들의 메트릭을 수집할 때 유용합니다. 각 노드별로 메트릭을 구분하여 분석하거나, 특정 노드의 메트릭만 필터링할 때 활용할 수 있습니다.

#### StaticEndpoints 설정 요소

- **targetName**: 타겟의 이름 (로깅 및 식별용)
- **type**: 타겟 유형 ("StaticEndpoints")
- **endpoints**: 스크래핑할 엔드포인트를 정의합니다.
  - `address`: 스크래핑할 대상 주소 (IP:PORT 또는 HOSTNAME:PORT)
  - `path`: 메트릭 경로 (기본값: /metrics)
  - `scheme`: 스크래핑 프로토콜 (http 또는 https, 기본값 http)
  - `interval`: 스크래핑 간격 (기본값: 60s)
  - `tlsConfig`: TLS 설정
  - `metricRelabelConfigs`: 스크래핑 후 메트릭 재라벨링 설정

StaticEndpoints는 이제 PodMonitor 및 ServiceMonitor와 동일한 `endpoints` 배열 구조를 사용하여 일관된 설정 방식을 제공합니다.

## AWS RDS 이벤트 수집 (에이전시 `aws_rds_event` 호환)

**OpenAgent 1.8.15 이상**에서 쓸 수 있습니다. 기본값은 **꺼짐**입니다.

AWS RDS `DescribeEvents`를 리전마다 주기적으로 조회해, RDS 이벤트(재시작, 페일오버, 백업 등) 하나를 태그카운트 팩(TagCountPack) 하나로 이 OpenAgent의 프로젝트에 보냅니다. 팩 형식은 클라우드 모니터링 에이전시가 보내던 RDS 이벤트와 같으므로, 에이전시 데이터로 만든 알림 규칙을 고치지 않고 씁니다.

| 항목 | 값 |
|---|---|
| category | `aws_rds_event` |
| time | 이벤트 발생 시각(`Date`, ms) |
| tags (이 순서) | `category0`, `category1`, … (이벤트 범주 순서대로), `sourceArn`, `sourceIdentifier`, `sourceType`(예: `db-instance`), `agency` |
| fields | `message` |

값이 없는 태그는 빈 문자열로 보냅니다. 한 주기에 모은 이벤트는 모든 리전을 합쳐 시각순으로 정렬한 뒤 보냅니다.

### 설정

`scrape_config.yaml`의 `features.openAgent` 아래에 `awsRdsEvents` 섹션을 둡니다. 이 섹션은 자체 `enabled`로 켜고 끄며 `openAgent.enabled`, `targets`와는 따로 동작합니다. 주기마다 다시 읽으므로 켜기·끄기·변경에 재시작이 필요 없습니다.

```yaml
features:
  openAgent:
    enabled: true
    targets: []
    awsRdsEvents:
      enabled: true                  # 기본 false
      regions: [ap-northeast-2, us-east-1]
      # 다른 계정의 역할을 맡을 때만 (없으면 서버의 기본 자격 증명으로 조회)
      roleArn: "arn:aws:iam::<조회 대상 계정>:role/WhatapRdsEventsReadOnly"
      externalId: "<외부 ID>"         # 선택, roleArn과 함께
      interval: 60s
      agencyName: openagent
      overlap: 5m
      initialLookback: 10m
      maxCatchUp: 24h
      stateFile: rds_events_state.json
```

| 키 | 기본값 | 설명 |
|---|---|---|
| `enabled` | `false` | `true`일 때만 AWS를 호출합니다. |
| `regions` | (필수) | 조회할 리전 목록. YAML 목록 또는 쉼표로 구분한 문자열. |
| `roleArn` | 없음 | 있으면 기본 자격 증명으로 이 역할을 맡아(`sts:AssumeRole`) 조회합니다. 세션 이름은 `whatap-openagent-rds-events`입니다. |
| `externalId` | 없음 | AssumeRole에 넣는 외부 ID. `roleArn`이 있어야 합니다. |
| `interval` | `60s` | 조회 주기. 10초 이상. |
| `agencyName` | `openagent` | `agency` 태그 값. 에이전시는 여기에 서비스 id를 넣었습니다. 기존 알림 규칙이나 화면이 `agency` 값으로 거른다면 기존 데이터에 찍힌 값을 그대로 넣으세요. |
| `overlap` | `5m` | 직전 구간 끝에서 이만큼 겹쳐 다시 읽습니다. 0~1h. |
| `initialLookback` | `10m` | 상태 파일이 없을 때(처음 켤 때) 거슬러 보는 범위. 최대 335h. |
| `maxCatchUp` | `24h` | 재시작 뒤 따라잡는 구간의 상한. `interval` 이상, 최대 335h. RDS는 이벤트를 14일 보관하지만 정확히 14일(336h) 전에서 시작하는 조회는 `InvalidParameterCombination`으로 거부합니다. |
| `stateFile` | `$WHATAP_OPEN_HOME/rds_events_state.json` | 마지막 조회 끝 시각과 중복 제거 키를 저장하는 파일. 상대 경로는 `$WHATAP_OPEN_HOME` 기준입니다. |

설정 값이 잘못되면(리전 이름 오류, 모르는 키 등) 수집만 멈추고 `RDS_EVENTS` 로그에 이유를 한 번 남깁니다. 에이전트의 다른 기능은 그대로 동작합니다.

**지원 범위**: 호스트에 직접 설치해 `scrape_config.yaml` 파일을 읽는 **파일 모드**만 지원합니다. Kubernetes 설치(Operator, Helm)는 ConfigMap의 `scrape_config.yaml`을 `enabled`, `targets` 등 정해진 키로만 만들기 때문에 `awsRdsEvents` 섹션이 전달되지 않습니다. ConfigMap을 직접 관리하면 OpenAgent가 섹션을 읽기는 하지만 검증하지 않았고, Pod가 다시 뜨면 상태 파일이 사라지므로 `stateFile`을 영구 볼륨에 두어야 합니다.

### 자격 증명과 IAM 권한

자격 증명은 AWS SDK 기본 체인에서 얻습니다(환경 변수, `~/.aws`, ECS·EKS 태스크 역할, EC2 인스턴스 역할 순). EC2에서는 인스턴스 역할을 쓰면 키를 서버에 두지 않아도 됩니다. `roleArn`을 주면 그 자격으로 역할을 맡고, 임시 자격은 만료 전에 자동으로 갱신합니다. 서버에서 `rds.<리전>.amazonaws.com`, `sts.<리전>.amazonaws.com`(HTTPS 443)으로 나갈 수 있어야 합니다.

이벤트를 조회하는 쪽(역할을 쓰면 그 역할, 아니면 서버의 자격)에 필요한 권한:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    { "Effect": "Allow", "Action": "rds:DescribeEvents", "Resource": "*" }
  ]
}
```

`rds:DescribeEvents`는 리소스 단위 권한을 지원하지 않아 `Resource`가 `*`입니다. `roleArn`을 쓰면 추가로 두 가지가 필요합니다.

1. 서버의 자격(예: 인스턴스 역할)에 역할을 맡을 권한:

   ```json
   { "Effect": "Allow", "Action": "sts:AssumeRole", "Resource": "arn:aws:iam::<조회 대상 계정>:role/WhatapRdsEventsReadOnly" }
   ```

2. 조회 대상 역할의 신뢰 관계(trust policy):

   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {
         "Effect": "Allow",
         "Principal": { "AWS": "arn:aws:iam::<서버 계정>:role/<서버 인스턴스 역할>" },
         "Action": "sts:AssumeRole",
         "Condition": { "StringEquals": { "sts:ExternalId": "<외부 ID>" } }
       }
     ]
   }
   ```

   `externalId`를 쓰지 않으면 `Condition`을 뺍니다.

### 동작

- **조회**: 주기마다 리전별로 `DescribeEvents(StartTime=구간 시작, EndTime=지금, MaxRecords=100)`을 호출하고 `Marker`가 없을 때까지 다음 페이지를 읽습니다.
- **구간**: 구간 시작은 `마지막 끝 시각 - overlap`입니다. RDS가 이벤트를 발생 시각보다 조금 늦게 보여 주는 경우를 위해 겹쳐 읽고, 겹친 구간에서 다시 나온 이벤트는 `sourceArn`·`sourceType`·`sourceIdentifier`·발생 시각·`message`·범주로 만든 키로 걸러 한 번만 보냅니다. 키는 다음 구간에 다시 나올 수 있는 동안만(겹침 구간) 보관하고 리전당 최대 1만 개입니다.
- **첫 실행**: 상태가 없으면 `initialLookback`(기본 10분)만 거슬러 봅니다.
- **재시작**: 마지막 끝 시각과 겹침 구간의 중복 제거 키를 `(roleArn, region)`별로 `stateFile`에 저장하므로, 다시 시작하면 멈춘 시점부터 빈틈과 중복 없이 이어갑니다. 저장은 임시 파일에 쓴 뒤 이름을 바꾸는 방식이라 도중에 죽어도 파일이 깨지지 않습니다. 파일을 읽을 수 없으면 `.corrupt-<시각>`으로 옮기고 `initialLookback`부터 다시 시작합니다.
- **따라잡기 상한**: 마지막 끝 시각이 `maxCatchUp`(기본 24시간)보다 오래됐으면 최근 24시간만 조회하고, 건너뛴 구간을 로그에 남깁니다.
- **오류**: 권한 거부(AccessDenied), 자격 오류·만료가 나면 그 리전의 구간을 앞으로 옮기지 않고 다음 주기에 같은 구간부터 다시 조회합니다. 자격 오류면 캐시된 자격을 버리고 새로 받습니다. 같은 리전·같은 종류의 오류 로그는 10분에 한 번만 남깁니다. 스로틀링은 SDK가 지수 백오프로 최대 5번 시도하고, 그래도 실패하면 그 리전만 주기의 2배·4배·8배·16배(최대 15분) 동안 쉬었다가 다시 조회합니다.
- **전송**: 이 OpenAgent의 수집 서버 연결(프로젝트, 오브젝트)로 보냅니다. 수집 서버 연결 전이거나 전송 큐가 30초 넘게 차 있으면 보내지 못한 이벤트가 있는 리전의 구간을 유지하고 다음 주기에 다시 보냅니다. 이미 큐에 넣은 이벤트는 다시 보내지 않습니다.
- **에이전트 보호**: 조회 중 패닉이 나도 복구하고 다음 주기에 다시 시도합니다.

### 에이전시와 다른 점

| 항목 | 에이전시 | OpenAgent |
|---|---|---|
| 처음 조회 범위 | 2일 2시간 | 10분(`initialLookback`). 오래된 재시작이 새 알림으로 울리지 않게 하기 위해서입니다. |
| 재시작 뒤 | 마지막 조회 시각부터 이어감 | `stateFile`로 이어가되 최대 24시간(`maxCatchUp`) |
| 구간 겹침 | 없음 | 5분 겹쳐 읽고 중복 제거(늦게 보이는 이벤트 대비) |
| `agency` 태그 | 에이전시 서비스 id | `agencyName`(기본 `openagent`) |
| 자격 증명 | 에이전시에 등록한 역할 또는 액세스 키 | 서버의 기본 자격 체인 + 선택적 `roleArn`/`externalId` |
| 보내는 곳 | 에이전시 오브젝트 | 이 OpenAgent의 프로젝트와 오브젝트. 한 프로세스가 여러 프로젝트로 나눠 보내지는 않습니다. |

### 확인용 한 번 실행 (`rds-events-dryrun`)

실제 자격으로 조회와 변환을 확인할 때 씁니다. 같은 `awsRdsEvents` 섹션을 읽어(`enabled`는 무시) 한 주기를 실행하고, 보낼 팩을 **보내지 않고** JSON 배열로 표준 출력에 씁니다. 상태 파일을 읽거나 쓰지 않고 와탭 수집 서버에 연결하지 않습니다. 진행 상황과 오류는 표준 에러로 나옵니다.

```bash
# 서버의 기본 자격 증명으로 서울 리전 최근 24시간
./openagent rds-events-dryrun -regions ap-northeast-2 -lookback 24h > rds-events.json

# 설정 파일의 역할로 조회
./openagent rds-events-dryrun -config /opt/whatap/scrape_config.yaml -lookback 1h
```

| 플래그 | 설명 |
|---|---|
| `-config` | 읽을 `scrape_config.yaml` (기본 `$WHATAP_OPEN_HOME/scrape_config.yaml`, 없으면 건너뜀) |
| `-regions` | 쉼표로 구분한 리전. 설정보다 우선 |
| `-role-arn`, `-external-id` | 맡을 역할과 외부 ID. 설정보다 우선 |
| `-no-role` | 설정의 `roleArn`을 무시하고 기본 자격으로 조회 |
| `-lookback` | 조회 범위(기본: 설정의 `initialLookback`, 10분) |
| `-agency-name` | `agency` 태그 값 |

종료 코드는 성공 0, 실패한 리전이 있으면 1, 플래그·설정 오류 2입니다. 출력 예(값은 예시):

```json
[
  {"category": "aws_rds_event", "time": 1795575481123, "timeUTC": "2026-11-25T02:58:01.123Z",
   "tags": {"category0": "failover", "sourceArn": "arn:aws:rds:ap-northeast-2:111122223333:db:orders-db", "sourceIdentifier": "orders-db", "sourceType": "db-instance", "agency": "openagent"},
   "fields": {"message": "Multi-AZ instance failover completed."}}
]
```

`timeUTC`는 읽기 편하라고 붙인 값이며 팩에는 없습니다.

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
  endpoints:
    - address: "secure-service.example.com:443"
      path: "/metrics"
      scheme: "https"  # 명시적으로 HTTPS 지정
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

### 대상 레이블 (job, instance)

1.8.14부터 재라벨링은 OpenAgent가 붙이는 대상 레이블 `job`(targetName)과 `instance`(수집 주소)가 붙은 뒤에 적용됩니다(프로메테우스와 같은 순서). 그래서 `source_labels`로 읽거나 `replace`로 값을 바꿀 수 있습니다. 1.8.13까지는 재라벨링 뒤에 대상 레이블이 붙어 두 레이블을 바꿀 수 없었습니다. `pcode`와 `node`는 지금도 재라벨링 뒤에 붙으므로 바꿀 수 없습니다.

```yaml
metricRelabelConfigs:
  # instance를 서버 이름으로 바꾸기
  - target_label: instance
    replacement: "web-01"
    action: replace
  # 또는 instance에서 포트만 떼기
  - source_labels: [instance]
    regex: '(.*):\d+'
    target_label: instance
    replacement: '${1}'
    action: replace
```

instance 값이 바뀌면 새 시계열로 저장됩니다. 기존 instance 값으로 만든 필터와 알림은 새 값으로 다시 지정해야 합니다.

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
