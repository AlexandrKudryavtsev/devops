# 13. StatefulSet и headless Service: идентичность реплики и её данные

## Какую проблему решаем

В [записи 12](12_pv_pvc.md) мы отделили хранилище от Pod: замена может подключить прежний PVC и получить сохранённые файлы. Но для распределённого приложения важны ещё и различия между экземплярами. Реплике может понадобиться постоянное имя, по которому её находят соседи, и собственное хранилище, которое при восстановлении достанется именно ей.

**StatefulSet поддерживает Pod с устойчивой логической идентичностью.** Например, при трёх репликах он создаёт `database-0`, `database-1` и `database-2`. Если `database-1` удалён, контроллер создаёт замену с тем же именем и связью с её хранилищем; это новый объект Pod, а не продолжение жизни старого.

| Deployment | StatefulSet |
|---|---|
| Реплики обычно взаимозаменяемы | Каждая реплика имеет постоянный ordinal — порядковый номер |
| Имена Pod содержат меняющийся суффикс | Имена имеют вид `name-0`, `name-1`, … |
| Поддерживает Pod через ReplicaSet | Управляет Pod напрямую |
| Может использовать PVC | Может создавать отдельные PVC для каждой реплики через `volumeClaimTemplates` |

StatefulSet часто используют для баз данных и распределённых систем, но название приложения само по себе не определяет выбор контроллера. Главный вопрос — нужна ли экземплярам индивидуальность: постоянное сетевое имя, собственные данные или определённый порядок запуска. Если достаточно взаимозаменяемых Pod, один факт использования PVC не требует StatefulSet.

## Механизм

StatefulSet связывает заменяемый Pod с его ordinal, сетевым именем и, если задан шаблон claims, личным хранилищем. При стандартной нумерации `replicas: 3` означает номера `0..2`; имя Pod получается из имени StatefulSet и номера реплики.

```text
StatefulSet database → Pod database-1 → PVC data-database-1 → PV B → данные B
                           ↑
          DNS: database-1.database.study.svc.cluster.local
```

Здесь есть две независимые связи: headless Service участвует в DNS-идентичности Pod, а PVC связывает реплику с предоставленным хранилищем. StatefulSet управляет экземплярами, но не хранит данные и не настраивает протокол репликации приложения. Три Pod базы сами по себе ещё не становятся согласованным кластером базы.

## StatefulSet и headless Service в одном примере

Сохрани два объекта ниже как `database.yaml`. Имя `database` продолжает пример из драфта, но контейнер — учебный NGINX: он отвечает по HTTP, а в `/data` мы можем вручную записать файл и проверить постоянство хранилища.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: database
  namespace: study
spec:
  clusterIP: None
  selector:
    app: database
  ports:
    - name: http
      port: 80
      targetPort: http
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: database
  namespace: study
