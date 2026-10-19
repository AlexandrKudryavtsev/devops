# 17. Размещение Pod: nodeSelector, affinity и taints/tolerations

## Какую проблему решаем

Deployment задаёт количество экземпляров и шаблон Pod, но обычно не указывает конкретный узел для каждого экземпляра. При создании Pod Scheduler должен решить, где его разместить: приложению может требоваться SSD, реплики желательно разнести по узлам, а специальные Node нужно оставить для определённых нагрузок.

**Правила размещения отвечают на два вопроса: на какие Node Pod можно поставить и какие из допустимых Node предпочтительнее.** Scheduler учитывает ресурсы, свойства узлов, расположение других Pod и ограничения самих Node.

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

Допустим, первый узел не вмещает CPU request, второй не имеет обязательной метки, третий подходит. Scheduler выберет третий: **предпочтения работают только среди узлов, прошедших обязательные проверки**. Если подходящих узлов нет, Pod остаётся без назначения, обычно в `Pending`, пока ситуация не изменится.

## Метки Node и nodeSelector

Node имеют собственные метки, например `disk=ssd`, `environment=production` или `gpu=true`. Их задают на узле. Метки Pod описывают отдельный объект.

`nodeSelector` — простое обязательное условие по меткам Node. В примере все три реплики `web` должны размещаться на узлах с `disk=ssd`:

```yaml
# web.yaml
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
      nodeSelector:  # Обязательные метки Node
        disk: ssd
      containers:
        - name: web
          image: nginx:1.27
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
```

`spec.selector` выбирает управляемые Pod по `app=web`, а `spec.template.spec.nodeSelector` ограничивает выбор **Node** по `disk=ssd`. Если подходящих SSD-узлов несколько, Scheduler выбирает среди них с учётом остальных условий. Если их нет, Pod будет ждать подходящего узла.

Все пары в `nodeSelector` должны совпасть одновременно. Например, `disk=ssd` вместе с `environment=production` требует обеих меток на одном узле. При этом selector не разносит реплики: несколько Pod могут оказаться на одном подходящем Node, если остальные условия допускают такое размещение.

## Node affinity: обязательное и предпочтительное

`nodeAffinity` тоже проверяет метки Node, но позволяет выразить набор допустимых значений и предпочтения. Следующий фрагмент **заменяет `nodeSelector`** в `spec.template.spec` предыдущего Deployment. Он требует `environment=production` или `environment=staging`, а SSD делает предпочтением:

```yaml
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution: # Обязательное условие
      nodeSelectorTerms:
        - matchExpressions:
            - key: environment
              operator: In
              values: [production, staging]
    preferredDuringSchedulingIgnoredDuringExecution: # Предпочтение
      - weight: 100 # Вес совпавшего предпочтения, от 1 до 100
        preference:
          matchExpressions:
            - key: disk
              operator: In
              values: [ssd]
```

Узел с production или staging проходит обязательное условие. Наличие SSD добавляет ему оценку с весом 100. В итоговой оценке участвуют и другие правила Scheduler, поэтому выбор SSD остаётся предпочтением.

Если SSD обязателен, выражение `disk In [ssd]` нужно поместить в required-часть. Для простого равенства это решает ту же задачу, что `nodeSelector`, но синтаксис affinity гибче. Если оставить и `nodeSelector`, и required node affinity, **обязательные условия обоих механизмов должны выполняться**.

В `matchExpressions` оператор `In` разрешает перечисленные значения, `NotIn` исключает их, `Exists` проверяет наличие ключа, а `DoesNotExist` — отсутствие. Несколько выражений внутри одного элемента списка `nodeSelectorTerms` соединяются через «И», а отдельные элементы этого списка — через «ИЛИ». В примере достаточно одного из значений `environment`: `production` или `staging`.

## Pod affinity и anti-affinity: учитывать соседние Pod

Node affinity спрашивает о свойствах узла, а `podAffinity` — о расположении других Pod. Например, frontend можно направлять туда, где находится backend. `podAntiAffinity` задаёт обратное отношение: избегать размещения рядом с выбранными Pod. Это помогает разносить реплики приложения, чтобы отказ одного Node не затронул их все.

### Что такое zone

**Зона (`zone`) — отдельная область размещения инфраструктуры, обычно облачная зона доступности.** В одной зоне может быть несколько Node. Общий сбой зоны может затронуть их все, поэтому реплики разносят по разным зонам.

