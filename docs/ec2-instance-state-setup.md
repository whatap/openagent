# EC2 상태 Exporter · OpenAgent 설정 매뉴얼

## 적용 전 확인

이 문서는 **OpenAgent 1.8.11 이상**과 **EC2 State Exporter 0.1.0 이상**을 기준으로 합니다. OpenAgent 1.8.11부터 자동 기본값·ParamPack ID 1002·Operator 환경변수 설정을 지원합니다. **OpenAgent 1.8.10 이하에서는 아래 최소 설정만으로 동작하지 않습니다.**

Yard 담당자가 안내한 지원 버전은 수집서버 **3.1.10, 2026-10-21 배포 예정**입니다. 담당자는 GS Demo 개발 브랜치에서 ID 1002 수신·해지 및 구 ID 56 무시를 확인했다고 공유했습니다. 2026-10-15 같은 환경에서 실제 EC2를 terminate해 약 26초 후 Server 등록이 자동 해지되는 것도 확인했습니다. 운영·고객 환경은 수집서버 버전을 따로 확인해야 합니다. 대상 수집서버의 지원 여부와 연동 검증을 확인하기 전에는 `ec2_state_forwarding_enabled=false`를 유지합니다.

이 매뉴얼의 기본 배치는 **Linux EC2 호스트에서 Exporter와 OpenAgent를 함께 실행하는 방식**입니다. 기존 OpenAgent의 주 프로젝트 라이선스·수집서버 연결은 정상 동작하고 있다고 가정합니다. Kubernetes에서 Whatap Operator로 OpenAgent를 설치했다면 2.1의 AWS 권한을 확인한 뒤 **10장**을 따릅니다.

## 1. 사용자가 설정할 항목

상태 Exporter를 설치한 뒤 아래 두 파일만 설정합니다.

| 파일 | 설정할 내용 |
| --- | --- |
| `scrape_config.yaml` | 상태 Exporter 주소를 수집 대상으로 한 번 등록 |
| `whatap.conf` | 관리할 Server 프로젝트 라이선스 목록과 기능 활성화 여부 |

**메트릭명, 레이블명, 상태 코드 해석 방식, 명령 번호, 재전송 주기는 입력하지 않아도 됩니다.** 이미 수집 대상으로 등록한 주소를 `whatap.conf`에 다시 적을 필요도 없습니다.

```text
AWS EC2 상태 조회
  → EC2 State Exporter: /metrics
  → OpenAgent: 등록된 Server 프로젝트별로 상태 전달
  → Yard: 해당 프로젝트의 서버 식별 및 등록 해지 판단
```

- EC2 State Exporter는 CPU·네트워크를 수집하는 기존 CloudWatch Exporter와 별도 프로그램입니다. 기존 CloudWatch Exporter만 설치되어 있다면 상태 Exporter도 설치해야 합니다.
- Exporter 하나는 인증된 AWS 계정의 **한 리전 전체 EC2 상태**를 조회합니다. 모든 EC2에 각각 설치할 필요는 없습니다. 여러 계정·리전을 자동 탐색하지 않으며, ASG 소속만 골라 조회하는 기능도 없습니다.
- OpenAgent는 관측 상태를 등록한 각 Server 프로젝트에 전달합니다. pcode 입력이나 `instanceId → oid` 매핑 설정은 필요하지 않습니다.
- 이 기능은 와탭의 Server 등록을 정리하기 위한 것입니다. AWS EC2를 중지하거나 삭제하지 않습니다.

## 2. EC2 State Exporter 실행

### 2.1 AWS 권한

EC2 인스턴스 역할(IAM role)에 다음 읽기 권한이 필요합니다. 이미 허용되어 있다면 중복 추가하지 않습니다.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "ec2:DescribeInstances",
      "Resource": "*"
    }
  ]
}
```

Exporter는 AWS SDK의 기본 인증 경로를 사용합니다. EC2 역할을 사용하는 환경에서는 Exporter 전용 설정에 Access Key·Secret Key를 다시 입력하지 않습니다. `whatap.conf`에도 AWS 키를 넣지 않습니다. 별도 프로필이나 역할 위임을 쓰는 환경은 그 인증 경로에 필요한 설정·권한을 준비해야 합니다.

AWS 리전은 SDK 환경변수·프로필 설정을 우선 사용하고, 없으면 EC2 메타데이터(IMDS)에서 확인합니다. 의도한 리전과 기존 환경 설정이 일치하는지 확인하세요.

### 2.2 바이너리 준비와 실행

공식 바이너리(linux, 정적 링크)를 내려받습니다. arm64 호스트는 경로의 `amd64`를 `arm64`로 바꿉니다. `latest/` 경로도 있지만 버전 경로를 권장합니다.

```sh
mkdir -p bin
curl -fLo bin/ec2_state_exporter https://repo.whatap.io/exporter/ec2_state_exporter/0.1.0/amd64/ec2_state_exporter
chmod +x bin/ec2_state_exporter
./bin/ec2_state_exporter --version
./bin/ec2_state_exporter
```

기본 주소는 `http://127.0.0.1:9107/metrics`입니다. 실행 인자나 별도 Exporter 설정 파일은 필요하지 않습니다. 위 명령은 현재 터미널에서 계속 실행되므로, 아래 확인 명령은 다른 터미널에서 실행합니다. 상시 운영 시에는 설치 환경의 서비스 관리 방식으로 프로세스를 유지해야 합니다. 위 명령 자체가 서비스 설치나 재부팅 자동 시작을 설정하지는 않습니다.

