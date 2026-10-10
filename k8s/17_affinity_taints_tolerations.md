# 17. Размещение Pod: nodeSelector, affinity и taints/tolerations

## Какую проблему решаем

Deployment задаёт количество экземпляров и шаблон Pod, но обычно не указывает конкретный узел для каждого экземпляра. При создании Pod Scheduler должен решить, где его разместить: приложению может требоваться SSD, реплики желательно разнести по узлам, а специальные Node нужно оставить для определённых нагрузок.

**Правила размещения отвечают на два вопроса: на какие Node Pod можно поставить и какие из допустимых Node предпочтительнее.** В записи 15 `requests` определяли, помещается ли Pod по ресурсам; теперь добавляем условия о свойствах узлов, соседних Pod и ограничениях самих Node. Все эти условия участвуют в одном решении Scheduler.

## Механизм: сначала отбор, затем оценка

Scheduler рассматривает Pod, которому ещё не назначен узел. Сначала он отсеивает Node, не удовлетворяющие обязательным условиям, затем оценивает оставшиеся и выбирает узел по итоговой оценке. После назначения kubelet на этом Node обеспечивает запуск контейнеров.

```text
Deployment → ReplicaSet → новый Pod
                              ↓
            фильтрация: куда Pod можно поставить?
                              ↓
            оценка: какой допустимый Node лучше?
                              ↓
             назначение Node → kubelet → контейнеры
```

Например, первый узел не вмещает CPU request, второй не имеет обязательной метки, третий подходит. Предпочтение первого узла не вернёт его в список кандидатов: **мягкие правила работают только среди узлов, прошедших обязательные проверки**. Если подходящих узлов нет, Pod остаётся без назначения, обычно в `Pending`, пока ситуация не изменится.

