# processServiceTarget updateTarget 디버깅 가이드

## 🎯 **추가된 디버깅 로그**

사용자가 의심한 `processServiceTarget`에서 `kd.updateTarget(target)` 전후의 문제를 추적하기 위해 상세한 디버깅 로그를 추가했습니다.

## 📋 **추가된 로그 위치**

### **1. Ready 주소 처리 (라인 498-516)**
```go
// Debug log before updateTarget
logutil.Printf("DEBUG_TARGET_UPDATE", "BEFORE updateTarget - Target ID: %s", targetID)
logutil.Printf("DEBUG_TARGET_UPDATE", "BEFORE updateTarget - Target URL: %s", url)
logutil.Printf("DEBUG_TARGET_UPDATE", "BEFORE updateTarget - metricRelabelConfigs count: %d", len(endpointConfig.MetricRelabelConfigs))
if len(endpointConfig.MetricRelabelConfigs) > 0 {
    for i, config := range endpointConfig.MetricRelabelConfigs {
        if configMap, ok := config.(map[string]interface{}); ok {
            logutil.Printf("DEBUG_TARGET_UPDATE", "BEFORE updateTarget - Config[%d]: action=%v, regex=%v", 
                i, configMap["action"], configMap["regex"])
        }
    }
} else {
    logutil.Printf("DEBUG_TARGET_UPDATE", "BEFORE updateTarget - No metricRelabelConfigs found")
}

kd.updateTarget(target)

// Debug log after updateTarget
logutil.Printf("DEBUG_TARGET_UPDATE", "AFTER updateTarget - Target ID: %s successfully updated", targetID)
```

### **2. NotReady 주소 처리 (라인 548-566)**
```go
// Debug log before updateTarget (NotReady)
logutil.Printf("DEBUG_TARGET_UPDATE", "BEFORE updateTarget (NotReady) - Target ID: %s", targetID)
logutil.Printf("DEBUG_TARGET_UPDATE", "BEFORE updateTarget (NotReady) - Target URL: %s", url)
logutil.Printf("DEBUG_TARGET_UPDATE", "BEFORE updateTarget (NotReady) - metricRelabelConfigs count: %d", len(endpointConfig.MetricRelabelConfigs))
// ... 상세 설정 로깅 ...

kd.updateTarget(target)

// Debug log after updateTarget (NotReady)
logutil.Printf("DEBUG_TARGET_UPDATE", "AFTER updateTarget (NotReady) - Target ID: %s successfully updated", targetID)
```

## 🔍 **테스트 방법**

### **1. 실시간 로그 모니터링**
```bash
kubectl logs -f deployment/whatap-open-agent -n whatap-monitoring | grep "DEBUG_TARGET_UPDATE"
```

### **2. ConfigMap 변경 테스트**
```bash
# 1. 현재 로그 확인
kubectl logs --tail=50 deployment/whatap-open-agent -n whatap-monitoring | grep "DEBUG_TARGET_UPDATE"

# 2. CR 변경 적용
kubectl apply -f whatap-agent-updated.yaml

# 3. 15초 후 새 로그 확인
sleep 20
kubectl logs --tail=100 deployment/whatap-open-agent -n whatap-monitoring | grep "DEBUG_TARGET_UPDATE"
```

### **3. 전체 파이프라인 추적**
```bash
# 모든 관련 로그를 시간순으로 확인
kubectl logs -f deployment/whatap-open-agent -n whatap-monitoring | grep -E "(GetScrapeConfigs|discoverTargets|DEBUG_TARGET_UPDATE|CreateScraperTask|METRIC_RELABEL)"
```

## 📊 **예상 로그 출력**

### **ConfigMap 변경 전 (오래된 regex)**
```
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Target ID: kube-apiserver-default-kubernetes-https-0-0
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Target URL: https://10.21.20.74:443/metrics
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - metricRelabelConfigs count: 1
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Config[0]: action=keep, regex=apiserver_reuqest_total
[DEBUG_TARGET_UPDATE] AFTER updateTarget - Target ID: kube-apiserver-default-kubernetes-https-0-0 successfully updated
```