리전 자동 확인이 불가능하거나 다른 리전을 조회하려는 경우에만 실행 인자를 지정합니다.

```sh
./bin/ec2_state_exporter --region=ap-northeast-2
```

두 실행 명령(`--region` 유무)은 대안입니다. 같은 포트에서 두 프로세스를 동시에 실행하지 않습니다.

### 2.3 실제 출력 확인

```sh
curl --fail --show-error --max-time 15 http://127.0.0.1:9107/metrics
```

아래는 출력 **형식 예시**입니다. ID는 설명용이며 실제 환경에서는 다른 값이 나옵니다.

```text
# HELP aws_ec2_instance_state EC2 lifecycle state code reported by DescribeInstances.
# TYPE aws_ec2_instance_state gauge
aws_ec2_instance_state{instance_id="i-0123456789abcdef0",region="ap-northeast-2",state_name="running"} 16
```

| 항목 | 의미 |
| --- | --- |
| `aws_ec2_instance_state` | EC2 lifecycle 상태 메트릭 |
| `instance_id` | EC2 인스턴스 ID |
| `state_name` | EC2 상태 이름 |
| `region` | 조회 리전. OpenAgent의 Yard 전달 payload에는 포함하지 않음 |
| 숫자 값 | AWS `State.Code`의 public low byte. 위 예시는 `16 = running` |

| 숫자 값 | 상태 |
| --- | --- |
| 0 | pending |
| 16 | running |
| 32 | shutting-down |
| 48 | terminated |
| 64 | stopping |
| 80 | stopped |

`0`은 정상적인 `pending` 상태입니다. 비활성 메트릭으로 무시하지 않습니다. CPU나 `StatusCheckFailed` 지표는 위 상태 메트릭을 대신할 수 없습니다.

## 3. OpenAgent 설정 파일 위치 확인

기존 서비스가 실제 사용하는 디렉터리를 확인한 뒤 파일을 편집합니다. **두 파일의 기준 환경변수가 서로 다릅니다.**

| 파일 | 일반 호스트 실행에서 읽는 위치 | 환경변수가 없을 때 |
| --- | --- | --- |
| `whatap.conf` | `$WHATAP_HOME/whatap.conf` | 프로세스 작업 디렉터리의 `whatap.conf` |
| `scrape_config.yaml` | `$WHATAP_OPEN_HOME/scrape_config.yaml` | 프로세스 작업 디렉터리의 `scrape_config.yaml` |

두 경로를 같은 디렉터리로 운영해도 됩니다. 다만 현재 터미널에서 환경변수를 바꾸는 것만으로 이미 실행 중인 서비스의 경로가 바뀌지는 않습니다. 서비스 실행 설정을 기준으로 확인하세요.

변경 전 두 파일을 백업합니다. 기존 OpenAgent 자신의 라이선스, 수집서버 주소와 다른 수집 대상은 그대로 유지합니다. `whatap.conf`와 백업에는 라이선스가 있으므로 읽기 권한을 제한하고, 채팅·로그·저장소에 올리지 않습니다.

## 4. 수집 대상 등록: scrape_config.yaml

기존 `features.openAgent.targets` 목록에 `ec2-state` 항목을 한 번 추가합니다. 이미 등록했다면 이 단계는 생략합니다.

아래는 **상태 Exporter 대상만 포함한 파일 예시**입니다. 기존 파일 전체를 이 내용으로 덮어쓰거나 `features`·`openAgent` 키를 중복 생성하지 마세요. 기존 YAML 구조에 항목을 병합해야 합니다.

```yaml
features:
  openAgent:
    enabled: true
    targets:
      - targetName: ec2-state
        type: StaticEndpoints
        enabled: true
        endpoints:
          - address: "127.0.0.1:9107"
            path: "/metrics"
            scheme: "http"
            interval: "60s"
            timeout: "15s"
```