spec:
  serviceName: database
  replicas: 3
  selector:
    matchLabels:
      app: database
  template:
    metadata:
      labels:
        app: database
    spec:
      containers:
        - name: nginx
          image: nginx:1.27-alpine
          ports:
            - name: http
              containerPort: 80
          readinessProbe:
            httpGet:
              path: /
              port: http
          volumeMounts:
            - name: data
              mountPath: /data
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 2Gi
```

Образ сохранён из предыдущих учебных примеров. NGINX здесь не является базой данных и не использует `/data` для своего сайта: HTTP-проверка показывает готовность процесса, а постоянные файлы проверяем отдельно. Для автоматического создания PV нужен StorageClass по умолчанию с работающим provisioner; другой существующий класс можно указать в `volumeClaimTemplates[].spec.storageClassName`.

| Поле | Что задаёт |
|---|---|
| Service `clusterIP: None` | Headless Service без виртуального IP |
| Service `selector` | Какие Pod входят в набор endpoints |
| StatefulSet `serviceName: database` | Имя Service, задающего сетевой домен реплик |
| `replicas: 3` | Требуемое количество экземпляров |
| `selector` и `template.metadata.labels` | Метки управляемых Pod; шаблон должен соответствовать selector |
| `volumeClaimTemplates` | Шаблоны отдельных PVC для каждой реплики |
| `volumeMounts[].name: data` | Подключение volume, созданного из claim template с именем `data` |

**`serviceName` не создаёт Service и не заменяет его selector.** Service нужно создать отдельно в том же namespace, а его selector должен выбирать нужные Pod. Имена StatefulSet и Service могут различаться: имя Pod строится от StatefulSet, а домен — от Service, указанного в `serviceName`.

## Headless Service: найти отдельные Pod через DNS

В записи 4 обычный Service давал один виртуальный IP перед меняющимися backend. Headless Service создаётся через явное `clusterIP: None`: виртуальный IP ему не выделяется, а DNS сообщает адреса endpoints. Если поле `clusterIP` просто не указать, получится обычный Service с выделенным IP.

```text
обычный Service: DNS-имя → ClusterIP → сетевой механизм выбирает backend
headless Service: DNS-имя → IP подходящих Pod → клиент подключается напрямую
```

Headless Service с selector тоже использует EndpointSlice, но Kubernetes не выполняет для него обычное проксирование и балансировку через виртуальный IP. Клиент получает адреса и выбирает, к какому подключиться; получение нескольких IP само по себе не гарантирует равномерного распределения запросов. См. [механизм headless Service](https://kubernetes.io/docs/concepts/services-networking/service/#headless-services).

При кластерном домене `cluster.local` наш пример даёт следующие имена. Этот домен распространён, но настраивается; в другом кластере последняя часть имени может отличаться.

| DNS-имя | Что позволяет найти |
|---|---|
| `database.study.svc.cluster.local` | Адреса опубликованных endpoints Service — в обычной конфигурации готовых Pod |
| `database-0.database.study.svc.cluster.local` | Конкретную реплику `database-0` |
| `database-1.database.study.svc.cluster.local` | Конкретную реплику `database-1` |
| `database-2.database.study.svc.cluster.local` | Конкретную реплику `database-2` |

**Стабильно имя, а не IP Pod.** После замены `database-1` её DNS-имя остаётся прежним, но адрес может измениться. Клиенту нужно учитывать обновление DNS и переподключение; DNS-кеш также может ненадолго сохранять старый адрес или прежний отрицательный ответ.

По умолчанию для публикации адреса Pod требуется готовность. Если приложение должно находить соседей ещё до Ready, например для первоначального формирования кластера, у Service можно задать `publishNotReadyAddresses: true`. Тогда наличие адреса в DNS не означает готовность обслуживать обычные запросы; эту настройку выбирают по механизму приложения. Подробнее — [DNS для Service и Pod](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/).

Headless Service можно использовать и без StatefulSet, а StatefulSet при необходимости может иметь дополнительный обычный Service для клиентского трафика. Такой Service выбирает подходящие реплики по своим меткам; Kubernetes сам не определяет, какая из них лидер базы и куда допустимо отправлять запись.

## `volumeClaimTemplates`: хранилище каждой реплики

В обычном Pod из записи 12 мы указали имя уже существующего PVC в `claimName`. Здесь StatefulSet получает шаблон и создаёт по нему отдельный PVC для каждого ordinal; объявлять тот же volume ещё раз в `template.spec.volumes` не нужно. Имя claim получается по правилу `<имя шаблона>-<имя StatefulSet>-<ordinal>`.

```text
database-0 → PVC data-database-0 → PV A → данные A
database-1 → PVC data-database-1 → PV B → данные B
database-2 → PVC data-database-2 → PV C → данные C
```

При одном шаблоне и трёх репликах получится три PVC; при двух шаблонах — шесть. StatefulSet создаёт claims, а подбор или динамическое предоставление PV происходит по механизму из записи 12. У каждой реплики свой том, поэтому её `ReadWriteOnce` не мешает другим репликам работать на других узлах со своими томами.

Если удалить `database-1`, замена снова использует `data-database-1`, связанный с PV B. Она не получает PVC от `database-2` и не должна автоматически начинать с пустого тома. Но если соответствующий PVC и хранилище удалены отдельно, прежнее имя Pod не восстанавливает потерянные данные.

У нового `database-1` будут новый UID и, возможно, другие IP и Node. **Постоянна логическая реплика и её связь с сохраняемым PVC, а не конкретный объект Pod или место запуска.** Возможность подключить существующий том на выбранном узле по-прежнему зависит от системы хранения.

## Порядок запуска и масштабирование

По умолчанию используется `podManagementPolicy: OrderedReady`: при создании StatefulSet сначала запускается `database-0`, затем после её Running и Ready — `database-1`, потом `database-2`. Если предыдущая реплика не готова, создание следующих может ждать; поэтому readiness из записи 9 влияет здесь не только на трафик, но и на продвижение запуска.

| Изменение | Реакция при стандартной нумерации и настройках |
|---|---|
| `replicas: 3 → 4` | Добавляется `database-3` со своим `data-database-3`, если claim ещё не существует |
| `replicas: 3 → 2` | Удаляется `database-2`; её PVC по умолчанию сохраняется |
| `replicas: 2 → 3` после такого уменьшения | Восстанавливается `database-2` с ранее сохранённым PVC |

При уменьшении количества Pod завершаются от большего ordinal к меньшему, с ожиданием завершения предыдущего шага. Политика `Parallel` позволяет выполнять операции масштабирования без такого последовательного ожидания, сохраняя идентичность реплик; стратегия обновления задаётся отдельно. Порядок действий описан в [учебном разборе StatefulSet](https://kubernetes.io/docs/tutorials/stateful-application/basic-stateful-set/).

Масштабирование меняет требуемое количество экземпляров, но не настраивает участие новой реплики в базе, перенос данных или безопасный вывод участника из приложения. Как в записи 5, `kubectl scale` меняет объект в API, а сохранённый YAML нужно привести к тому же желаемому состоянию.

## Когда сохраняются PVC

**PVC из `volumeClaimTemplates` по умолчанию остаются и при уменьшении replicas, и при удалении StatefulSet.** Это отделяет удаление вычислительных экземпляров от удаления их данных. Поведение можно явно описать в `spec`:

```yaml
persistentVolumeClaimRetentionPolicy:
  whenDeleted: Retain
  whenScaled: Retain