Метка `topology.kubernetes.io/zone` на Node указывает его зону. Например, два узла с одинаковым значением `zone-a` принадлежат одной зоне.

### Граница соседства: topologyKey

**`topologyKey` определяет, что считать соседством:** `kubernetes.io/hostname` — один узел, `topology.kubernetes.io/zone` — одна зона. Нужные метки должны присутствовать на Node.

В следующем фрагменте желательно разместить реплики `web` на разных узлах:

```yaml
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector: # Метки соседних Pod
            matchLabels:
              app: web
          topologyKey: kubernetes.io/hostname # Сравнивать на уровне узла
```

По умолчанию соседние Pod ищутся в namespace самого Pod — здесь `study`. Другую область поиска задают через `namespaces` или `namespaceSelector`.

Если вместо `podAntiAffinity` использовать `podAffinity` и выбрать `app=backend`, аналогичное preferred-правило будет поощрять размещение рядом с backend. У обоих механизмов есть required и preferred: первый делает соседство или его отсутствие обязательным, второй лишь влияет на оценку.

**Preferred anti-affinity делает разнесение предпочтительным, а соседство оставляет допустимым.** Например, три реплики из нашего Deployment могут разместиться на двух подходящих узлах. Если сделать отсутствие соседних реплик обязательным через required anti-affinity по hostname, для трёх одновременно размещённых реплик потребуются как минимум три подходящих узла. Иначе часть Pod останется без назначения.

## Taints и tolerations: ограничение со стороны Node

Selector и affinity описывают требования Pod к месту запуска. **Taint задаётся на Node и ограничивает размещение Pod без подходящих tolerations.** Например, `dedicated=gpu:NoSchedule` содержит ключ `dedicated`, значение `gpu` и эффект `NoSchedule`: новые Pod без подходящей toleration Scheduler сюда не поставит.

**Toleration** задаётся у Pod и снимает для него ограничение соответствующего taint. Фрагмент для `spec.template.spec`:

```yaml
tolerations:
  - key: dedicated
    operator: Equal
    value: gpu
    effect: NoSchedule
```

Здесь `Equal` требует совпадения ключа, значения и эффекта. При `operator: Exists` значение не указывают: toleration подходит для любого значения данного ключа с указанным эффектом. Если на Node несколько taints, нужно учесть каждый: одна подходящая toleration не снимает остальные ограничения.

**Toleration допускает размещение на Node с соответствующим taint.** Scheduler по-прежнему учитывает ресурсы, обязательные условия и предпочтения. Обычные узлы без этого taint тоже остаются допустимыми.

### Три эффекта taint

| Эффект | Новое размещение без toleration | Уже размещённые Pod без toleration |
|---|---|---|
| `NoSchedule` | Не допускается | Продолжают работать |
| `PreferNoSchedule` | Нежелательно, но допустимо | Продолжают работать |
| `NoExecute` | Не допускается | Подлежат удалению с узла |

Для `NoExecute` поле `tolerationSeconds` задаёт, сколько секунд Pod может оставаться после появления taint. Без этого поля подходящая toleration разрешает оставаться без ограничения времени. Если taint снят раньше, Pod продолжит работать.

## Как сочетать выбор узла и разрешение

Допустим, специальные узлы имеют **два независимых свойства**: метку `gpu=true` и taint `dedicated=gpu:NoSchedule`. Метка позволяет найти нужную группу, а taint ограничивает допуск. Метку и taint задают отдельно. Для Pod, который должен идти именно в эту группу, нужны обе части в `spec.template.spec`:

```yaml
nodeSelector:
  gpu: "true"
tolerations:
  - key: dedicated
    operator: Equal
    value: gpu
    effect: NoSchedule
```

Selector выбирает узлы с `gpu=true`, toleration снимает их taint-ограничение. Выдачу самого GPU контейнеру настраивают отдельно через ресурсы и устройства.

```text
проверка requests + nodeSelector / required affinity + taints/tolerations
                                   ↓
                          допустимые Node
                                   ↓
        preferred affinity + PreferNoSchedule + остальные оценки
                                   ↓
                            выбранный Node
```

## Что происходит после назначения

`DuringScheduling` означает проверку при планировании. `IgnoredDuringExecution` означает, что уже запущенный Pod продолжит работать, даже если условие позже перестанет выполняться. Если после запуска Node потерял нужную метку, обычный Pod не переносится автоматически. Это верно и для `nodeSelector`. Появление более предпочтительного узла тоже не вызывает переезд уже размещённого Pod.

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

