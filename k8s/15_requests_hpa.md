# 15. Requests, limits и HPA: ресурсы экземпляра и количество реплик

## Requests, limits и потребление

**Requests и limits задают ресурсы одного контейнера, а HPA меняет количество реплик приложения по метрикам.**

| Понятие | Что означает |
|---|---|
| `requests` | Заявленная потребность: Scheduler учитывает её при размещении. CPU request также влияет на распределение CPU при конкуренции |
| `limits.cpu` | Ограничение процессорного времени. При достижении применяется throttling |
| `limits.memory` | Ограничение памяти. При превышении возможен OOM kill и состояние `OOMKilled` |
| Фактическое потребление | Измеренная нагрузка, которую показывают метрики и `kubectl top` |

Контейнер может потреблять больше request при доступных ресурсах и подходящих limits. Request не должен превышать limit. Если задан только limit и другой request не назначен, Kubernetes использует limit как request.

CPU: `1000m = 1 CPU`, `100m = 0.1 CPU`. Память: `Mi` и `Gi` — двоичные единицы размера, отличающиеся от десятичных `M` и `G`.

Ресурсы задают в `containers[].resources`, у Deployment — внутри `spec.template.spec`. Scheduler сравнивает requests с доступным для Pod бюджетом Node (`allocatable`) и уже размещёнными requests. Например, если из `2000m` учтено `1500m`, новый Pod с request `700m` не помещается, даже при низком текущем потреблении CPU.

## Что делает HPA

HPA получает метрики, сравнивает их с целью и меняет `replicas` Deployment в пределах `minReplicas` и `maxReplicas`. Затем Deployment и ReplicaSet создают или убирают Pod.

```text
потребление → kubelet → Metrics Server → HPA → replicas Deployment → Pod
```

Для HPA по CPU или памяти нужен работающий поставщик метрик, обычно Metrics Server. HPA не меняет requests/limits и не добавляет узлы кластера. В YAML Deployment под управлением HPA стоит избегать фиксированного `replicas`: повторный `apply` может вернуть старое количество.

## Resource, Utilization и AverageValue

**В HPA два поля `type` с разными задачами:**

| Поле | Что выбирает |
|---|---|
| `metrics[].type: Resource` | Источник метрики: потребление ресурса Pod |
| `resource.name: cpu` | Какой ресурс измеряем — здесь CPU |
| `resource.target.type` | Как выражена цель: процент от request или количество ресурса |

`Resource` в HPA выбирает метрики потребления. Для этого источника доступны два типа цели:

- **`Utilization`** — процент от request. Значение цели задаётся в `averageUtilization`: при request `100m` и потреблении `60m` текущая загрузка равна `60 / 100 × 100% = 60%`. Limit и мощность Node в расчёте не участвуют. Процент может превышать 100%, если потребление больше request.
- **`AverageValue`** — количество ресурса на Pod. Значение цели задаётся в `averageValue`, например `30m` CPU. Потребление сравнивается с целью напрямую, без деления на request.

Обе цели относятся к среднему значению среди учитываемых Pod. Для простоты здесь у каждой реплики один контейнер и одинаковый request:

| CPU request каждой реплики | `Utilization`: цель 30% | `AverageValue`: цель 30m |
|---|---|---|
| `100m` | Ориентир `30m` CPU на Pod | Ориентир `30m` CPU на Pod |
| `200m` | Ориентир `60m` CPU на Pod | Ориентир `30m` CPU на Pod |

Для расчёта `Utilization` нужны requests выбранного ресурса у учитываемых контейнеров. Для `AverageValue` они не служат базой расчёта, но продолжают влиять на размещение. При источнике `Resource` учитывается суммарное потребление контейнеров Pod.

## Пример: приложение и HPA по CPU

Образ `hpa-example` выполняет вычисления при HTTP-запросах. Service нужен для подачи нагрузки.

```yaml
# cpu-demo.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cpu-demo
  namespace: study
spec:
  selector:
    matchLabels:
      app: cpu-demo
  template:
    metadata:
      labels:
        app: cpu-demo
    spec:
      containers:
        - name: app
          image: registry.k8s.io/hpa-example:latest
          ports:
            - name: http
              containerPort: 80
          resources:
            requests:
              cpu: 100m
              memory: 32Mi
            limits:
              cpu: 200m
              memory: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: cpu-demo
  namespace: study
spec:
  selector:
    app: cpu-demo
  ports:
    - port: 80
      targetPort: http
```