```

| Настройка | Какое событие регулирует |
|---|---|
| `whenDeleted` | Удаление StatefulSet |
| `whenScaled` | Уменьшение требуемого числа реплик |

Для каждого поля доступны `Retain` и `Delete`. Например, `whenScaled: Delete` удаляет claims убираемых реплик после завершения их Pod; обычная замена потерянного Pod не является уменьшением replicas. Эти поля регулируют PVC из шаблонов StatefulSet, а не произвольные claims приложения. См. [политику удержания PVC](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#persistentvolumeclaim-retention).

Не смешивай две политики: `persistentVolumeClaimRetentionPolicy` StatefulSet определяет, будет ли удалён **PVC**, а `persistentVolumeReclaimPolicy` PV — что произойдёт с **томом после удаления PVC**. Если первая приводит к удалению claim, а вторая равна `Delete`, могут быть удалены и PV, и данные в системе хранения.

## Обновление шаблона

Изменение `spec.template`, например image или readiness probe, меняет конфигурацию экземпляров. При `RollingUpdate`, стратегии по умолчанию, без дополнительных настроек контроллер заменяет Pod по одному, начиная с наибольшего ordinal, и ждёт готовности замены перед следующим шагом. Имена реплик и связь с их PVC сохраняются.

При `updateStrategy.type: OnDelete` контроллер не заменяет существующие Pod автоматически из-за изменения шаблона; новую конфигурацию получает замена после удаления Pod. Поэтому изменение желаемого шаблона и его применение ко всем работающим экземплярам — разные состояния.

История и откат шаблона доступны через `kubectl rollout`, как в записи 6, но **откат шаблона не откатывает данные на диске**. Если при последовательном обновлении неисправный Pod не становится Ready, одного возврата рабочего шаблона может быть недостаточно: после возврата может потребоваться удалить Pod с неисправной конфигурацией, чтобы контроллер создал замену. Этот случай разобран в [документации об откате StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#forced-rollback).

## Основные команды

```bash
# Применить оба объекта и наблюдать достижение готовности
kubectl apply -f database.yaml
kubectl rollout status statefulset/database -n study --timeout=2m
kubectl get statefulset database -n study
kubectl get pods -n study -l app=database -o wide

# Проверить claims реплик и конкретный экземпляр
kubectl get pvc -n study -o wide
kubectl describe statefulset database -n study
kubectl describe pod database-1 -n study
kubectl describe pvc data-database-1 -n study
kubectl exec database-1 -n study -- ls -la /data

# Проверить Service и опубликованные endpoints
kubectl get service database -n study
kubectl get endpointslices -n study -l kubernetes.io/service-name=database -o yaml

# Найти конкретную реплику через DNS из временного клиентского Pod
kubectl run dns-client -n study --image=busybox:1.36 \
  --restart=Never --rm -i -- nslookup database-1.database.study.svc.cluster.local

# Изменить требуемое количество экземпляров
kubectl scale statefulset/database -n study --replicas=4