- `interval: "60s"`는 이 매뉴얼의 **권장 수집 간격**입니다. OpenAgent의 전역 기본값이나 아래의 5분 재전송 주기와는 다릅니다.
- Exporter의 AWS 전체 조회 제한은 10초이므로 수집 HTTP timeout은 그보다 긴 `15s`로 지정합니다.
- 기존 CloudWatch Exporter의 target은 유지합니다. 그 주소를 이 상태 Exporter 주소로 바꾸지 않습니다.
- `targetName`은 수집 대상의 이름입니다. AWS 인스턴스 ID나 Server 프로젝트 이름을 넣는 자리가 아닙니다.

## 5. 관리 라이선스 등록: whatap.conf

### 5.1 활성화 전 준비 설정

기존 `whatap.conf`에 다음 두 키를 추가합니다. 같은 키가 이미 있다면 새 줄을 중복 추가하지 말고 기존 값을 수정합니다.

```properties
# 지원 수집서버와 연동 검증을 확인할 때까지 비활성 유지
ec2_state_forwarding_enabled=false
ec2_state_licenses=<SERVER_LICENSE_A>,<SERVER_LICENSE_B>
```

`<SERVER_LICENSE_A>`, `<SERVER_LICENSE_B>` 전체를 각각 관리할 Server 프로젝트의 실제 라이선스로 바꿉니다. 꺾쇠나 따옴표를 남기지 않습니다. 위 라이선스는 **OpenAgent 자신의 주 프로젝트 라이선스를 대체하는 값이 아닙니다.**

- 프로젝트가 하나면 라이선스 하나만, 여러 개면 쉼표로 구분해 한 줄에 입력합니다.
- 마지막에 쉼표를 붙이거나 중간에 빈 항목을 넣지 않습니다.
- 각 라이선스에서 pcode를 자동 해석하므로 pcode를 별도로 입력하지 않습니다.
- 등록할 프로젝트는 기존 OpenAgent가 연결한 수집서버 경로로 라우팅 가능해야 합니다. 프로젝트마다 다른 수집서버 주소를 지정하는 기능은 없습니다.
- `ec2_state_*` 키는 같은 이름의 환경변수로도 지정할 수 있습니다. Kubernetes Operator 환경에서 사용하는 방식이며 10장에서 설명합니다. `whatap.conf`에 비어 있지 않은 값이 있으면 파일 값이 우선하고, 환경변수 변경은 프로세스를 재시작해야 반영됩니다.

### 5.2 지원 수집서버 확인 후 활성화

상태 메트릭 수집과 대상 수집서버의 command 1002 지원·연동 검증을 확인한 뒤, **위 설정의 `false`만 `true`로 변경**합니다. 활성 상태의 최소 설정은 다음과 같습니다.

```properties
ec2_state_forwarding_enabled=true
ec2_state_licenses=<SERVER_LICENSE_A>,<SERVER_LICENSE_B>
```