```yaml
# cpu-demo-hpa.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: cpu-demo
  namespace: study
spec:
  scaleTargetRef:              # Какой объект масштабировать
    apiVersion: apps/v1
    kind: Deployment
    name: cpu-demo
  minReplicas: 1
  maxReplicas: 4
  metrics:
    - type: Resource           # Источник метрики
      resource:
        name: cpu              # Выбранный ресурс
        target:
          type: Utilization    # Тип цели
          averageUtilization: 30
```

Чтобы попробовать `AverageValue`, замени только блок `target` в этом HPA:

```yaml
target:
  type: AverageValue
  averageValue: 30m
```

В одной цели задаётся одно из полей: `averageUtilization` или `averageValue`. Цель масштабирования не ограничивает потребление контейнера — для этого служат limits.

## Как HPA рассчитывает количество реплик

Допустим, сейчас работают две реплики. По метрикам каждая в среднем потребляет `55m` CPU при request `100m`. Тогда `kubectl get hpa` покажет в колонке `TARGETS` значение `55%/30%`: 55% рассчитаны из текущего потребления (`55 / 100 × 100%`), а 30% — цель из нашего YAML. Базовый расчёт количества реплик такой:

```text
desiredReplicas = ceil(currentReplicas × currentMetric / targetMetric)
ceil(2 × 55 / 30) = 4
```

Для `AverageValue` сравниваются количества ресурса: `55m / 30m` даст то же отношение. Результат ограничивается диапазоном HPA. Контроллер также учитывает готовность Pod, недостающие метрики и правила масштабирования.

**Окно стабилизации — это период, за который HPA учитывает предыдущие рекомендации по количеству реплик.** При уменьшении по умолчанию оно составляет **300 секунд**: HPA выбирает наибольшую рекомендацию за последние пять минут, чтобы не убирать Pod из-за краткого спада нагрузки. Это не таймер обязательного уменьшения через пять минут. Для увеличения реплик такое ожидание по умолчанию не применяется.

## Проверка под нагрузкой

Нужны namespace `study` и работающие метрики.

```bash
kubectl top pods -n study
kubectl apply -f cpu-demo.yaml
kubectl rollout status deployment/cpu-demo -n study --timeout=60s
kubectl apply -f cpu-demo-hpa.yaml

# Запустить нагрузку в отдельном терминале
kubectl run load-generator -n study --image=busybox:1.36 \
  --restart=Never --rm -it -- sh -c \
  'while sleep 0.01; do wget -q -O- http://cpu-demo; done'

# В другом терминале смотреть потребление и масштаб
kubectl top pods -n study -l app=cpu-demo --containers
kubectl get hpa cpu-demo -n study -w
```

## Если результат неожиданный

Начни с `kubectl describe hpa cpu-demo -n study` и Events нужного Pod.

| Наблюдение | Что проверить |
|---|---|
| `top` не работает, HPA показывает `<unknown>` | Metrics API и Metrics Server. Новым Pod нужно время для появления метрик |
| `top` работает, но `Utilization` не рассчитывается | Requests учитываемых контейнеров, `scaleTargetRef`, Conditions и Events HPA |
| Реплик меньше или больше ожидаемого | Тип и значение цели, `minReplicas`/`maxReplicas`, стабилизацию |
| HPA требует реплики, но новые Pod `Pending` | Ресурсы и правила размещения по Events Pod |
| `OOMKilled` или замедление около CPU limit | Память и OOM либо CPU throttling. Добавление реплик не исправляет любую причину |

## Проверь себя

**1. Почему Pod может остаться Pending при низком потреблении CPU на Node?**

<details>
<summary>Показать ответ</summary>

Scheduler проверяет requests относительно allocatable и уже учтённых requests. Низкая текущая нагрузка не освобождает этот бюджет.

</details>

**2. Что выбирают Resource и Utilization? Какова загрузка при request 100m и потреблении 60m?**

<details>
<summary>Показать ответ</summary>

Resource выбирает источник метрики, Utilization — цель в процентах от request. Текущая загрузка — 60%.

</details>

**3. Цели 30% и 30m совпадают при request 100m. Что изменится при request 200m?**

<details>
<summary>Показать ответ</summary>

30% будут соответствовать 60m CPU на Pod. Цель AverageValue останется 30m.

</details>

**4. Две реплики, текущая загрузка 55%, цель 30%, максимум четыре. Каков базовый расчёт?**

<details>
<summary>Показать ответ</summary>

ceil(2 × 55 / 30) = 4. Готовые Pod появятся после размещения и запуска. Расчёт сам по себе не гарантирует их готовность.

</details>

**5. HPA задаёт averageValue: 30m. Ограничит ли он CPU каждого контейнера этой величиной?**

<details>
<summary>Показать ответ</summary>

Нет. HPA меняет количество реплик по среднему потреблению. Ограничение CPU контейнера задаётся через limits.cpu.

</details>