### **ConfigMap 변경 후 (새로운 regex)**
```
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Target ID: kube-apiserver-default-kubernetes-https-0-0
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Target URL: https://10.21.20.74:443/metrics
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - metricRelabelConfigs count: 1
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Config[0]: action=keep, regex=apiserver_(request|current|registered)_.*|etcd_.*|kubernetes_build_info
[DEBUG_TARGET_UPDATE] AFTER updateTarget - Target ID: kube-apiserver-default-kubernetes-https-0-0 successfully updated
```

## 🔧 **문제 진단 체크리스트**

### **1. Target 생성 시점 확인**
- [ ] `BEFORE updateTarget` 로그에서 metricRelabelConfigs count가 0이 아닌가?
- [ ] `BEFORE updateTarget` 로그에서 regex가 최신 값인가?
- [ ] Ready와 NotReady 주소 모두에서 동일한 설정이 사용되는가?

### **2. updateTarget 과정 확인**
- [ ] `BEFORE updateTarget`과 `AFTER updateTarget` 로그가 쌍으로 나타나는가?
- [ ] updateTarget 호출 중에 오류가 발생하지 않는가?
- [ ] Target ID가 일관되게 유지되는가?

### **3. 설정 전파 확인**
- [ ] ConfigMap 변경 후 15초 이내에 새로운 regex가 로그에 나타나는가?
- [ ] `GetScrapeConfigs` 로그와 `DEBUG_TARGET_UPDATE` 로그의 설정이 일치하는가?
- [ ] `CreateScraperTask` 로그와 `DEBUG_TARGET_UPDATE` 로그의 설정이 일치하는가?

## 🚨 **문제 시나리오별 진단**

### **시나리오 1: metricRelabelConfigs count가 0**
```
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - metricRelabelConfigs count: 0
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - No metricRelabelConfigs found
```
**원인**: EndpointConfig 파싱 과정에서 metricRelabelConfigs가 누락됨
**확인**: `parseEndpointConfig` 함수 로그 확인

### **시나리오 2: 오래된 regex 지속**
```
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Config[0]: action=keep, regex=apiserver_reuqest_total
```
**원인**: ConfigManager에서 최신 설정을 읽지 못함
**확인**: `GetScrapeConfigs` 로그에서 reload 성공 여부 확인

### **시나리오 3: updateTarget 후 설정 손실**
```
[DEBUG_TARGET_UPDATE] BEFORE updateTarget - Config[0]: action=keep, regex=새로운regex
[DEBUG_TARGET_UPDATE] AFTER updateTarget - Target ID: xxx successfully updated
[CreateScraperTask] Found 0 metric relabel configs  ← 문제!
```
**원인**: updateTarget 과정에서 메타데이터 손실
**확인**: `updateTarget` 함수 내부 로직 점검

## 🎯 **핵심 확인 포인트**

1. **ConfigMap → EndpointConfig**: 최신 설정이 제대로 파싱되는가?
2. **EndpointConfig → Target**: metricRelabelConfigs가 Target 메타데이터에 포함되는가?
3. **updateTarget**: Target 업데이트 과정에서 데이터 손실이 없는가?
4. **Target → ScraperTask**: 업데이트된 Target에서 ScraperTask로 설정이 전달되는가?

## 🚀 **다음 단계**

이 디버깅 로그를 통해 정확히 어느 단계에서 문제가 발생하는지 파악할 수 있습니다. 문제가 발견되면 해당 단계의 코드를 더 자세히 분석하여 근본 원인을 찾을 수 있습니다.

**사용자의 의심이 정확했다면, `updateTarget` 전후에서 metricRelabelConfigs의 변화를 명확히 추적할 수 있을 것입니다!** 🔍