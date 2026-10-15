# EC2 상태 전달: INFRA_EC2_INSTANCE_STATE

OpenAgent가 별도 EC2 상태 Exporter의 관측값을 사용자가 등록한 Server 프로젝트 라이선스마다 전달한다.
새 pack type을 추가하지 않고 기존 **ParamPack(oneway)** 을 사용한다.
설치·기본값·파일별 설정 예시는 [EC2 상태 설정 매뉴얼](ec2-instance-state-setup.md)을 참고한다.
명령 ID는 [최신 Yard 담당자 정정](https://whatap-labs.atlassian.net/browse/SERVER-2139?focusedCommentId=85949)에 따라 **56 → 1002**로 변경한다. Open Agent의 ParamPack ID 할당 범위는 **1000~1099**다. 아래 단건 payload와 oneway 방식은 [기존 Yard 담당자 회신](https://whatap-labs.atlassian.net/browse/SERVER-2139?focusedCommentId=85923)의 계약을 유지한다. 해당 과거 회신의 ID 56 및 단일 서버 매핑 제한이 아닌 최신 계약을 사용한다.
담당자 안내상 지원 수집서버는 **3.1.10 (2026-10-21 배포 예정)**이다. 담당자는 GS Demo의 Yard 3.1.10 개발 브랜치에서 **1002 수신·해지 및 56 무시**를 확인했다고 공유했다. 이는 담당자 개발 환경의 검증 결과다. 2026-10-15 GS Demo(Yard 3.1.10 개발 브랜치)에서 OpenAgent·Exporter 변경분으로 실제 EC2를 terminate해 약 26초 후 해당 Server 등록이 자동 해지되는 것을 확인했다(`purgeTerminatedInstance ... state=shutting-down matched=1`). 등록 후 받은 running 재전송으로는 해지되지 않았다. 운영 수집서버는 3.1.10 배포 후 사용한다.

## 책임과 전송 경로

```text
AWS EC2 → whatap/exporters의 ec2 상태 Exporter (/metrics)
→ 기존 ScraperTask HTTP 수집
→ Processor (metric relabel/target label 추가 전 원본)
→ ec2state.Forwarder.Forward (snapshot 검증 → 변경/주기 판단)
→ secure.SendOneway (각 목적지의 ParamPack을 즉시 byte snapshot으로 큐잉)
→ 기존 TCP sender/connection → collector → Yard
```

- 상태 Exporter: 별도 저장소 `whatap/exporters`의 `ec2/` 컴포넌트(인접 checkout 기준 `../exporters/ec2`). AWS EC2의 실제 `State.Code`/`State.Name`을 수집하고 `/metrics`로 노출한다. 기존 CloudWatch 성능 Exporter와는 별도 프로세스·포트를 사용한다.
- OpenAgent: 기존 scrape target에서 받은 원본 관측만 사용하며, 여러 라이선스 등록, 라이선스 구조 검증 및 pcode/licenseHash 해석, Exporter 상태 전달을 담당한다. Exporter 설치/기동이나 추가 HTTP probe를 수행하지 않는다.
- Yard: 목적지 pcode 안의 현재 Server 등록과 instance ID 매칭, 상태 판정 및 등록 해지.
- 최신 Yard 매핑: `32/shutting-down` 또는 `48/terminated`일 때, 수신 pcode의 현재 서버 중 `meta:instance-id`가 일치하는 서버를 **모두** 해지한다. 같은 EC2에 재설치하여 여러 oid가 남아 있어도 단일 매핑으로 제한하지 않는다. 다른 pcode나 다른 instance ID의 서버는 이 관측의 해지 대상이 아니다.
- OpenAgent는 AWS 조회, Yard 목록/관리 HTTP 호출, 대상 oid 계산/지정, 서버 등록 해지를 하지 않는다.
- `pending`, `running`, `shutting-down`, `terminated`, `stopping`, `stopped`를 모두 그대로 전달한다.
- stopped·목록 소실·수집 실패는 종료 관측을 만들지 않는다. 상태가 사라졌다고 terminated를 합성하지 않는다.
- 전송 오류는 EC2 전달에 한정하며 기존 OpenMx 메트릭 처리와 주 프로젝트 라우팅을 중단하지 않는다.

## 수신 명세

| 위치 | 필드 | 타입/의미 |
|---|---|---|
| 논리 명령명 | `INFRA_EC2_INSTANCE_STATE` | 코드/문서의 명칭. 이름 문자열 자체가 wire의 Id는 아님 |
| ParamPack | `Id` | int32, 기본 **1002**. `ec2_state_param_id`로 명시한 경우 해당 양의 정수 |
| ParamPack | `Pcode` | 해당 목적지 라이선스에서 해석한 int64 |
| ParamPack | `Oid` | OpenAgent 발신자 int32. **해지할 Server oid가 아님**. 음수도 유효 |
| ParamPack | `Time` | 관측 시각, Unix epoch milliseconds |
| ParamPack | `Request`, `Response` | 모두 0. 응답/ACK 요청 없음 |
| payload | `instanceId` | TextValue, 예: `i-0123456789abcdef0` |
| payload | `stateCode` | DecimalValue / PutLong(int64), 예: 48 |
| payload | `stateName` | TextValue, 예: `terminated` |

인스턴스 1건 × 목적지 pcode 1개당 ParamPack 1개를 보낸다. payload는 위 세 필드뿐이며 license 원문·secure key·대상 oid는 넣지 않는다.
Yard에 같은 instance ID로 여러 Server oid가 등록돼 있어도 전송 단위를 늘리거나 관측을 버리지 않는다. OpenAgent는 등록 서버 수를 조회·검사하지 않으며, 하나의 관측으로 해당 pcode의 매칭 서버 전체를 처리하는 것은 Yard의 책임이다. Exporter snapshot 안의 동일 관측 중복 제거·상충 상태 거절은 별개의 입력 검증이며 그대로 유지한다.
같은 라이선스의 중복 등록은 제거하고, 같은 pcode에 서로 다른 라이선스가 있으면 해당 scrape의 전달을 거절한다.
여러 목적지 모두 동일 collector 경로에서 라우팅 가능한 환경이어야 한다. 목적지별 collector 주소 선택은 이 기능에 포함하지 않는다.

기존 go-apm `SendOneway`와 같은 TCP frame:

```text
source byte=10 (NETSRC_ONEWAY)
version byte=0
pcode int64
licenseHash int64 (음수 허용)
pack length int32
raw ParamPack bytes
```

**기존 oneway 프로토콜은 일반 세션 암호화를 적용하지 않는다.** licenseHash와 EC2 식별/상태가 frame에 있으므로 신뢰된 collector 연결망을 사용한다.
서버 측 license 검증·권한 판단은 collector/Yard 책임이다. 클라이언트 구조 검증이 서버 인증 성공을 뜻하지 않는다.

## 최소 설정

아래 자동 기본값과 명령 ID **1002**는 **OpenAgent 1.8.11 이상**에서 사용한다. 1.8.10 이하에는 포함되어 있지 않다. Kubernetes Operator 환경은 같은 키를 CR `envs`로 넣는다(값 직접 입력 또는 Secret 참조, [설정 매뉴얼](ec2-instance-state-setup.md) 10장).

기존 OpenAgent 자신의 라이선스와 collector 설정은 유지한다. 상태 Exporter를 `scrape_config.yaml`의 target에 **한 번** 등록한 뒤, `whatap.conf`에는 다음 두 키만 추가하면 된다.

**기본값은 OFF다.** 아래 `true`는 대상 수집서버가 1002를 지원하는지(3.1.10 이상) 확인한 뒤의 활성화 예시다. 확인 전에는 `ec2_state_forwarding_enabled=false`로 둔다.

```properties
ec2_state_forwarding_enabled=true
ec2_state_licenses=<SERVER_LICENSE_A>,<SERVER_LICENSE_B>
```

라이선스 값은 예제용 placeholder이며 실제 키를 저장소·로그·Jira에 올리지 않는다.
설정은 파일 snapshot(`GetConfigMap`)에 같은 이름의 `ec2_state_*` 환경변수를 더해 읽는다. 파일에 비어 있지 않은 값이 있으면 파일 값이 우선하고, 환경변수는 없거나 빈 키만 채운다. Kubernetes Operator는 `whatap.conf`를 생성하지 않으므로 `spec.features.openAgent.envs`로 이 키를 전달한다(라이선스는 Secret 참조). `ec2_state_` 외의 환경변수는 가져오지 않으며, 원격 설정 조회용 `GetConfigMap`에는 환경변수 값을 넣지 않는다. 환경변수는 프로세스 재시작 시에만 바뀐다.
기존 파일 감시 reload 후 다음 scrape부터 새 설정을 읽는다. 이미 큐에 들어간 pack은 설정 변경으로 취소되지 않는다.

상태 Exporter의 기본 listen 주소는 `127.0.0.1:9107`이다. 아래 예시는 OpenAgent와 같은 호스트에 별도로 실행한 상태 Exporter를 기존 `scrape_config.yaml`에 등록하는 형태다. 다른 호스트/포트로 배치했다면 해당 주소로 변경한다. **OpenAgent 코드에는 이 주소나 포트를 하드코딩하지 않으며**, target을 등록하지 않은 endpoint를 탐색하지 않는다.

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

이미 등록된 상태 Exporter target이 있다면 중복 추가하지 않는다. 기존 CloudWatch 성능 Exporter는 별도 컴포넌트이므로 기존 target/포트를 유지한다. CPU나 `StatusCheckFailed` 같은 CloudWatch 성능·상태검사 메트릭만으로 EC2 lifecycle 상태를 추론하지 않는다.

### 선택 설정과 하위 호환성

| 키 | 미설정·빈 값의 기본 동작 |
|---|---|
| `ec2_state_metric_name` | `aws_ec2_instance_state`. 공백만 있어도 동일 |
| `ec2_state_exporter_urls` | 자동 source: 기존 scraper가 넘긴 모든 원본 중 지정 metric family만 처리. 공백만 있어도 동일 |
| `ec2_state_instance_id_label` | canonical metric은 `instance_id`, 명시한 custom metric은 기존 `instanceId` |
| `ec2_state_state_name_label` | canonical metric은 `state_name`, 명시한 custom metric은 기존 `stateName` |
| `ec2_state_state_code_source` | canonical metric은 `value`, 명시한 custom metric은 기존 `label` |
| `ec2_state_state_code_label` | `stateCode` (`label` source에서만 사용) |
| `ec2_state_param_id` | Yard 할당 ID **1002**. 공백만 있어도 동일 |
| `ec2_state_resend_interval` | **5분(`5m`)**. 공백만 있어도 동일 |

- `ec2_state_metric_name`을 생략하거나 `aws_ec2_instance_state`로 명시하면 동일한 snake_case label + numeric sample 계약을 사용한다. 메트릭명 앞뒤 공백은 무시하고 family 이름은 정확히 일치해야 한다.
- canonical/custom 어느 쪽이든 개별 label/source override가 기본값보다 우선한다. custom metric을 명시한 기존 사용자의 camelCase label + sample 0/1 동작은 바꾸지 않는다.
- `ec2_state_exporter_urls`를 생략하면 **추가 HTTP 요청, localhost probe, AWS 조회 없이** 이미 수집한 raw source를 사용한다. 허용할 source를 제한해야 할 때만 쉼표 구분 URL 목록을 설정한다. 명시 목록은 기존과 같이 resolved target URL(query 포함)에 정확히 일치해야 하며, 각 항목 앞뒤 공백만 무시한다. `,`, `url,`, 빈 항목이 끼어 있는 목록은 자동 모드로 확대하지 않고 `EC2_STATE_CONFIG` 오류로 전달을 막는다. URL 목록만으로 새 scrape 작업을 만들지 않는다.
- `ec2_state_param_id`에 명시한 0·음수·int32 범위 초과·해석 불가능한 값은 전송을 막는다. override는 수신 명세가 별도로 합의된 경우에만 사용한다. 테스트의 `190001`은 fixture일 뿐 운영 할당값이 아니다.
- 기존 `ec2_state_param_id=56`은 자동으로 바꾸지 않는다. 명시한 값이 기본값보다 우선하므로, 해당 설정을 제거하거나 `1002`로 변경해야 한다. ID 56으로의 자동 fallback이나 두 ID의 동시 전송은 하지 않는다.
- `ec2_state_resend_interval`은 양의 Go duration(예: `1m`, `10m`)으로 변경할 수 있으며 앞뒤 공백은 무시한다. 명시한 0·음수·해석 불가능한 값은 `EC2_STATE_CONFIG` 오류로 전달을 막는다.

## Exporter 입력

새 상태 Exporter의 고정 계약은 **gauge `aws_ec2_instance_state`**, label `instance_id`, `state_name`, `region`, sample 값은 실제 EC2 `State.Code`의 public low byte다. AWS 내부용 high byte는 Exporter가 제외한다. `region`은 OpenAgent 전달 payload에 넣지 않는다. 아래는 계약 예시이며 실제 AWS live 수집 성공의 증거가 아니다.

```text
# TYPE aws_ec2_instance_state gauge
aws_ec2_instance_state{instance_id="i-0123456789abcdef0",state_name="running",region="ap-northeast-2"} 16
aws_ec2_instance_state{instance_id="i-1123456789abcdef0",state_name="pending",region="ap-northeast-2"} 0
```

- 코드/이름 조합: **0/pending, 16/running, 32/shutting-down, 48/terminated, 64/stopping, 80/stopped**. value mode의 0은 정상 pending 관측이지 비활성 표시가 아니다. `state_name`은 필수이며 누락·불일치·잘못된 code/name·NaN/Inf는 거절한다.
- 새 Exporter 계약은 timestamp 없이 각 scrape의 최신 AWS snapshot을 제공하는 것이다. HTTP scrape 실패나 목록 소실을 terminated로 합성해서는 안 된다. OpenAgent는 timestamp가 없으면 수집 요청 시작 시각을 사용하며 원천 AWS snapshot의 최신성을 자체 증명하지 않는다.
- 입력은 Prometheus text 0.0.4의 gauge 또는 untyped를 처리한다. negotiated delimited protobuf (`io.prometheus.client.MetricFamily`)도 지원한다. OpenMetrics 전용 Content-Type은 EC2 전달에서 거절하므로 이 target은 text/plain 또는 지원 protobuf로 제공해야 한다.
- 기존 custom Exporter가 timestamp를 제공하면 수집 요청 시작 시각보다 미래이거나 5분보다 오래된 값은 거절한다.
- instance ID는 `i-`와 소문자 16진수 8자리 또는 17자리. target labels/relabeling으로 상태 identity를 덮어쓰지 않는다.
- 동일 instance의 충돌 관측, 필수 필드 누락, 잘못된 라이선스가 있으면 해당 snapshot은 **어떤 목적지도 큐잉하기 전에** 거절한다.
- 제한: exposition 2 MiB, state samples 4,096개, 목적지 64개, scrape당 큐잉 조합 16,384개. 제한 초과는 잘라 보내지 않고 오류 처리한다.

기존 custom metric의 예시(`ec2_state_metric_name=example_ec2_instance_state`를 명시한 경우):

```text
# TYPE example_ec2_instance_state gauge
example_ec2_instance_state{instanceId="i-0123456789abcdef0",stateCode="48",stateName="terminated"} 1
```

이 경우 기본 `label` mode를 유지한다. sample 1만 현재 상태이고 0은 비활성 series로 무시하며 다른 값은 거절한다. 코드가 sample 값인 custom Exporter는 종전처럼 `ec2_state_state_code_source=value`와 필요한 label override를 명시한다. 이름 label은 어느 mode에서도 필수다.

## 실패·반복 처리

- `EC2_STATE_CONFIG`: 설정/라이선스 오류. 값이나 라이선스 원문은 오류에 포함하지 않는다.
- `EC2_STATE_INPUT`: 상태/형식/시간/크기 검증 실패.
- `EC2_STATE_SEND`: 로컬 bounded queue 거절. 이미 수락된 일부 pack은 취소되지 않는다.
- 기능은 기본 OFF다. 명령 ID·재전송 주기 미설정은 차단하지 않고 각각 1002·5분을 사용한다.
- **최초 관측 / 상태 변경 / 재전송 주기 경과** 중 하나에 해당할 때만 큐잉한다. `stateCode`와 `stateName`을 직접 비교하며 timestamp는 변경 비교에서 제외한다. `licenseHash`는 라이선스 라우팅용이지 관측 비교용 hash가 아니다.
- 이력은 Processor별로 유지하고 `(Exporter URL, pcode, instanceId)`별로 분리한다. 서로 다른 Exporter가 같은 인스턴스를 보고하면 각각 전달될 수 있다. 라이선스 hash·명령 ID·발신자 OID 변경도 최초 전달처럼 처리한다.
- 재전송 기준은 **해당 목적지의 마지막 로컬 큐 수락 시각**이다. 같은 상태를 반복 수집해도 재전송 기한을 미루지 않으며 주기 설정 변경은 다음 수집에 반영된다. Go의 monotonic clock을 사용하며, 시각 역전이 관찰되면 억제하지 않고 다시 전달한다.
- 주기가 지나도 별도 timer에서 과거 pack을 재생하지 않는다. **다음 성공한 scrape에 포함된 유효한 최신 관측만** 보낸다. 수집이 느리거나 실패하면 실제 재전송도 늦어질 수 있다. 목록 소실·무응답·잘못된 snapshot에서는 캐시로 상태를 합성/재생하지 않는다.
- 큐 거절 시 실패한 항목의 상태·수락 시각은 갱신하지 않는다. 다음 유효한 수집에서 재시도하고, 이전에 수락된 목적지는 계속 억제한다. 부분 성공 뒤 실패하면 그 호출은 중단하므로 후속 항목은 다음 수집에서 처리한다.
- 재시작은 이력을 초기화한다. OFF 설정을 관찰하면 이력을 비우며, 유효한 snapshot에서 제거된 목적지 또는 해당 source에 없는 인스턴스의 이력도 제거한다. 명시 URL 목록에서 제외된 source의 이력은 제거한다. 자동 source에서는 다른 source를 수집했다는 이유로 기존 이력을 지우지 않으며, 사라진 source 자체는 다음 유효한 snapshot·reset·LRU 제거 전까지 이력만 유지한다. 다시 등장한 인스턴스는 해당 이력이 제거됐을 때 최초 관측으로 전송한다. 오류 snapshot은 이력을 갱신하지 않는다.
- 이력은 Processor당 최대 16,384개 LRU 항목으로 제한한다. 한도를 넘으면 오래 사용하지 않은 이력을 제거하고, 제거된 항목의 다음 관측은 다시 전송한다. 이 제한은 추가 전송을 허용할 뿐 새 관측을 잘라내거나 종료 상태를 만들지 않는다.
- 기존 failover 재전송 설정을 따른다. best-effort 전송이며 중복·유실 가능, durable outbox나 exactly-once 보장이 아니다.
- 로그의 `locally queued`는 로컬 수락 수다. **Yard 수신·인증·등록 해지 완료 수가 아니다.** Yard에서 오래된/중복 관측과 매핑 불일치를 방어해야 한다.

## 로컬 검증

운영 agent를 실행하지 않고 scratch HOME/WHATAP_HOME/WHATAP_OPEN_HOME 및 허용목록 환경으로 테스트한다.
Go 1.24.3 기준 핵심 명령(위 격리 환경 안에서 실행):

```sh
go test -race -mod=readonly -p=1 -parallel=1 -timeout=45s \
  ./pkg/ec2state ./pkg/processor -count=1
go vet -mod=readonly -p=1 ./pkg/ec2state ./pkg/processor
```

- 최소 두 키 설정 → HTTP fixture → 실제 ScraperTask → 기본 Processor → 실제 oneway queue: 기본 command1002·목적지·타입·필드, pending=0·상태 변경·반복 억제, 추가 probe 없음과 일반 메트릭 유지 확인. HTTP 실패·목록 소실·invalid snapshot에서 종료 관측을 합성하지 않음을 확인한다.
- `32/shutting-down → 48/terminated` 전이는 재전송 주기 전에도 각 목적지로 전달하고, 유효한 terminated 관측은 기본 5분 주기 경과 후 다시 전달한다. 인스턴스별·프로젝트별 한 pack과 세 payload 필드를 유지하며, Yard의 실제 다중 oid 해지 결과는 별도 수신 측 검증 대상이다.
- metric명 미설정·빈 값·공백·명시 canonical, 6개 상태, custom metric 하위 호환과 개별 label/source override, 무관한 CPU/StatusCheckFailed 비전송을 검증한다. 자동 A/B source의 교차·병렬 수집, 독립 5분 주기, source별 소실 prune, URL exact match와 잘못된 목록의 fail-closed도 검증한다.
- 주입 시계로 주기 직전/경계·반복 수집·주기 reload·시각 역전을 검증한다. 부분 큐 실패 재시도, 목적지 추가/제거·라이선스 교체, source 분리, 재시작, 잘못된 snapshot, 병렬 호출·LRU 상한도 검증한다.
- 기본 Processor 생성자와 config snapshot 교체: 다음 scrape의 disable 반영, 일반 메트릭의 주 프로젝트 유지 확인.
- 별도 transport tests: net.Pipe로 실제 frame decode, signed licenseHash, byte snapshot, reconnect routing, 기존 일반 frame 혼용 확인.
- 실제 AWS→Exporter tracer에서 확보한 `/metrics` capture는 아래 선택 테스트로 원본 그대로 재생할 수 있다. collector 인증·수신, Yard 처리와 최종 등록 상태는 별도 연동 검증 대상이다. tracer snapshot이나 로컬 테스트 통과는 대상 환경의 E2E를 대신하지 않는다.

### 선택적 실제 capture 재생 (네트워크 전송 없음)

`EC2_STATE_CAPTURE_FILE`이 없으면 `TestForwarderLiveCaptureWithMinimalSettings`는 skip한다. 값이 있으면 파일을 수정하지 않고 실제 sample을 decode하여, metric/label/URL 설정 없이 합성 테스트 라이선스 두 개로 `Forwarder`에 넣는다. 실제 ParamPack round-trip의 command1002, code/name/type, 목적지별 전체 fanout과 중복 억제를 검증한다. 수집시각은 재생 시각이며 과거 AWS 관측 시각을 복원하지 않는다. 실제 인스턴스 ID를 포함한 capture 파일은 저장소에 추가하지 않는다.

```sh
# 동일한 scratch HOME·offline 허용목록 환경에 이 변수 하나만 추가한다.
EC2_STATE_CAPTURE_FILE=/absolute/path/to/capture.prom \
  go test -race -mod=readonly -p=1 -parallel=1 -timeout=45s \
  ./pkg/ec2state -run '^TestForwarderLiveCaptureWithMinimalSettings$' -count=1 -v
```

이 테스트는 AWS 조회, 실제 라이선스 사용, collector 연결이나 실제 큐 전송을 하지 않는다.

관련 이슈: [OPMT-121](https://whatap-labs.atlassian.net/browse/OPMT-121), [SERVER-2139](https://whatap-labs.atlassian.net/browse/SERVER-2139).
