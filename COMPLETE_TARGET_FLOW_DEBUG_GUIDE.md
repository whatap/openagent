# 완전한 타겟 데이터 흐름 디버깅 가이드

## 🎯 **목적**
`updateTarget`으로 업데이트된 타겟이 실제로 스크래핑에서 사용될 때까지의 전체 데이터 흐름을 추적하여, 업데이트된 metricRelabelConfigs가 제대로 전달되는지 확인합니다.

## 🔄 **전체 데이터 흐름**

```
1. ConfigMap 변경 (kubectl apply)
   ↓
2. ServiceDiscovery.discoverTargets() (15초 주기)
   ↓
3. processServiceTarget() → updateTarget() 
   ↓
4. ScraperManager.performScraping() → GetReadyTargets()
   ↓
5. createScraperTaskFromTarget()
   ↓
6. ScraperTask.Run() → ScrapeRawData 생성
   ↓
7. Processor.processRawData() → 메트릭 필터링
```

## 📋 **추가된 디버깅 로그**

### **1. Target 업데이트 시점 (processServiceTarget)**
```
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Target ID: xxx
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - metricRelabelConfigs count: X
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Config[0]: action=keep, regex=새로운regex
[DEBUG_TARGET_UPDATE] AFTER updateTarget - Target ID: xxx successfully updated
```

### **2. Target 반환 시점 (GetReadyTargets)**
```
[DEBUG_GET_TARGETS] GetReadyTargets: Found X ready targets out of Y total targets
[DEBUG_GET_TARGETS] ReadyTarget[0]: ID=xxx, URL=xxx, State=ready
[DEBUG_GET_TARGETS] ReadyTarget[0]: metricRelabelConfigs count=X
[DEBUG_GET_TARGETS] ReadyTarget[0]: First config - action=keep, regex=새로운regex
[DEBUG_GET_TARGETS] ReadyTarget[0]: LastSeen=15:04:05
```

### **3. Target 사용 시점 (performScraping)**
```
[PerformScraping01] Found X ready targets
[PerformScraping02] Target[0] ID: xxx, URL: xxx, State: ready
[PerformScraping04] Target[0] Metadata keys: [targetName type endpoint metricRelabelConfigs]
[PerformScraping05] Target[0] metricRelabelConfigs: Found X configs
[PerformScraping06] Target[0] Config[0]: action=keep, regex=새로운regex, sourceLabels=[__name__]
[PerformScraping09] Target[0] LastSeen: 15:04:05
```

### **4. ScraperTask 생성 시점 (createScraperTaskFromTarget)**
```
[CreateScraperTask] Creating scraper task for target: xxx
[CreateScraperTask] Found X metric relabel configs
[CreateScraperTask] Target metadata keys: [targetName type endpoint metricRelabelConfigs]
```

### **5. Processor 처리 시점 (processRawData)**
```
[METRIC_RELABEL] === MetricRelabelConfigs Status ===
[METRIC_RELABEL] Target: https://xxx/metrics
[METRIC_RELABEL] Total MetricRelabelConfigs: X
[METRIC_RELABEL] Config[0]: Action=keep, Regex=새로운regex
```

## 🔍 **테스트 방법**

### **1. 전체 파이프라인 추적**
```bash
kubectl logs -f deployment/whatap-open-agent -n whatap-monitoring | grep -E "(DEBUG_TARGET_UPDATE|DEBUG_GET_TARGETS|PerformScraping|CreateScraperTask|METRIC_RELABEL)"
```

### **2. 단계별 확인**
```bash
# Step 1: Target 업데이트 확인
kubectl logs -f deployment/whatap-open-agent -n whatap-monitoring | grep "DEBUG_TARGET_UPDATE"

# Step 2: Target 반환 확인  
kubectl logs -f deployment/whatap-open-agent -n whatap-monitoring | grep "DEBUG_GET_TARGETS"

# Step 3: Target 사용 확인
kubectl logs -f deployment/whatap-open-agent -n whatap-monitoring | grep "PerformScraping"

# Step 4: ScraperTask 생성 확인
kubectl logs -f deployment/whatap-open-agent -n whatap-monitoring | grep "CreateScraperTask"

# Step 5: Processor 처리 확인
kubectl logs -f deployment/whatap-open-agent -n whatap-monitoring | grep "METRIC_RELABEL"
```

### **3. ConfigMap 변경 테스트**
```bash
# 1. 현재 상태 확인
kubectl logs --tail=100 deployment/whatap-open-agent -n whatap-monitoring | grep -E "(DEBUG_TARGET_UPDATE|METRIC_RELABEL)"

# 2. CR 변경 적용
kubectl apply -f whatap-agent-updated.yaml

# 3. 15초 후 변경 확인
sleep 20
kubectl logs --tail=200 deployment/whatap-open-agent -n whatap-monitoring | grep -E "(DEBUG_TARGET_UPDATE|DEBUG_GET_TARGETS|PerformScraping06|METRIC_RELABEL)"
```

## 📊 **예상 로그 출력**

### **ConfigMap 변경 전 (오래된 regex)**
```
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Config[0]: action=keep, regex=apiserver_reuqest_total
[DEBUG_GET_TARGETS] ReadyTarget[0]: First config - action=keep, regex=apiserver_reuqest_total
[PerformScraping06] Target[0] Config[0]: action=keep, regex=apiserver_reuqest_total
[METRIC_RELABEL] Config[0]: Action=keep, Regex=apiserver_reuqest_total
```

