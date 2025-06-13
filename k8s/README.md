# OpenAgent 쿠버네티스 배포 가이드

이 가이드는 OpenAgent를 쿠버네티스 클러스터에 배포하는 방법을 제공합니다.

## 사전 요구 사항

- 로컬 머신에 Docker 설치
- 쿠버네티스 클러스터 접근 권한
- 클러스터와 통신하도록 구성된 `kubectl` 명령줄 도구
- 라이센스 키, 호스트, 포트 정보가 있는 WHATAP 계정

## 배포 구성

### 옵션 1: create-whatap-secret.sh 스크립트 사용 (권장)

1. 제공된 스크립트를 사용하여 시크릿 생성:
   ```bash
   ./k8s/create-whatap-secret.sh <WHATAP_LICENSE> <WHATAP_HOST> <WHATAP_PORT>
   ```

   예시:
   ```bash
   ./k8s/create-whatap-secret.sh x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502 15.165.146.117 6600
   ```

   이 스크립트는 제공된 값으로 `whatap-credentials`라는 쿠버네티스 시크릿을 생성합니다.

### 옵션 2: 수동 시크릿 생성

1. kubectl을 사용하여 직접 시크릿 생성:
   ```bash
   kubectl create secret generic whatap-credentials \
       --from-literal=license=<WHATAP_LICENSE> \
       --from-literal=host=<WHATAP_HOST> \
       --from-literal=port=<WHATAP_PORT>
   ```

(선택 사항) 스크래핑 구성을 조정하기 위해 `deployment.yaml`의 ConfigMap을 사용자 정의합니다.

## 쿠버네티스에 배포

### 옵션 1: deploy-openagent.sh 스크립트 사용 (권장)

OpenAgent를 배포하는 가장 쉬운 방법은 제공된 deploy-openagent.sh 스크립트를 사용하는 것입니다:

```bash
./k8s/deploy-openagent.sh <WHATAP_LICENSE> <WHATAP_HOST> <WHATAP_PORT>
```

예시:
```bash
./k8s/deploy-openagent.sh x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502 15.165.146.117 6600
```

이 스크립트는 다음을 수행합니다:
1. Whatap 자격 증명 시크릿 생성
2. OpenAgent 배포
3. 배포가 준비될 때까지 대기

### 옵션 2: 수동 배포

수동으로 배포하려면 다음 단계를 따르세요:

1. Whatap 자격 증명 시크릿 생성 (위의 "배포 구성" 섹션 참조)

2. 쿠버네티스 매니페스트 적용:
   ```bash
   kubectl apply -f k8s/deployment.yaml
   ```

3. 배포 확인:
   ```bash
   kubectl get pods -l app=openagent
   ```

4. 로그 확인:
   ```bash
   kubectl logs -l app=openagent
   ```

## 구성 사용자 정의

OpenAgent는 메트릭을 스크래핑할 대상을 결정하기 위해 구성 파일(`scrape_config.yaml`)을 사용합니다. 이 구성은 ConfigMap에 저장되어 컨테이너에 마운트됩니다.

구성을 업데이트하려면:

1. `deployment.yaml`의 ConfigMap 편집
2. 변경 사항 적용:
   ```bash
   kubectl apply -f k8s/deployment.yaml
   ```
3. 변경 사항을 적용하기 위해 배포 재시작:
   ```bash
   kubectl rollout restart deployment openagent
   ```

## 문제 해결

배포에 문제가 발생하면 다음을 확인하세요:

1. 파드 상태:
   ```bash
   kubectl describe pod -l app=openagent
   ```

2. 컨테이너 로그:
   ```bash
   kubectl logs -l app=openagent
   ```

3. ServiceAccount에 올바른 권한이 있는지 확인:
   ```bash
   kubectl auth can-i get pods --as=system:serviceaccount:default:openagent-sa
   kubectl auth can-i list services --as=system:serviceaccount:default:openagent-sa
   kubectl auth can-i watch endpoints --as=system:serviceaccount:default:openagent-sa
   ```

4. 시크릿이 존재하고 올바른 값을 포함하는지 확인:
   ```bash
   kubectl get secret whatap-credentials -o yaml
   ```

## 제거

클러스터에서 OpenAgent를 제거하려면:

```bash
kubectl delete -f k8s/deployment.yaml
```

## Docker 이미지 빌드

Docker 이미지 빌드에 관한 자세한 내용은 [docker-build.md](docker-build.md) 파일을 참조하세요.
