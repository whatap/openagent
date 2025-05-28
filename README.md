# OpenAgent (오픈에이전트)

프로메테우스 엔드포인트에서 메트릭을 수집하고 와탭 서버로 전송하는 Go 기반 에이전트입니다.

## 개요

OpenAgent는 프로메테우스 엔드포인트에서 메트릭을 스크래핑하고, 이를 처리하여 와탭 서버로 전송하는 역할을 합니다. 자바 에이전트를 기반으로 Go 언어로 재구현되었으며, 유사한 아키텍처와 기능을 제공합니다.

## 아키텍처

에이전트는 다음과 같은 주요 컴포넌트로 구성되어 있습니다:

- **스크래퍼(Scraper)**: 대상 시스템에서 메트릭을 수집합니다.
- **프로세서(Processor)**: 수집된 메트릭을 처리하고 OpenMx 형식으로 변환합니다.
- **센더(Sender)**: 처리된 메트릭을 와탭 서버로 전송합니다.
- **설정 관리자(Config Manager)**: 에이전트의 설정을 관리합니다.
- **HTTP 클라이언트(HTTP Client)**: 대상 시스템에 HTTP 요청을 보내 메트릭을 수집합니다.
- **변환기(Converter)**: 프로메테우스 메트릭을 OpenMx 형식으로 변환합니다.

## 디렉토리 구조

```
openagent/
├── cmd/
│   └── agent/
│       └── main.go       # 메인 애플리케이션 진입점
├── pkg/
│   ├── client/           # HTTP 요청을 위한 클라이언트
│   ├── config/           # 설정 관리
│   ├── converter/        # 프로메테우스 메트릭 변환기
│   ├── model/            # 데이터 모델
│   ├── processor/        # 수집된 메트릭 처리기
│   ├── scraper/          # 메트릭 스크래퍼
│   └── sender/           # 처리된 메트릭 전송기
├── go.mod                # Go 모듈 정의
└── README.md             # 현재 파일
```

## 빌드 및 실행

### 사전 요구사항

- Go 1.16 이상

### 빌드 방법

```bash
# 의존성 다운로드
go mod tidy

# 에이전트 빌드
go build -o openagent ./cmd/agent
```

### 실행 방법

```bash
# WHATAP_HOME 환경 변수 설정 (선택사항)
export WHATAP_HOME=/path/to/whatap/home

# WHATAP_LICENSE 환경 변수 설정 (필수)
export WHATAP_LICENSE=x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502

# WHATAP_HOST 환경 변수 설정 (필수)
export WHATAP_HOST=15.165.146.117

# WHATAP_PORT 환경 변수 설정 (필수)
export WHATAP_PORT=6600

# 에이전트 실행
./openagent
```

## 샘플 데이터 전송

테스트 목적으로 샘플 데이터를 와탭 서버로 직접 전송할 수 있습니다:

```bash
# 환경 변수 설정 (필수)
export WHATAP_LICENSE=x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502
export WHATAP_HOST=15.165.146.117
export WHATAP_PORT=6600

# 샘플 데이터 전송 실행
go run direct_sample_sender.go
```

## 설정

에이전트는 `$WHATAP_HOME/scrape_config.yaml` 위치의 YAML 파일을 통해 설정됩니다. 설정 파일의 구조는 다음과 같습니다:

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

## 라이센스

이 프로젝트는 MIT 라이센스를 따릅니다 - 자세한 내용은 LICENSE 파일을 참조하세요.
