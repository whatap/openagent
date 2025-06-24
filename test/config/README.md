# WhatapConfig의 환경 변수

이 문서는 환경 변수가 `whatapConfig` 인스턴스와 어떻게 동기화되는지 설명합니다.

## 환경 변수 작동 방식

`whatapConfig` 인스턴스는 설정 파일을 확인하기 전에 `WHATAP_` 접두사가 있는 환경 변수를 확인합니다. 이는 환경 변수가 설정 파일의 값보다 우선한다는 것을 의미합니다.

예를 들어, `WHATAP_DEBUG=true` 환경 변수를 설정하면 `whatap.conf` 파일의 `debug` 값을 재정의합니다.

## 환경 변수 사용 방법

`whatapConfig` 인스턴스와 함께 환경 변수를 사용하려면 `WHATAP_` 접두사 뒤에 설정 키의 대문자 버전을 붙인 환경 변수를 설정하면 됩니다.

예시:
- `debug` 설정 키에 대한 `WHATAP_DEBUG`
- `server_port` 설정 키에 대한 `WHATAP_SERVER_PORT`
- `log_level` 설정 키에 대한 `WHATAP_LOG_LEVEL`

## 예제

```go
package main

import (
	"fmt"
	"os"
	whatap_config "open-agent/pkg/config"
)

func main() {
	// 환경 변수 설정
	os.Setenv("WHATAP_DEBUG", "true")
	os.Setenv("WHATAP_SERVER_PORT", "9090")
	os.Setenv("WHATAP_LOG_LEVEL", "debug")

	// Get 메서드를 사용하여 값 가져오기
	fmt.Printf("debug: %s\n", whatap_config.Get("debug"))
	fmt.Printf("server_port: %s\n", whatap_config.Get("server_port"))
	fmt.Printf("log_level: %s\n", whatap_config.Get("log_level"))

	// GetConfig 메서드를 사용하여 값 가져오기
	config := whatap_config.GetConfig()
	fmt.Printf("Debug: %v\n", config.Debug)
	fmt.Printf("ServerPort: %d\n", config.ServerPort)
	fmt.Printf("LogLevel: %s\n", config.LogLevel)

	// 정리
	os.Unsetenv("WHATAP_DEBUG")
	os.Unsetenv("WHATAP_SERVER_PORT")
	os.Unsetenv("WHATAP_LOG_LEVEL")
	whatap_config.Cleanup()
}
```

## 구현 세부 사항

환경 변수 동기화는 `WhatapConfig` 구조체의 `Get` 메서드에서 구현됩니다:

```go
func (wc *WhatapConfig) Get(key string) string {
	// 먼저 동일한 이름의 환경 변수가 있는지 확인
	envValue := os.Getenv("WHATAP_" + strings.ToUpper(key))
	if envValue != "" {
		return envValue
	}
	// 그런 다음 설정 파일 확인
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.values[key]
}
```

`GetConfig` 메서드는 `Get` 메서드를 사용하여 값을 검색하므로 환경 변수가 `Config` 구조체와 적절하게 동기화됩니다:

```go
func (wc *WhatapConfig) GetConfig() *Config {
	config := &Config{
		Debug:          isTruthy(wc.Get("debug")),
		ScrapeInterval: wc.Get("scrape_interval"),
		ScrapeTimeout:  wc.Get("scrape_timeout"),
		ServerPort:     parseIntValue(wc.Get("server_port"), 0),
		ServerHost:     wc.Get("server_host"),
		LogLevel:       wc.Get("log_level"),
		LogFile:        wc.Get("log_file"),
		EnableMetrics:  isTruthy(wc.Get("enable_metrics")),
		EnableTracing:  isTruthy(wc.Get("enable_tracing")),
		EnableLogging:  isTruthy(wc.Get("enable_logging")),
	}
	return config
}
```

## 결론

환경 변수는 `whatapConfig` 인스턴스와 적절하게 동기화됩니다. 환경 변수는 설정 파일의 값보다 우선하며, `Get` 메서드와 `GetConfig` 메서드 모두에 반영됩니다.