이 설정은 실제 Server 등록 해지로 이어질 수 있습니다. 관리할 프로젝트가 맞는지 확인한 뒤 적용합니다. [Yard 담당자의 최신 정정](https://whatap-labs.atlassian.net/browse/SERVER-2139?focusedCommentId=85949)에 따라 `32/shutting-down` 또는 `48/terminated`이고, 해당 pcode의 현재 서버 중 `meta:instance-id`가 일치하는 서버는 **모두 등록 해지 대상**입니다. 같은 EC2에 에이전트를 재설치하여 이전 oid와 새 oid가 함께 남아 있어도 모두 대상이며, 정확히 한 대여야 한다는 이전 제한은 없습니다. 다른 프로젝트나 다른 인스턴스는 해당 관측의 대상이 아닙니다. OpenAgent는 서버 목록이나 해지할 oid를 고르지 않고 인스턴스 관측만 전달하며, 최종 처리는 Yard가 담당합니다.

## 6. 입력하지 않아도 적용되는 기본값

아래 키는 정상적인 신규 Exporter 연결에서는 `whatap.conf`에 추가하지 않습니다.

| 설정 키 | 생략 시 동작 |
| --- | --- |
| `ec2_state_forwarding_enabled` | `false`에 해당하는 비활성 상태 |
| `ec2_state_licenses` | 기본 목적지 없음. 활성화하려면 Server 라이선스 필요 |
| `ec2_state_metric_name` | `aws_ec2_instance_state` |
| `ec2_state_instance_id_label` | `instance_id` |
| `ec2_state_state_name_label` | `state_name` |
| `ec2_state_state_code_source` | `value`: 메트릭의 숫자 값을 상태 코드로 사용 |
| `ec2_state_state_code_label` | 기본 `value` 방식에서는 사용하지 않음 |
| `ec2_state_exporter_urls` | 기존 scraper가 수집한 원본에서 해당 메트릭을 처리. 주소 재등록·추가 탐색 없음 |
| `ec2_state_param_id` | `1002` (Open Agent 할당 범위 `1000~1099`) |
| `ec2_state_resend_interval` | `5m` |

위 레이블·값 해석 기본값은 신규 `aws_ec2_instance_state` 계약 기준입니다. 직접 제작한 다른 메트릭명을 명시한 경우에는 기존 사용자 설정과 하위 호환 동작을 따릅니다.

**명령 ID 변경:** 최신 Yard 담당자 안내에 따라 `56`에서 `1002`로 변경했습니다. 기존 `ec2_state_param_id=56` 설정이 있다면 해당 줄을 제거해 새 기본값을 사용하거나 `ec2_state_param_id=1002`로 변경합니다. 명시한 ID를 자동 교정하거나 구 ID로 fallback하지 않습니다. OpenAgent 1.8.11 이상과 수집서버의 `1002` 지원을 함께 확인해야 합니다. 1.8.10 이하의 기본값은 바뀌지 않습니다.

**과거 예제 설정이 남아 있다면 확인하세요.** 명시한 설정이 기본값보다 우선하므로, 신규 Exporter로 전환하면서 예전 메트릭명·camelCase 레이블·`label` 방식이 남으면 자동 기본값을 쓰지 못합니다. 이 Exporter를 기본값으로 연결하려면 아래 항목의 불필요한 기존 override를 제거합니다.

```text
ec2_state_metric_name
ec2_state_instance_id_label
ec2_state_state_name_label
ec2_state_state_code_source
ec2_state_state_code_label
ec2_state_exporter_urls
```

의도적으로 다른 Exporter나 URL 제한을 운영하고 있다면 설정을 일괄 삭제하지 말고 기존 사용처부터 확인합니다.

## 7. 설정 반영과 정상 동작 확인

1. 신규 바이너리를 처음 적용하거나 교체했다면 설치 방식에 맞게 Exporter·OpenAgent를 재시작합니다. 서비스 이름은 설치마다 다르므로 기존 실행 설정을 사용합니다.
2. 일반 호스트 실행에서는 설정 파일 변경을 감시해 다시 읽습니다. 라이선스·활성화 설정은 reload 후 다음 scrape부터 사용합니다. scrape target 추가는 설정 동기화와 대상 갱신 로그를 함께 확인합니다.
3. `GET /metrics`의 HTTP 200과 `aws_ec2_instance_state` 샘플을 확인합니다. HTTP 200이어도 해당 계정·리전에 인스턴스가 없으면 샘플은 없을 수 있습니다.
4. OpenAgent 로그에서 `EC2_STATE_CONFIG`, `EC2_STATE_INPUT`, `EC2_STATE_SEND` 오류가 없는지 확인합니다. 상태를 큐에 넣은 수집에서는 INFO 로그 `EC2_STATE_FORWARD ... locally queued=<건수> from <수집 주소>`가 남습니다. 최초·변경·재전송 시점에만 남으므로 동일 상태가 반복되는 수집마다 나오지는 않습니다.
5. 실제 수신·등록 해지 여부는 Yard 로그와 대상 Server 프로젝트의 에이전트 목록을 재조회해 확인합니다.

`locally queued`는 **OpenAgent의 로컬 큐 수락 건수**입니다. 수집서버 수신이나 등록 해지 성공 건수가 아닙니다. 구버전 Yard는 명령을 무시할 수 있어 OpenAgent에 오류가 없다는 사실만으로 지원 여부를 확인할 수 없습니다.

### 전송 시점

| 상황 | 동작 |
| --- | --- |
| 처음 관측한 상태 | 해당 수집에서 전달 |
| 상태 코드·이름 변경 | 변경을 확인한 수집에서 전달 |
| 동일 상태 반복 | 마지막 로컬 큐 수락 후 5분 전까지 억제 |
| 동일 상태로 5분 경과 | 다음 성공한 수집의 유효한 상태를 다시 전달 |
| 조회 실패·목록에서 사라짐 | 과거 상태를 재생하거나 `terminated`를 합성하지 않음 |

'변경 시 전달'은 AWS 이벤트를 즉시 받는 방식이 아니라 **다음 scrape에서 변경을 발견했을 때**를 뜻합니다. 재시작하면 비교 이력이 초기화되어 다시 전달할 수 있습니다. 여러 Exporter나 OpenAgent가 같은 인스턴스를 수집하면 중복 전달될 수 있습니다.

`32/shutting-down`을 보낸 뒤에도 수집을 계속합니다. AWS가 `48/terminated`를 반환하면 다음 scrape에서 상태 변경으로 즉시 전달하고, 이후에도 유효한 terminated 관측이 있으면 위 주기로 다시 전달합니다. shutting-down 때 해지된 Server가 에이전트 생존으로 재등록된 경우의 재정리는 Yard가 수행합니다. 목록에서 사라진 인스턴스를 임의로 terminated로 만들거나 마지막 값을 무기한 재생하지는 않습니다.

## 8. 필요할 때만 바꾸는 설정

### 재전송 주기 변경

같은 상태를 10분마다 재확인하려면 다음 한 줄을 추가합니다. 최초 관측·상태 변경 전달은 유지됩니다.

```properties
ec2_state_resend_interval=10m
```

수집 간격은 계속 `scrape_config.yaml`의 `interval`로 관리합니다. `0`, 음수, 해석할 수 없는 값은 전달 오류를 일으킵니다.

### 여러 수집 대상 중 특정 Exporter만 허용

기본 자동 처리를 제한해야 할 때만 URL을 지정합니다.

```properties
ec2_state_exporter_urls=http://127.0.0.1:9107/metrics
```

이 값은 **이미 등록된 수집 대상의 허용 목록**입니다. 이 키만 설정해도 새로운 수집 작업이 생기지는 않습니다. 실제 resolved URL의 scheme·주소·port·path·query와 정확히 일치해야 합니다. 목록을 쓰면 쉼표로 구분하되 빈 항목이나 마지막 쉼표를 넣지 않습니다.

## 9. 다른 호스트·컨테이너에서 수집하는 경우

`127.0.0.1`은 OpenAgent 프로세스가 속한 네트워크 공간입니다. 같은 물리 서버라도 컨테이너가 다르면 기본 loopback으로 접근하지 못할 수 있습니다.

- Exporter의 `--web.listen-address`를 OpenAgent가 접근할 수 있는 사설 주소와 포트로 지정합니다.
- `scrape_config.yaml`의 `address`를 그 주소로 바꿉니다. 메트릭명·레이블 설정은 그대로 생략합니다.
- 이 Exporter에는 TLS·HTTP 인증이 없습니다. 불특정 외부에 공개하지 말고 방화벽·보안 그룹으로 OpenAgent의 접근만 허용합니다.
- Kubernetes에서 Whatap Operator가 OpenAgent를 관리한다면 생성된 ConfigMap·Deployment를 직접 수정하지 말고 10장처럼 `WhatapAgent` CR에 설정합니다.

## 10. Kubernetes: Whatap Operator로 설정

Operator가 OpenAgent를 관리하는 환경에서는 Pod 안의 `whatap.conf`나 Operator가 생성한 `whatap-open-agent-config` ConfigMap·Deployment를 직접 수정하지 않습니다. Operator가 다시 맞추면서 수동 변경을 되돌립니다. 설정은 모두 `WhatapAgent` CR의 `spec.features.openAgent`에 넣습니다.

| 설정할 내용 | CR 항목 |
| --- | --- |
| 상태 Exporter 수집 대상 | `targets` (`StaticEndpoints`) |
| 기능 활성화·관리할 Server 라이선스 | `envs` (값 직접 입력, 필요하면 Secret 참조) |
| 이 기능을 포함한 OpenAgent 이미지 | `imageVersion` (1.8.11 이상) |

`envs`로 넣은 `ec2_state_*` 값은 같은 이름의 `whatap.conf` 키와 같게 동작합니다. Pod 안 `whatap.conf`에 같은 키가 비어 있지 않은 값으로 있으면 파일 값이 우선합니다. **1.8.10 이하 OpenAgent는 이 환경변수를 읽지 않으므로 1.8.11 이상 이미지가 필요합니다.** 아래 예시는 Operator 기본 namespace `whatap-monitoring`과 CR 이름 `whatap` 기준입니다. 설치 환경의 이름으로 바꿉니다.

### 10.1 EC2 State Exporter 배포

Exporter 공식 컨테이너 이미지는 아직 없습니다. 2.2의 공식 바이너리를 담은 이미지를 준비해 `<EC2_STATE_EXPORTER_IMAGE>`에 지정합니다. 아래 예시는 이미지의 실행 명령이 Exporter 바이너리라고 가정합니다.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ec2-state-exporter
  namespace: whatap-monitoring
  # EKS IRSA를 사용할 때만 주석을 해제합니다.
  # annotations:
  #   eks.amazonaws.com/role-arn: arn:aws:iam::<ACCOUNT_ID>:role/<ROLE_NAME>
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ec2-state-exporter
  namespace: whatap-monitoring
  labels:
    app: ec2-state-exporter
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ec2-state-exporter
  template:
    metadata:
      labels:
        app: ec2-state-exporter
    spec:
      serviceAccountName: ec2-state-exporter
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: exporter
          image: <EC2_STATE_EXPORTER_IMAGE>
          args:
            - --web.listen-address=0.0.0.0:9107
            - --region=ap-northeast-2
          ports:
            - name: metrics
              containerPort: 9107
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
---
apiVersion: v1
kind: Service
metadata:
  name: ec2-state-exporter
  namespace: whatap-monitoring
spec:
  selector:
    app: ec2-state-exporter
  ports:
    - name: metrics
      port: 9107
      targetPort: metrics
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: ec2-state-exporter
  namespace: whatap-monitoring
spec:
  podSelector:
    matchLabels:
      app: ec2-state-exporter
  policyTypes: ["Ingress"]
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: whatap-open-agent
      ports:
        - protocol: TCP
          port: 9107
```

- 다른 Pod의 OpenAgent가 접근해야 하므로 `--web.listen-address=0.0.0.0:9107`로 실행합니다. 인증·TLS가 없으므로 NetworkPolicy로 OpenAgent Pod만 허용합니다. NetworkPolicy는 이를 지원하는 CNI(Calico 등)에서만 적용됩니다.
- Pod에서는 EC2 메타데이터 조회가 막혀 있을 수 있으므로 `--region`을 조회할 리전으로 지정합니다.
- AWS 인증에는 2.1의 `ec2:DescribeInstances` 권한을 가진 IAM 역할을 사용합니다. EKS에서는 ServiceAccount의 IRSA 주석(위 예시의 주석 처리 부분) 또는 EKS Pod Identity로 연결합니다.
- AWS 밖의 클러스터(설치형 데모 등)에서는 같은 권한만 가진 전용 Access Key를 Secret으로 넣습니다. 키가 셸 기록에 남지 않도록 파일로 입력합니다.

  ```sh
  umask 077
  vi ./aws.env    # AWS_ACCESS_KEY_ID=..., AWS_SECRET_ACCESS_KEY=... 두 줄
  kubectl -n whatap-monitoring create secret generic ec2-state-exporter-aws --from-env-file=./aws.env
  rm -f ./aws.env
  ```

  그리고 Deployment의 `exporter` 컨테이너에 다음을 추가합니다.

  ```yaml
  envFrom:
    - secretRef:
        name: ec2-state-exporter-aws
  ```

배포 후 응답을 확인합니다.

```sh
kubectl -n whatap-monitoring rollout status deploy/ec2-state-exporter
kubectl -n whatap-monitoring port-forward svc/ec2-state-exporter 9107:9107
# 다른 터미널에서 실행
curl --fail --show-error --max-time 15 http://127.0.0.1:9107/metrics
```

### 10.2 WhatapAgent CR 설정

변경 전 CR을 백업합니다. 백업 파일은 권한을 제한해 보관합니다.

```sh
umask 077
kubectl get whatapagent whatap -o yaml > "whatapagent-backup-$(date +%Y%m%d%H%M%S).yaml"
kubectl edit whatapagent whatap
```

`spec.features.openAgent`에 아래 항목을 추가합니다. 기존 `envs`·`targets` 항목은 지우지 말고 목록에 이어서 추가합니다. `kubectl patch --type merge`는 목록 전체를 교체하므로 기존 항목이 있으면 사용하지 않습니다. CR을 Helm values나 배포 YAML로 관리한다면 그 원본에도 같은 내용을 반영해야 다음 배포에서 되돌아가지 않습니다.

```yaml
spec:
  features:
    openAgent:
      enabled: true
      imageVersion: "1.8.11"
      envs:
        - name: ec2_state_forwarding_enabled
          value: "false"
        - name: ec2_state_licenses
          value: "<SERVER_LICENSE_A>,<SERVER_LICENSE_B>"
      targets:
        - targetName: ec2-state
          type: StaticEndpoints
          enabled: true
          endpoints:
            - address: "ec2-state-exporter.whatap-monitoring.svc:9107"
              path: "/metrics"
              scheme: "http"
              interval: "60s"
```

- `imageVersion`은 1.8.11 이상을 지정합니다. 사설 레지스트리를 쓰면 `customImageFullName`에 이미지 전체 주소를 지정합니다.
- 환경변수 값은 문자열이므로 `"false"`·`"true"`처럼 따옴표를 붙입니다. 처음에는 `"false"`로 적용합니다.
- `ec2_state_licenses`는 5.1과 같이 쉼표로 구분해 한 줄로 넣습니다. 이 값은 CR과 OpenAgent Deployment에 그대로 저장되어 해당 리소스를 조회할 수 있는 사용자에게 보입니다. Operator의 주 라이선스를 CR `spec.license`에 직접 넣는 것과 같은 수준입니다. 노출 범위를 줄이려면 10.3의 Secret 방식을 사용합니다.
- CR의 `endpoints`에는 `timeout` 항목이 없습니다. 이 경로의 기본 수집 timeout은 60초로 Exporter 조회 제한(10초)보다 길어 따로 지정하지 않습니다.
- 메트릭명·레이블·명령 ID·재전송 주기는 6장의 기본값을 사용합니다. 바꿔야 할 때만 같은 이름의 항목을 `envs`에 추가합니다(예: `ec2_state_resend_interval`).

### 10.3 (선택) 라이선스를 Secret으로 분리

라이선스를 CR에 그대로 두지 않으려면 Secret을 만들고 `value` 대신 `valueFrom`으로 참조합니다. 라이선스가 셸 기록에 남지 않도록 파일로 입력합니다. 작성 규칙은 5.1과 같고, 파일 끝의 줄바꿈은 무시됩니다.

```sh
umask 077
vi ./ec2-state-licenses    # 한 줄: <SERVER_LICENSE_A>,<SERVER_LICENSE_B>
kubectl -n whatap-monitoring create secret generic whatap-ec2-state --from-file=licenses=./ec2-state-licenses
rm -f ./ec2-state-licenses
```

10.2 예시의 `ec2_state_licenses` 항목을 아래 내용으로 바꿉니다.

```yaml
- name: ec2_state_licenses
  valueFrom:
    secretKeyRef:
      name: whatap-ec2-state
      key: licenses
```

OpenAgent에는 두 방식 모두 같은 환경변수로 전달되므로 동작은 같습니다. 차이는 값이 저장되는 위치와 변경 반영 방식(10.5)입니다.

### 10.4 반영 확인

```sh
kubectl -n whatap-monitoring rollout status deploy/whatap-open-agent
# Operator가 생성한 수집 설정에 ec2-state 대상이 있는지 확인
kubectl -n whatap-monitoring get configmap whatap-open-agent-config -o jsonpath='{.data.scrape_config\.yaml}'
# 환경변수 이름만 확인합니다. 값은 출력하지 않습니다.
kubectl -n whatap-monitoring get deploy whatap-open-agent \
  -o jsonpath='{range .spec.template.spec.containers[?(@.name=="whatap-open-agent")].env[*]}{.name}{"\n"}{end}'
# 오류 로그 확인
kubectl -n whatap-monitoring logs deploy/whatap-open-agent --since=10m | grep -E 'EC2_STATE_(CONFIG|INPUT|SEND)'
```

실제 수신·등록 해지 여부는 7장과 같이 Yard와 대상 Server 프로젝트에서 확인합니다.

### 10.5 활성화·변경·비활성화

환경변수는 실행 중에 다시 읽지 않습니다. 값이 바뀌면 Pod가 새로 시작되어야 반영됩니다.

| 작업 | 방법 | 반영 |
| --- | --- | --- |
| 활성화 | 지원 수집서버 확인 후 CR의 `ec2_state_forwarding_enabled` 값을 `"true"`로 변경 | Operator가 Deployment를 갱신해 Pod 재시작 |
| 라이선스 변경 (직접 입력) | CR의 `ec2_state_licenses` 값 수정 | Operator가 Deployment를 갱신해 Pod 재시작 |
| 라이선스 변경 (Secret) | 아래 명령으로 Secret 갱신 후 OpenAgent Pod 삭제 | Secret만 바꾸면 Pod가 자동 재시작되지 않음 |
| 비활성화 | 값을 `"false"`로 변경 | Pod 재시작 후 다음 처리부터 중단 |

Secret 방식에서 라이선스를 바꿀 때:

```sh
umask 077
vi ./ec2-state-licenses
kubectl -n whatap-monitoring create secret generic whatap-ec2-state --from-file=licenses=./ec2-state-licenses \
  --dry-run=client -o yaml | kubectl apply -f -
rm -f ./ec2-state-licenses
kubectl -n whatap-monitoring delete pod -l app=whatap-open-agent
```

## 11. 문제 해결과 비활성화

| 증상 또는 오류 | 확인할 내용 |
| --- | --- |
| 9107 연결 거절 | Exporter 프로세스·listen 주소·포트, 컨테이너 네트워크 공간 확인 |
| `EC2_STATE_EXPORTER: AWS region is unavailable` | AWS 환경/프로필 리전 또는 IMDS 접근 확인. 필요 시 `--region` 지정 |
| HTTP 503 / `EC2_STATE_EXPORTER: collection failed` | 동일 실행 사용자·인증 경로의 AWS 자격 증명과 `ec2:DescribeInstances` 권한, 리전·네트워크, 조회 제한 확인. 응답 문구만으로 세부 원인을 단정하지 않음 |
| HTTP 503 / `EC2_STATE_EXPORTER: scrape already in progress` | 같은 Exporter에 중복 target이나 동시 수집이 있는지 확인 |
| HTTP 200인데 상태 샘플 없음 | 잘못된 계정·리전 또는 인스턴스가 없는 결과인지 확인. 종료 상태를 임의 생성하지 않음 |
| `EC2_STATE_CONFIG` | 라이선스 누락·오타·빈 항목·동일 pcode의 서로 다른 라이선스, 잘못된 선택 설정 확인 |
| `EC2_STATE_INPUT` | 메트릭 형식·필수 레이블·코드/이름 일치·수집 데이터 크기와 건수 확인 |
| `EC2_STATE_SEND` | 로컬 전송 큐 거절. 기존 수집서버 연결과 큐 상태 확인. 다음 유효한 scrape에서 재시도 |
| 수집은 되지만 전달이 안 됨 | OpenAgent가 1.8.11 이상인지, 실제 읽는 `whatap.conf`인지, 기능이 활성화됐는지, 과거 override가 남았는지 확인 |
| Kubernetes에서 전달이 안 됨 | 10.4의 환경변수 이름, OpenAgent 이미지가 1.8.11 이상인지, Secret을 쓰면 Secret 이름·key, Pod 안 `whatap.conf`에 같은 키가 있는지 확인 |
| `locally queued`인데 Server가 남아 있음 | Yard 지원 버전·수신 로그·목적지 pcode·현재 `meta:instance-id` 매칭 확인. running/stopped는 해지 대상이 아님 |

Exporter는 전체 조회를 10초로 제한하며 최대 64페이지·원본 인스턴스 레코드 4,096개를 처리합니다. 조회 실패·한도 초과·상태 충돌 시 부분 결과 대신 HTTP 503을 반환합니다. OpenAgent도 상태 4,096건, 목적지 64개, 한 scrape의 상태×목적지 조합 16,384개 상한을 적용합니다. 한도를 넘으면 일부만 잘라 전송하지 않습니다.

AWS 조회 결과에서 이미 사라진 종료 인스턴스는 복구해 전달할 수 없습니다. 이 방식은 상태 polling이며 모든 종료 이벤트의 영구 보존·전달을 보장하지 않습니다. 인스턴스가 없거나 일시적으로 조회되지 않는다는 이유로 종료를 추정하지 않습니다.

비활성화하려면 기존 키를 다음과 같이 수정합니다.

```properties
ec2_state_forwarding_enabled=false
```

설정 reload 후 다음 처리부터 상태 전달을 중단합니다. 일반 메트릭 수집은 유지되고, 이미 큐에 들어간 전송은 취소되지 않습니다. AWS 조회도 중단하려면 해당 `ec2-state` target을 비활성화하고 Exporter 프로세스를 별도로 중지합니다. 기존 CloudWatch target이나 OpenAgent 전체를 중지할 필요는 없습니다.

Operator 환경에서는 10.5와 같이 CR의 값을 `"false"`로 바꿉니다.

## 참고 자료

- OpenAgent 상세 계약: `docs/ec2-instance-state.md`
- Exporter 실행·수집 제한: `whatap/exporters` 저장소의 `ec2/README.md`
- Yard 담당자 초기 payload 명세(ID 56 당시 회신이며, 최신 ID는 1002): https://whatap-labs.atlassian.net/browse/SERVER-2139?focusedCommentId=85923
- Yard 담당자 최신 정정(ID 1002, 동일 instance ID의 매칭 서버 전체 해지): https://whatap-labs.atlassian.net/browse/SERVER-2139?focusedCommentId=85949

이전에 수집한 실제 EC2 상태 4건의 원본을 ID 1002 변경 후 테스트용 목적지 두 개로 다시 재생하여 command 1002 ParamPack 8개가 정확히 만들어지는 것을 확인했습니다. 이는 **Exporter 출력·OpenAgent 변환 검증**이며, 실제 collector 수신이나 Yard 등록 해지 검증 완료를 뜻하지 않습니다.