### **ConfigMap 변경 후 (새로운 regex)**
```
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Config[0]: action=keep, regex=apiserver_(request|current|registered)_.*|etcd_.*|kubernetes_build_info
[DEBUG_GET_TARGETS] ReadyTarget[0]: First config - action=keep, regex=apiserver_(request|current|registered)_.*|etcd_.*|kubernetes_build_info
[PerformScraping06] Target[0] Config[0]: action=keep, regex=apiserver_(request|current|registered)_.*|etcd_.*|kubernetes_build_info
[METRIC_RELABEL] Config[0]: Action=keep, Regex=apiserver_(request|current|registered)_.*|etcd_.*|kubernetes_build_info
```

## 🚨 **문제 진단 체크리스트**

### **1. Target 업데이트 단계**
- [ ] `DEBUG_TARGET_UPDATE` 로그에서 새로운 regex가 나타나는가?
- [ ] `BEFORE updateTarget`과 `AFTER updateTarget` 로그가 쌍으로 나타나는가?

### **2. Target 반환 단계**
- [ ] `DEBUG_GET_TARGETS` 로그에서 업데이트된 regex가 나타나는가?
- [ ] `metricRelabelConfigs count`가 0이 아닌가?
- [ ] `LastSeen` 시간이 최근인가?

### **3. Target 사용 단계**
- [ ] `PerformScraping06` 로그에서 새로운 regex가 나타나는가?
- [ ] `metricRelabelConfigs: Found X configs`에서 X가 0이 아닌가?

### **4. ScraperTask 생성 단계**
- [ ] `CreateScraperTask` 로그에서 `Found X metric relabel configs`가 나타나는가?
- [ ] Target metadata에 `metricRelabelConfigs`가 포함되어 있는가?

### **5. Processor 처리 단계**
- [ ] `METRIC_RELABEL` 로그에서 최종적으로 새로운 regex가 나타나는가?
- [ ] `Total MetricRelabelConfigs`가 0이 아닌가?

## 🔧 **문제 시나리오별 해결책**

### **시나리오 1: Target 업데이트는 되지만 반환되지 않음**
```
✅ [DEBUG_TARGET_UPDATE] 새로운 regex 확인됨
❌ [DEBUG_GET_TARGETS] 오래된 regex 또는 타겟 없음
```
**원인**: `updateTarget` 함수에서 타겟이 제대로 저장되지 않음
**확인**: `kd.targets` 맵 업데이트 로직 점검

### **시나리오 2: Target은 반환되지만 사용되지 않음**
```
✅ [DEBUG_GET_TARGETS] 새로운 regex 확인됨
❌ [PerformScraping06] 오래된 regex 또는 설정 없음
```
**원인**: `GetReadyTargets()`와 `performScraping()` 사이의 타이밍 문제
**확인**: 두 로그의 시간 차이 확인

### **시나리오 3: Target은 사용되지만 ScraperTask에 전달되지 않음**
```
✅ [PerformScraping06] 새로운 regex 확인됨
❌ [CreateScraperTask] 설정 없음 또는 오래된 regex
```
**원인**: `createScraperTaskFromTarget`에서 메타데이터 추출 실패
**확인**: 메타데이터 타입 캐스팅 로직 점검

### **시나리오 4: ScraperTask는 생성되지만 Processor에 전달되지 않음**
```
✅ [CreateScraperTask] 새로운 regex 확인됨
❌ [METRIC_RELABEL] 오래된 regex 또는 설정 없음
```
**원인**: `ScrapeRawData` 생성 시 설정 누락
**확인**: `ScraperTask.Run()` 함수의 `ScrapeRawData` 생성 로직 점검

## 🎯 **핵심 확인 포인트**

1. **일관성**: 모든 단계에서 동일한 regex가 나타나는가?
2. **타이밍**: 각 단계의 로그 시간이 순차적인가?
3. **개수**: metricRelabelConfigs 개수가 모든 단계에서 일치하는가?
4. **신선도**: LastSeen 시간이 최근인가?

## 🚀 **성공 시나리오**

모든 단계에서 다음과 같은 일관된 로그가 나타나야 합니다:

```
15:30:15 [DEBUG_TARGET_UPDATE] Config[0]: action=keep, regex=apiserver_(request|current|registered)_.*|etcd_.*|kubernetes_build_info
15:30:30 [DEBUG_GET_TARGETS] First config - action=keep, regex=apiserver_(request|current|registered)_.*|etcd_.*|kubernetes_build_info
15:30:45 [PerformScraping06] Config[0]: action=keep, regex=apiserver_(request|current|registered)_.*|etcd_.*|kubernetes_build_info
15:30:46 [CreateScraperTask] Found 1 metric relabel configs
15:30:47 [METRIC_RELABEL] Config[0]: Action=keep, Regex=apiserver_(request|current|registered)_.*|etcd_.*|kubernetes_build_info
```

## ✅ **결론**

이 가이드를 통해 `updateTarget`으로 업데이트된 값이 실제로 스크래핑에서 사용되는 전체 과정을 추적할 수 있습니다. 각 단계별 로그를 확인하여 어느 지점에서 문제가 발생하는지 정확히 파악할 수 있습니다.

**사용자의 우려사항인 "업데이트를 제대로 했다해도 업데이트된 값을 제대로 가져가지 않으면 문제"를 완벽하게 추적할 수 있습니다!** 🔍