# Посмотреть сохранённые ревизии шаблона
kubectl rollout history statefulset/database -n study
```

В `get service` значение `CLUSTER-IP` должно быть `None`: отсутствие виртуального IP здесь ожидаемо. `nslookup` выполняется внутри кластера, потому что кластерный DNS обычно недоступен с ноутбука. Тайм-аут `rollout status` завершает ожидание kubectl, а не работу контроллера; после `scale` файл `database.yaml` сам не изменится.

## Если реплика не работает или не находится по имени

При диагностике разделяй управление экземпляром, хранилище и обнаружение по сети. StatefulSet → нужный Pod → его PVC и PV → подключённый volume объясняют запуск и данные; Service → selector → EndpointSlice → readiness → DNS объясняют сетевую доступность.

| Наблюдение | С чего начать |
|---|---|
| Создан только `database-0`, остальные отсутствуют | Готовность первого Pod и Events: OrderedReady может ждать |
| Pod есть, но `Pending` или `ContainerCreating` | Его Events и PVC; диагностика storage из записи 12 |
| Pod Ready, но DNS-имя реплики не находится | Существование headless Service, `serviceName`, namespace, selector и EndpointSlice |
| Неготовый Pod не находится через DNS | Условия публикации адресов и необходимость `publishNotReadyAddresses` для этого приложения |
| Имя находится, но соединение не проходит | Актуальный IP, порт процесса, готовность приложения и сетевой путь |
| После замены Pod файлы отсутствуют | Тот ли PVC подключён, сохранился ли он, куда приложение записывало данные |

После появления готовой реплики DNS-ответ может обновиться с задержкой из-за кеширования. Само разрешение имени также не доказывает работоспособность базы или HTTP-приложения: проверь реальное соединение, как в записи 4, и логи нужного Pod. Начинай с того слоя, на котором нарушено ожидаемое состояние.

## Проверь себя

**1. Нужно ли выбирать StatefulSet для любого приложения с PVC?**

<details>
<summary>Показать ответ</summary>

Нет. PVC доступен и обычному Pod, и Deployment. StatefulSet нужен, когда важны индивидуальные реплики: их постоянные имена, связь со своими данными или порядок операций.

</details>

**2. Удалили `database-1`, сохранив её PVC. Что сохранится, а что может измениться у замены?**

<details>
<summary>Показать ответ</summary>

Сохраняются ordinal, имя database-1 и связь с data-database-1; с работающим Service сохраняется DNS-имя. Сам Pod новый: UID изменится, IP и Node тоже могут измениться. Замена получает прежние данные, если хранилище сохранилось и доступно для подключения.

</details>

**3. Есть три реплики и один `volumeClaimTemplates` с именем `data`. Сколько PVC будет создано и получит ли `database-1` claim от `database-2`?**

<details>
<summary>Показать ответ</summary>

Три: data-database-0, data-database-1 и data-database-2. Реплика database-1 использует свой claim, а не claim соседа. Два шаблона дали бы по два PVC на реплику, всего шесть.

</details>

**4. В StatefulSet задано `serviceName: database`, но Service не создан. Достаточно ли этого для DNS-идентичности из примера?**

<details>
<summary>Показать ответ</summary>

Нет. Поле ссылается на отдельный Service и не создаёт его. Нужен headless Service в том же namespace с selector, выбирающим Pod StatefulSet.

</details>

**5. Чем различаются DNS-ответы для обычного и headless Service? Сохраняется ли IP реплики после её замены?**

<details>
<summary>Показать ответ</summary>

Обычный Service разрешается в свой ClusterIP, headless — в адреса опубликованных endpoints. Имя отдельной реплики устойчиво, её IP может измениться. Клиент должен учитывать DNS и переподключение.

</details>

**6. У StatefulSet три реплики, но первый Pod Running и не Ready, а остальных нет. Противоречит ли это `replicas: 3`?**

<details>
<summary>Показать ответ</summary>

Нет. Replicas описывает желаемое количество, а OrderedReady ждёт готовности предшествующих реплик. Нужно исследовать причину неготовности первого Pod, а не считать, что остальные обязаны уже существовать.

</details>

**7. StatefulSet уменьшили с трёх реплик до двух, затем снова увеличили до трёх. Что по умолчанию произойдёт с данными `database-2`?**

<details>
<summary>Показать ответ</summary>

Её Pod удалится, но PVC data-database-2 останется. При увеличении новый database-2 подключит сохранённый claim. Если настроен whenScaled: Delete, последствия зависят ещё и от политики освобождения PV.

</details>

**8. Откатили шаблон StatefulSet после ошибочного обновления приложения. Откатились ли изменения данных и обязано ли восстановление сразу продолжиться?**

<details>
<summary>Показать ответ</summary>

Нет. Откат возвращает конфигурацию Pod, сохраняя содержимое томов. Если обновление остановилось на неготовом Pod, после возврата рабочего шаблона может потребоваться удалить этот Pod для создания исправной замены.

</details>