`NODE` и `POD` — имена существующих объектов, `web.yaml` — файл с полным манифестом из примера. `-L` выводит значения выбранных меток Node, `-o wide` показывает узел каждого Pod. Завершающий `-` в команде taint удаляет указанное ограничение. Метки и taints Node меняются на уровне кластера, поэтому у этих команд нет `-n study`.

## Если Pod остаётся Pending

Сначала посмотри, назначен ли ему Node: `Pending` возможен и при ожидании запуска контейнеров после назначения. Если узла ещё нет, начни с Events в `describe pod` и сопоставь сообщение с конфигурацией самого Pod и выбранных Node. Файл на диске может отличаться от принятого объекта в API, поэтому полезен `get pod -o yaml`.

| Причина в Events | Что проверить |
|---|---|
| `Insufficient cpu` или `Insufficient memory` | Requests нового Pod, allocatable Node и requests уже размещённых Pod |
| Несовпадение node selector/affinity | Метки Node и все обязательные условия Pod |
| `untolerated taint` | Ключ, значение и эффект taint, наличие подходящей toleration |
| Несовпадение pod affinity/anti-affinity | Метки и namespace соседних Pod, topologyKey и число допустимых областей |

У Node может быть несколько причин отказа сразу. Исправить нужно каждую. После назначения узла переходи к диагностике контейнеров из [записи 3](03_debug.md).

## Проверь себя

**1. Pod требует disk=ssd, но у всех Node стоит disk=hdd. Можно ли поставить его на HDD, если CPU и память свободны?**

<details>
<summary>Показать ответ</summary>

Нет. nodeSelector — обязательное условие, которое ресурсы не отменяют. Pod останется без назначения, пока не появится подходящий узел или не изменится требование.

</details>

**2. SSD указан только в preferred node affinity с weight=100. Гарантирует ли это SSD и что будет, если остались только HDD-узлы?**

<details>
<summary>Показать ответ</summary>

Гарантии нет: вес участвует в итоговой оценке вместе с другими правилами. HDD допустим, если прошёл все обязательные проверки.

</details>

**3. В одном nodeSelectorTerms-элементе стоят disk In [ssd] и environment In [production, staging]. Какое условие должен выполнить Node?**

<details>
<summary>Показать ответ</summary>

Нужны одновременно SSD и одно из двух значений метки окружения: production или staging. Выражения внутри элемента соединяются через «И», значения In — альтернативы.

</details>

**4. Чем отличаются nodeAffinity и podAffinity? Что меняет topologyKey hostname по сравнению с zone?**

<details>
<summary>Показать ответ</summary>

Node affinity проверяет метки узла, pod affinity — расположение Pod с выбранными метками. Hostname задаёт соседство на уровне отдельного Node, zone — всей зоны. В одной зоне могут находиться разные узлы.

</details>

**5. Есть три реплики и два подходящих Node. Чем отличаются preferred и required anti-affinity по app=web и hostname?**

<details>
<summary>Показать ответ</summary>

Preferred допускает соседство, поэтому третья реплика может запуститься на занятом узле. Required запрещает такое соседство: для трёх одновременно размещённых реплик нужны как минимум три подходящих Node, иначе часть останется без назначения.

</details>

**6. На GPU-узлах есть метка gpu=true и taint dedicated=gpu:NoSchedule. Что задать в Pod, чтобы выбрать именно эти узлы?**

<details>
<summary>Показать ответ</summary>

`nodeSelector` или required node affinity по метке Node `gpu=true` и toleration для `dedicated=gpu:NoSchedule`. Первое выбирает группу, второе разрешает размещение на ней.

</details>

**7. На Node с работающими Pod добавили NoSchedule, а позже NoExecute. Что происходит без подходящих tolerations?**

<details>
<summary>Показать ответ</summary>

NoSchedule запрещает новое размещение, но не удаляет уже размещённые Pod. NoExecute также приводит к их удалению с узла.

</details>

**8. Уже размещённый Pod имел required node affinity по disk=ssd, затем Node получил disk=hdd. Перенесёт ли его Scheduler?**

<details>
<summary>Показать ответ</summary>

Нет. IgnoredDuringExecution не требует выселения при таком изменении меток. Новые Pod будут проверяться по текущим меткам, а существующий Pod не переезжает автоматически на другой узел.

</details>
