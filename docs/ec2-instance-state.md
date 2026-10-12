# EC2 상태 전달: INFRA_EC2_INSTANCE_STATE

OpenAgent가 Exporter의 EC2 관측값을 사용자가 등록한 Server 프로젝트 라이선스마다 전달한다.
새 pack type을 추가하지 않고 기존 **ParamPack(oneway)** 을 사용한다.
명령 ID **56**과 아래 단건 payload는 [Yard 담당자 회신](https://whatap-labs.atlassian.net/browse/SERVER-2139?focusedCommentId=85923)에서 확인한 계약이다.
담당자 안내상 지원 수집서버는 **3.1.10 (2026-10-21 배포 예정)**이며, 이 문서는 실제 Yard 구현·배포 또는 연동 검증이 완료됐다는 의미가 아니다. 구버전은 알 수 없는 명령을 버리고 해지하지 않는다는 안내이며, 실제 배포 환경에서의 검증과 구분한다.

## 책임과 전송 경로

```text
기존 ScraperTask HTTP 수집
→ Processor (metric relabel/target label 추가 전 원본)
→ ec2state.Forwarder.Forward (snapshot 검증 → 변경/주기 판단)
→ secure.SendOneway (각 목적지의 ParamPack을 즉시 byte snapshot으로 큐잉)
→ 기존 TCP sender/connection → collector → Yard
```

- OpenAgent: 여러 라이선스 등록, 라이선스 구조 검증 및 pcode/licenseHash 해석, Exporter 상태 전달.
- Yard: 목적지 pcode 안의 현재 Server 등록과 instance ID 매칭, 상태 판정 및 등록 해지.
- OpenAgent는 AWS 조회, Yard 목록/관리 HTTP 호출, 대상 oid 계산/지정, 서버 등록 해지를 하지 않는다.
- `pending`, `running`, `shutting-down`, `terminated`, `stopping`, `stopped`를 모두 그대로 전달한다.
- stopped·목록 소실·수집 실패는 종료 관측을 만들지 않는다. 상태가 사라졌다고 terminated를 합성하지 않는다.
- 전송 오류는 EC2 전달에 한정하며 기존 OpenMx 메트릭 처리와 주 프로젝트 라우팅을 중단하지 않는다.

## 수신 명세

| 위치 | 필드 | 타입/의미 |
|---|---|---|
| 논리 명령명 | `INFRA_EC2_INSTANCE_STATE` | 코드/문서의 명칭. 이름 문자열 자체가 wire의 Id는 아님 |
| ParamPack | `Id` | int32, 기본 **56**. `ec2_state_param_id`로 명시한 경우 해당 양의 정수 |
| ParamPack | `Pcode` | 해당 목적지 라이선스에서 해석한 int64 |
| ParamPack | `Oid` | OpenAgent 발신자 int32. **해지할 Server oid가 아님**. 음수도 유효 |
| ParamPack | `Time` | 관측 시각, Unix epoch milliseconds |
| ParamPack | `Request`, `Response` | 모두 0. 응답/ACK 요청 없음 |
| payload | `instanceId` | TextValue, 예: `i-0123456789abcdef0` |
| payload | `stateCode` | DecimalValue / PutLong(int64), 예: 48 |
| payload | `stateName` | TextValue, 예: `terminated` |

인스턴스 1건 × 목적지 pcode 1개당 ParamPack 1개를 보낸다. payload는 위 세 필드뿐이며 license 원문·secure key·대상 oid는 넣지 않는다.
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

## 설정

`whatap.conf`에 아래 키를 추가한다. 기존 OpenAgent 자신의 라이선스와 collector 설정은 유지한다.
이 기능은 일관된 파일 설정 snapshot(`GetConfigMap`)을 사용하므로 아래 키는 **환경변수만으로 설정하지 않는다**.
기존 파일 감시 reload 후 다음 scrape부터 새 설정을 읽는다. 이미 큐에 들어간 pack은 설정 변경으로 취소되지 않는다.

```properties
# 지원 Yard 배포와 Exporter 실제 출력 검증 전에는 false 유지
 ec2_state_forwarding_enabled=false
 ec2_state_exporter_urls=http://127.0.0.1:9106/metrics
 ec2_state_metric_name=example_ec2_instance_state
# 선택 사항: 생략/빈 값이면 Yard 할당 ID 56
 ec2_state_param_id=56
 ec2_state_licenses=<SERVER_LICENSE_A>,<SERVER_LICENSE_B>
# 선택 사항: 생략/빈 값이면 5분
 ec2_state_resend_interval=5m
```

앞의 공백은 선택 사항이다. 라이선스 값은 예제용 placeholder이며 실제 키를 저장소·로그·Jira에 올리지 않는다.
`ec2_state_param_id`는 미설정·빈 값·공백만 있는 값이면 **56**을 사용한다. 명시한 0·음수·int32 범위 초과·해석 불가능한 값은 전송을 막는다. override는 수신 명세가 별도로 합의된 경우에만 사용한다. 테스트의 `190001`은 fixture일 뿐 운영 할당값이 아니다.
`ec2_state_resend_interval`의 기본값은 **5분(`5m`)**이다. 미설정·빈 값·공백만 있는 값은 기본값을 사용하므로 최초·변경 전송을 차단하지 않는다. 양의 Go duration(예: `1m`, `10m`)으로 변경할 수 있으며 앞뒤 공백은 무시한다. 명시한 0·음수·해석 불가능한 값은 `EC2_STATE_CONFIG` 오류로 전달을 막는다.
지원 Yard 배포와 실제 Exporter 출력을 검증하고, 라이선스와 Exporter URL/metric을 설정한 뒤에만 `ec2_state_forwarding_enabled=true`로 바꾼다. 기본 명령 ID 56과 재전송 주기 5분은 별도 설정 없이 적용된다.

Exporter는 기존 `scrape_config.yaml`의 target에도 등록해야 한다. `ec2_state_exporter_urls`만으로 새 수집 작업을 만들지 않는다.
여러 URL은 쉼표로 구분하며, resolved target URL(query 포함)과 정확히 일치해야 한다.

```yaml
features:
  openAgent:
    enabled: true
    targets:
      - targetName: ec2-state
        type: StaticEndpoints
        enabled: true
        endpoints:
          - address: "127.0.0.1:9106"
            path: "/metrics"
            scheme: "http"
            interval: "60s"
            timeout: "10s"
```

위 주소·metric은 형식 예제이지 실제 운영 Exporter를 확인한 값이 아니다. 표준 CloudWatch 성능 메트릭만으로 EC2 lifecycle 상태를 추론하지 않는다.

## Exporter 입력

기본 입력은 Prometheus text 0.0.4의 gauge 또는 untyped다. negotiated delimited protobuf (`io.prometheus.client.MetricFamily`)도 지원한다.
OpenMetrics 전용 Content-Type은 EC2 전달에서 거절하므로 이 target은 text/plain 또는 지원 protobuf로 제공해야 한다.

```text
# TYPE example_ec2_instance_state gauge
example_ec2_instance_state{instanceId="i-0123456789abcdef0",stateCode="48",stateName="terminated"} 1
```

- 기본 label mode: sample 1만 현재 상태, 0은 비활성 series로 무시. 다른 값/NaN/Inf는 거절.
- `ec2_state_instance_id_label` 기본 `instanceId`, `ec2_state_state_code_label` 기본 `stateCode`, `ec2_state_state_name_label` 기본 `stateName`.
- 코드가 sample 값인 Exporter는 `ec2_state_state_code_source=value`로 설정한다. 이 경우 0은 pending 관측이며 비활성 표시가 아니다. stateName label은 여전히 필수다.
- 코드/이름 조합: 0/pending, 16/running, 32/shutting-down, 48/terminated, 64/stopping, 80/stopped.
- instance ID는 `i-`와 소문자 16진수 8자리 또는 17자리. target labels/relabeling으로 상태 identity를 덮어쓰지 않는다.
- timestamp가 있으면 수집 요청 시작 시각보다 미래이거나 5분보다 오래된 값은 거절한다. 없으면 수집 요청 시작 시각을 사용하며, Exporter cache 원천의 최신성까지 증명하는 것은 아니다.
- 동일 instance의 충돌 관측, 필수 필드 누락, 잘못된 라이선스가 있으면 해당 snapshot은 **어떤 목적지도 큐잉하기 전에** 거절한다.
- 제한: exposition 2 MiB, state samples 4,096개, 목적지 64개, scrape당 큐잉 조합 16,384개. 제한 초과는 잘라 보내지 않고 오류 처리한다.

## 실패·반복 처리

- `EC2_STATE_CONFIG`: 설정/라이선스 오류. 값이나 라이선스 원문은 오류에 포함하지 않는다.
- `EC2_STATE_INPUT`: 상태/형식/시간/크기 검증 실패.
- `EC2_STATE_SEND`: 로컬 bounded queue 거절. 이미 수락된 일부 pack은 취소되지 않는다.
- 기능은 기본 OFF다. 명령 ID·재전송 주기 미설정은 차단하지 않고 각각 56·5분을 사용한다.
- **최초 관측 / 상태 변경 / 재전송 주기 경과** 중 하나에 해당할 때만 큐잉한다. `stateCode`와 `stateName`을 직접 비교하며 timestamp는 변경 비교에서 제외한다. `licenseHash`는 라이선스 라우팅용이지 관측 비교용 hash가 아니다.
- 이력은 Processor별로 유지하고 `(Exporter URL, pcode, instanceId)`별로 분리한다. 서로 다른 Exporter가 같은 인스턴스를 보고하면 각각 전달될 수 있다. 라이선스 hash·명령 ID·발신자 OID 변경도 최초 전달처럼 처리한다.
- 재전송 기준은 **해당 목적지의 마지막 로컬 큐 수락 시각**이다. 같은 상태를 반복 수집해도 재전송 기한을 미루지 않으며 주기 설정 변경은 다음 수집에 반영된다. Go의 monotonic clock을 사용하며, 시각 역전이 관찰되면 억제하지 않고 다시 전달한다.
- 주기가 지나도 별도 timer에서 과거 pack을 재생하지 않는다. **다음 성공한 scrape에 포함된 유효한 최신 관측만** 보낸다. 수집이 느리거나 실패하면 실제 재전송도 늦어질 수 있다. 목록 소실·무응답·잘못된 snapshot에서는 캐시로 상태를 합성/재생하지 않는다.
- 큐 거절 시 실패한 항목의 상태·수락 시각은 갱신하지 않는다. 다음 유효한 수집에서 재시도하고, 이전에 수락된 목적지는 계속 억제한다. 부분 성공 뒤 실패하면 그 호출은 중단하므로 후속 항목은 다음 수집에서 처리한다.
- 재시작은 이력을 초기화한다. OFF 설정을 관찰하면 이력을 비우며, 유효한 snapshot에서 제거된 목적지/Exporter 또는 해당 source에 없는 인스턴스의 이력도 제거한다. 다시 등장한 항목은 최초 관측으로 전송한다. 오류 snapshot은 이력을 갱신하지 않는다.
- 이력은 Processor당 최대 16,384개 LRU 항목으로 제한한다. 한도를 넘으면 오래 사용하지 않은 이력을 제거하고, 제거된 항목의 다음 관측은 다시 전송한다. 이 제한은 추가 전송을 허용할 뿐 새 관측을 잘라내거나 종료 상태를 만들지 않는다.
- 기존 failover 재전송 설정을 따른다. best-effort 전송이며 중복·유실 가능, durable outbox나 exactly-once 보장이 아니다.
- 로그의 `locally queued`는 로컬 수락 수다. **Yard 수신·인증·등록 해지 완료 수가 아니다.** Yard에서 오래된/중복 관측과 매핑 불일치를 방어해야 한다.

## 로컬 검증

운영 agent를 실행하지 않고 scratch HOME/WHATAP_HOME/WHATAP_OPEN_HOME 및 허용목록 환경으로 테스트한다.
Go 1.24.3 기준 핵심 명령(위 격리 환경 안에서 실행):

```sh
go test -race -mod=readonly -p=1 -parallel=1 -timeout=45s \
  ./pkg/ec2state ./pkg/processor github.com/whatap/gointernal/net/secure -count=1
go vet -mod=readonly ./pkg/ec2state ./pkg/processor github.com/whatap/gointernal/net/secure
(cd gointernal && go test -race -mod=readonly -p=1 -timeout=45s ./net/secure -count=1)
```

- HTTP fixture → 실제 ScraperTask → Processor → 실제 oneway queue: 목적지·타입·필드, 반복 수집 억제·상태 변경 전달과 일반 메트릭 유지 확인.
- 주입 시계로 주기 직전/경계·반복 수집·주기 reload·시각 역전을 검증한다. 부분 큐 실패 재시도, 목적지 추가/제거·라이선스 교체, source 분리, 재시작, 잘못된 snapshot, 병렬 호출·LRU 상한도 검증한다.
- 기본 Processor 생성자와 config snapshot 교체: 다음 scrape의 disable 반영, 일반 메트릭의 주 프로젝트 유지 확인.
- 별도 transport tests: net.Pipe로 실제 frame decode, signed licenseHash, byte snapshot, reconnect routing, 기존 일반 frame 혼용 확인.
- 실제 Exporter 스키마, collector 인증·수신, Yard 처리와 최종 등록 상태는 별도 연동 검증 대상이다. 로컬 fixture 통과를 운영 E2E 완료로 보지 않는다.

관련 이슈: [OPMT-121](https://whatap-labs.atlassian.net/browse/OPMT-121), [SERVER-2139](https://whatap-labs.atlassian.net/browse/SERVER-2139).