Ресурсная проверка опирается на `requests` и доступный для размещения бюджет Node, а не только на текущее потребление из `kubectl top`. Низкая загрузка не отменяет нехватку бюджета requests или несовпадение selector. Этапы отбора и оценки описаны в [документации Scheduler](https://kubernetes.io/docs/concepts/scheduling-eviction/kube-scheduler/).

## Метки Node и nodeSelector

Node имеют собственные метки: например, `disk=ssd`, `environment=production` или `gpu=true`. Это метки узла, а не размещённых на нём Pod; наличие `disk=ssd` у Pod не делает его Node подходящим. Node — ресурс уровня кластера, поэтому namespace приложения не выделяет ему отдельный набор узлов.

`nodeSelector` — простое обязательное условие по меткам Node. В примере все три реплики `web` должны размещаться на узлах с `disk=ssd`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: study
spec:
  replicas: 3
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      nodeSelector:
        disk: ssd
      containers:
        - name: web
          image: nginx:1.27
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
```

`spec.selector` выбирает управляемые Pod по `app=web`, а `spec.template.spec.nodeSelector` ограничивает выбор **Node** по `disk=ssd`. Если подходящих SSD-узлов несколько, Scheduler выбирает среди них с учётом остальных условий. Если их нет, он не заменит требование на HDD ради запуска.

Все пары в `nodeSelector` должны совпасть одновременно. Например, `disk=ssd` вместе с `environment=production` требует обеих меток на одном узле. При этом selector не разносит реплики: несколько Pod могут оказаться на одном подходящем Node, если остальные условия допускают такое размещение.

## Node affinity: обязательное и предпочтительное

`nodeAffinity` тоже проверяет метки Node, но позволяет выразить набор допустимых значений и предпочтения. Два основных вида правил читаются так:

| Поле | Как участвует в выборе |
|---|---|
| `requiredDuringSchedulingIgnoredDuringExecution` | Обязательное условие: неподходящий Node исключается |
| `preferredDuringSchedulingIgnoredDuringExecution` | Предпочтение: подходящий Node получает дополнительную оценку |

Следующий фрагмент **заменяет `nodeSelector`** в `spec.template.spec` предыдущего Deployment. Он требует `environment=production` или `environment=staging`, а SSD делает предпочтением:

```yaml
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
        - matchExpressions:
            - key: environment
              operator: In
              values: [production, staging]
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        preference:
          matchExpressions:
            - key: disk
              operator: In
              values: [ssd]
```

Узел с `environment=development` не подходит даже с SSD. Узел со staging и HDD допустим, а SSD добавляет оценку допустимому узлу. `weight` задаётся от 1 до 100; это вес совпавшего предпочтения, а не процент и не гарантия выбора: итог учитывает и другие правила Scheduler.

Если SSD обязателен, выражение `disk In [ssd]` нужно поместить в required-часть. Для простого равенства это решает ту же задачу, что `nodeSelector`, но синтаксис affinity гибче. Если оставить и `nodeSelector`, и required node affinity, **обязательные условия обоих механизмов должны выполняться**; preferred-правило не ослабляет существующий selector.

В `matchExpressions` оператор `In` разрешает перечисленные значения, `NotIn` исключает их, `Exists` проверяет наличие ключа, а `DoesNotExist` — отсутствие. Несколько выражений внутри одного элемента списка `nodeSelectorTerms` соединяются через «И», а отдельные элементы этого списка — через «ИЛИ». В примере `production` и `staging` — альтернативные значения одного условия, а не две обязательные метки.

## Pod affinity и anti-affinity: учитывать соседние Pod

Node affinity спрашивает о свойствах узла, а `podAffinity` — о расположении других Pod. Например, frontend можно направлять туда, где находится backend. `podAntiAffinity` задаёт обратное отношение: избегать размещения рядом с выбранными Pod. Это помогает разносить реплики приложения, чтобы отказ одного Node не затронул их все.

Слово «рядом» определяет `topologyKey` — ключ метки **Node**, по значению которой узлы объединяются в область размещения. Для `kubernetes.io/hostname` это обычно отдельный узел, для `topology.kubernetes.io/zone` — зона, в которой может быть несколько узлов. Метки нужной топологии должны присутствовать на Node; selector соседей при этом проверяет метки **Pod**. В следующем фрагменте для шаблона `web` желательно избегать Node, на которых уже есть его реплики:

```yaml
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app: web
          topologyKey: kubernetes.io/hostname
```

`labelSelector` выбирает соседние Pod с `app=web`, а `topologyKey` задаёт, что сравниваем их размещение на уровне узла. Без `namespaces` и `namespaceSelector` поиск идёт в namespace самого Pod — здесь `study`. Namespace соседних Pod и топология их Node отвечают за разные части условия.

Если вместо `podAntiAffinity` использовать `podAffinity` и выбрать `app=backend`, аналогичное preferred-правило будет поощрять размещение рядом с backend. У обоих механизмов есть required и preferred: первый делает соседство или его отсутствие обязательным, второй лишь влияет на оценку.

**Предпочтительная anti-affinity не гарантирует «одна реплика на Node».** При трёх репликах и двух подходящих узлах соседство допустимо. Если сделать отсутствие соседних реплик обязательным через required anti-affinity по hostname, для трёх одновременно размещённых реплик потребуются как минимум три подходящих узла; иначе часть Pod останется без назначения. Синтаксис и выбор топологии описаны в [документации affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#inter-pod-affinity-and-anti-affinity).

## Taints и tolerations: ограничение со стороны Node

Selector и affinity описывают требования Pod к месту запуска. **Taint задаётся на Node и ограничивает размещение Pod без подходящих tolerations.** Например, `dedicated=gpu:NoSchedule` содержит ключ `dedicated`, значение `gpu` и эффект `NoSchedule`: новые Pod без подходящей toleration Scheduler сюда не поставит.

Toleration находится в спецификации Pod и позволяет ему выдерживать соответствующий taint. Фрагмент для `spec.template.spec`:

```yaml
tolerations:
  - key: dedicated
    operator: Equal
    value: gpu
    effect: NoSchedule
```

Здесь `Equal` требует совпадения ключа, значения и эффекта. При `operator: Exists` значение не указывают: toleration подходит для любого значения данного ключа с указанным эффектом. Если на Node несколько taints, нужно учесть каждый: одна подходящая toleration не снимает остальные ограничения.

**Toleration разрешает рассматривать Node, но не требует выбрать его и не добавляет предпочтение сама по себе.** Pod может попасть на обычный узел без такого taint, если другие условия позволяют. Снятие одного запрета также не отменяет нехватку ресурсов для requests, обязательный selector или required affinity.

### Три эффекта taint

| Эффект | Новое размещение без toleration | Уже размещённые Pod без toleration |
|---|---|---|
| `NoSchedule` | Не допускается | Продолжают работать |
| `PreferNoSchedule` | Нежелательно, но допустимо | Продолжают работать |
| `NoExecute` | Не допускается | Подлежат удалению с узла |

`PreferNoSchedule` — мягкое ограничение, поэтому его нельзя использовать как обязательную изоляцию узла. `NoExecute` влияет и на существующие Pod: подходящая toleration без `tolerationSeconds` позволяет оставаться, а с этим полем — только заданное число секунд после появления taint. Если taint снят до истечения этого времени, удаление по нему не требуется. Подробности — в [документации taints и tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/).

## Как сочетать выбор узла и разрешение

Пусть специальные узлы имеют **два независимых свойства**: метку `gpu=true` и taint `dedicated=gpu:NoSchedule`. Метка позволяет найти нужную группу, а taint ограничивает допуск. Добавление метки не создаёт taint, и добавление taint не создаёт метку. Для Pod, который должен идти именно в эту группу, нужны обе части в `spec.template.spec`:

```yaml
nodeSelector:
  gpu: "true"
tolerations:
  - key: dedicated
    operator: Equal
    value: gpu
    effect: NoSchedule
```

Selector исключает узлы без `gpu=true`, а toleration снимает соответствующий запрет на выбранных узлах. Только toleration оставила бы допустимыми обычные Node; только selector выбрал бы нужную группу, но не преодолел её taint. После сочетания этих правил Scheduler всё равно проверяет ресурсы и остальные ограничения. Метка описывает группу узлов; выдача реального GPU контейнеру требует отдельной настройки ресурсов и устройств.

```text
проверка requests + nodeSelector / required affinity + taints/tolerations
                                   ↓
                          допустимые Node
                                   ↓
        preferred affinity + PreferNoSchedule + остальные оценки
                                   ↓
                            выбранный Node
```

При чтении конфигурации сначала собери все обязательные условия, затем предпочтения. Требования Pod и ограничения Node не заменяют друг друга: разрешение пройти taint не исправляет несовпадение меток, а высокое предпочтение не делает запрещённый узел допустимым.

## Что происходит после назначения

`DuringScheduling` означает проверку при планировании, а `IgnoredDuringExecution` — отсутствие автоматического выселения из-за последующего нарушения этого affinity-условия. Если после запуска Node потерял нужную метку, обычный Pod не переносится автоматически; это верно и для `nodeSelector`. Появление более предпочтительного узла тоже не вызывает переезд уже размещённого Pod.

Изменение правил в `spec.template` Deployment меняет шаблон и запускает rollout: новые Pod планируются по новым условиям. Это замена экземпляров контроллером, а не перемещение существующего Pod между Node. `NoExecute` — отдельный механизм удаления по taint, а реакция DaemonSet на потерю метки из записи 11 связана с пересчётом нужного набора узлов его контроллером.

## Основные команды

```bash
# Метки и ограничения узлов
kubectl get nodes -L disk,environment,gpu
kubectl describe node NODE

# Фактическое размещение и причина отсутствия назначения
kubectl get pods -n study -l app=web -o wide
kubectl describe pod POD -n study
kubectl get pod POD -n study -o yaml

# Применить шаблон и изменить свойства учебного узла
kubectl apply -f web.yaml
kubectl label node NODE disk=ssd --overwrite
kubectl taint node NODE dedicated=gpu:NoSchedule
kubectl taint node NODE dedicated=gpu:NoSchedule-
```

`NODE` и `POD` — имена существующих объектов, `web.yaml` — файл с полным манифестом из примера. `-L` выводит значения выбранных меток Node, `-o wide` показывает узел каждого Pod; завершающий `-` в команде taint удаляет указанное ограничение. Метки и taints Node меняются на уровне кластера, поэтому у этих команд нет `-n study`.

## Если Pod остаётся Pending

Сначала посмотри, назначен ли ему Node: `Pending` возможен и при ожидании запуска контейнеров после назначения. Если узла ещё нет, начни с Events в `describe pod` и сопоставь сообщение с конфигурацией самого Pod и выбранных Node. Файл на диске может отличаться от принятого объекта в API, поэтому полезен `get pod -o yaml`.

| Причина в Events | Что проверить |
|---|---|
| `Insufficient cpu` или `Insufficient memory` | Requests нового Pod, allocatable Node и requests уже размещённых Pod |
| Несовпадение node selector/affinity | Метки Node и все обязательные условия Pod |
| `untolerated taint` | Ключ, значение и эффект taint, наличие подходящей toleration |
| Несовпадение pod affinity/anti-affinity | Метки и namespace соседних Pod, topologyKey и число допустимых областей |

У Node может быть несколько причин отказа сразу. Добавление toleration устранит только соответствующий taint; увеличение числа реплик или перезапуск команды apply не создаёт подходящий узел. После назначения диагностика переходит к kubelet и контейнерам, как в записи 3.

## Проверь себя

**1. Pod требует disk=ssd, но у всех Node стоит disk=hdd. Можно ли поставить его на HDD, если CPU и память свободны?**

<details>
<summary>Показать ответ</summary>

Нет. nodeSelector — обязательное условие, которое ресурсы не отменяют. Pod останется без назначения, пока не появится подходящий узел или не изменится требование.

</details>

**2. SSD указан только в preferred node affinity с weight=100. Гарантирует ли это SSD и что будет, если остались только HDD-узлы?**

<details>
<summary>Показать ответ</summary>

Гарантии нет: вес участвует в итоговой оценке вместе с другими правилами. HDD допустим, если прошёл все обязательные проверки. Если одновременно остался nodeSelector disk=ssd, HDD будет исключён уже этим selector.

</details>

**3. В одном nodeSelectorTerms-элементе стоят disk In [ssd] и environment In [production, staging]. Какое условие должен выполнить Node?**

<details>
<summary>Показать ответ</summary>

Нужны одновременно SSD и одно из двух значений метки окружения: production или staging. Выражения внутри элемента соединяются через «И», значения In — альтернативы. Отдельные элементы nodeSelectorTerms были бы альтернативными наборами условий.

</details>

**4. Чем отличаются nodeAffinity и podAffinity? Что меняет topologyKey hostname по сравнению с zone?**

<details>
<summary>Показать ответ</summary>

Node affinity проверяет метки узла, pod affinity — расположение Pod с выбранными метками. Hostname задаёт соседство на уровне отдельного Node, zone — всей зоны; в одной зоне могут находиться разные узлы.

</details>

**5. Есть три реплики и два подходящих Node. Чем отличаются preferred и required anti-affinity по app=web и hostname?**

<details>
<summary>Показать ответ</summary>

Preferred допускает соседство, поэтому третья реплика может запуститься на занятом узле. Required запрещает такое соседство: для трёх одновременно размещённых реплик нужны как минимум три подходящих Node, иначе часть останется без назначения.

</details>

**6. Pod получил toleration dedicated=gpu:NoSchedule. Обязан ли он попасть на GPU Node? Достаточно ли одной метки gpu=true на Pod?**

<details>
<summary>Показать ответ</summary>

Нет. Toleration только снимает соответствующее ограничение, обычный Node тоже может быть допустим. Для обязательного выбора группы нужен nodeSelector или required node affinity по меткам Node. Метка самого Pod не заменяет это условие.

</details>

**7. На Node с работающими Pod добавили NoSchedule, а позже NoExecute. Что происходит без подходящих tolerations?**

<details>
<summary>Показать ответ</summary>

NoSchedule запрещает новое размещение, но не удаляет уже размещённые Pod. NoExecute также приводит к их удалению с узла. Toleration для NoSchedule не совпадает с taint NoExecute, если в ней явно указан другой эффект.

</details>

**8. Уже размещённый Pod имел required node affinity по disk=ssd, затем Node получил disk=hdd. Перенесёт ли его Scheduler?**

<details>
<summary>Показать ответ</summary>

Нет. IgnoredDuringExecution не требует выселения при таком изменении меток. Новые Pod будут проверяться по текущим меткам, а существующий Pod не переезжает автоматически на другой узел.

</details>
