# 14. StatefulSet и headless Service: идентичность реплики и её данные

## Какую проблему решаем

Репликам распределённого приложения могут понадобиться постоянные имена и собственные тома. После замены Pod каждой реплике должны достаться именно её имя и данные.

**StatefulSet поддерживает Pod с устойчивой логической идентичностью.** Допустим, задано три реплики. StatefulSet создаст `database-0`, `database-1` и `database-2`. Если `database-1` удалён, контроллер создаёт замену с тем же именем и связью с её хранилищем. У нового Pod будет другой UID, а IP и Node могут измениться.

| Deployment | StatefulSet |
|---|---|
| Реплики обычно взаимозаменяемы | Каждая реплика имеет постоянный ordinal — порядковый номер |
| Имена Pod содержат меняющийся суффикс | Имена имеют вид `name-0`, `name-1`, … |
| Поддерживает Pod через ReplicaSet | Управляет Pod напрямую |
| Может использовать PVC | Может создавать отдельные PVC для каждой реплики через `volumeClaimTemplates` |

Выбирай StatefulSet, если важны постоянные имена реплик, отдельные данные или порядок запуска. Для взаимозаменяемых Pod подходит Deployment, в том числе с PVC.

## Механизм

StatefulSet связывает заменяемый Pod с его ordinal, сетевым именем и, если задан шаблон claims, личным хранилищем. При стандартной нумерации `replicas: 3` означает номера `0..2`. Имя Pod получается из имени StatefulSet и номера реплики.

```text
StatefulSet database → Pod database-1 → PVC data-database-1 → PV B → данные B
                           ↑
          DNS: database-1.database.study.svc.cluster.local
```

**Headless Service** публикует через DNS адреса отдельных Pod. PVC связывает реплику с её хранилищем. За репликацию данных между экземплярами отвечает само приложение.

## StatefulSet и headless Service в одном примере

Для проверки используем NGINX: HTTP-запрос покажет готовность, а файл в `/data` — сохранение данных после замены Pod.

```yaml
# database.yaml
apiVersion: v1
kind: Service
metadata:
  name: database
  namespace: study
spec:
  clusterIP: None       # Headless Service: без виртуального IP
  selector:             # Метки Pod, чьи адреса публикуем
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
  serviceName: database # Service для сетевых имён реплик
  replicas: 3           # Создать database-0, database-1, database-2
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
            - name: data      # Том из volumeClaimTemplates
              mountPath: /data # Папка с данными этой реплики
  volumeClaimTemplates: # Отдельный PVC для каждой реплики
    - metadata:
        name: data      # Имя шаблона и соответствующего volumeMount
      spec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 2Gi
```

В `/data` будем записывать файлы вручную. Для автоматического создания PV нужен StorageClass по умолчанию с работающим provisioner. Другой существующий класс можно указать в `volumeClaimTemplates[].spec.storageClassName`.

**`serviceName` ссылается на отдельно созданный Service в том же namespace.** Его selector должен выбирать Pod StatefulSet. Имена StatefulSet и Service могут различаться: имя Pod строится от StatefulSet, а домен — от Service, указанного в `serviceName`.

## Headless Service: найти отдельные Pod через DNS

В записи 4 обычный Service давал один виртуальный IP перед меняющимися backend. Headless Service создаётся через явное `clusterIP: None`: виртуальный IP ему не выделяется, а DNS сообщает адреса endpoints. Если поле `clusterIP` просто не указать, получится обычный Service с выделенным IP.

```text
обычный Service: DNS-имя → ClusterIP → сетевой механизм выбирает backend
headless Service: DNS-имя → IP подходящих Pod → клиент подключается напрямую
```

Headless Service с selector тоже использует EndpointSlice. Клиент получает адреса и сам выбирает Pod для подключения. Балансировка через виртуальный IP здесь отсутствует.

Допустим, домен кластера — `cluster.local`. Тогда доступны такие имена:

| DNS-имя | Что позволяет найти |
|---|---|
| `database.study.svc.cluster.local` | Адреса опубликованных endpoints Service — в обычной конфигурации готовых Pod |
| `database-0.database.study.svc.cluster.local` | Конкретную реплику `database-0` |
| `database-1.database.study.svc.cluster.local` | Конкретную реплику `database-1` |
| `database-2.database.study.svc.cluster.local` | Конкретную реплику `database-2` |

**DNS-имя реплики сохраняется после замены Pod.** IP может измениться. Тогда клиенту нужно заново разрешить имя и переподключиться. Обновление DNS может задержаться из-за кеширования.

### Особые случаи DNS

По умолчанию Service публикует готовые адреса. Если приложению нужно найти соседей ещё до Ready, например при формировании кластера, задают `publishNotReadyAddresses: true`. В этом случае DNS может возвращать адрес ещё неготового Pod.

StatefulSet может иметь дополнительный обычный Service для клиентского трафика. Его selector должен выбирать реплики, способные обработать такой запрос. Роль лидера базы определяет приложение.

## `volumeClaimTemplates`: хранилище каждой реплики

В обычном Pod из записи 13 мы указали имя уже существующего PVC в `claimName`. Здесь StatefulSet получает шаблон и создаёт по нему отдельный PVC для каждого ordinal. Объявлять тот же volume ещё раз в `template.spec.volumes` не нужно. Имя claim получается по правилу `<имя шаблона>-<имя StatefulSet>-<ordinal>`.

```text
database-0 → PVC data-database-0 → PV A → данные A
database-1 → PVC data-database-1 → PV B → данные B
database-2 → PVC data-database-2 → PV C → данные C
```

При одном шаблоне и трёх репликах получится три PVC. При двух шаблонах — шесть. StatefulSet создаёт claims, а подбор или динамическое предоставление PV происходит по механизму из записи 13. У каждой реплики свой том, поэтому её `ReadWriteOnce` не мешает другим репликам работать на других узлах со своими томами.

Если удалить `database-1`, замена снова использует `data-database-1`, связанный с PV B. Если PVC и хранилище удалены отдельно, для восстановления данных потребуется резервная копия.

Возможность подключить существующий том на выбранном узле зависит от системы хранения.

## Порядок запуска и масштабирование

По умолчанию используется `podManagementPolicy: OrderedReady`: при создании StatefulSet сначала запускается `database-0`, затем после её Running и Ready — `database-1`, потом `database-2`. Пока предыдущая реплика не готова, создание следующих может ждать.

| Изменение | Реакция при стандартной нумерации и настройках |
|---|---|
| `replicas: 3 → 4` | Добавляется `database-3` со своим `data-database-3`, если claim ещё не существует |
| `replicas: 3 → 2` | Удаляется `database-2`. Её PVC по умолчанию сохраняется |
| `replicas: 2 → 3` после такого уменьшения | Восстанавливается `database-2` с ранее сохранённым PVC |

При уменьшении числа реплик Pod завершаются от большего ordinal к меньшему, с ожиданием завершения предыдущего шага. Политика `Parallel` позволяет выполнять операции масштабирования без такого последовательного ожидания, сохраняя идентичность реплик. Стратегия обновления задаётся отдельно.

После масштабирования приложение должно само включить новую реплику в работу или безопасно вывести удаляемую. `kubectl scale` меняет объект в API. Сохранённый YAML исправляют отдельно.

## Когда сохраняются PVC

**PVC из `volumeClaimTemplates` по умолчанию остаются и при уменьшении replicas, и при удалении StatefulSet.** Это отделяет удаление вычислительных экземпляров от удаления их данных. Поведение можно явно описать в `spec`:

```yaml
persistentVolumeClaimRetentionPolicy:
  whenDeleted: Retain # Оставить PVC при удалении StatefulSet
  whenScaled: Retain  # Оставить PVC убираемой реплики при уменьшении replicas
```

Для каждого поля доступны `Retain` и `Delete`. Например, `whenScaled: Delete` удаляет claims убираемых реплик после завершения их Pod. Обычная замена потерянного Pod не является уменьшением replicas. Эти поля регулируют PVC из шаблонов StatefulSet, а не произвольные claims приложения.

**`persistentVolumeClaimRetentionPolicy` StatefulSet управляет удалением PVC. `persistentVolumeReclaimPolicy` PV управляет судьбой тома после удаления PVC.** Если первая приводит к удалению claim, а вторая равна `Delete`, могут быть удалены и PV, и данные в системе хранения.

## Обновление шаблона

Изменение `spec.template`, например image или readiness probe, меняет конфигурацию экземпляров. При `RollingUpdate`, стратегии по умолчанию, без дополнительных настроек контроллер заменяет Pod по одному, начиная с наибольшего ordinal, и ждёт готовности замены перед следующим шагом. Имена реплик и связь с их PVC сохраняются.

При `updateStrategy.type: OnDelete` новая конфигурация применяется к замене после удаления Pod. Уже работающие Pod сохраняют прежний шаблон.

**Откат через `kubectl rollout` возвращает шаблон Pod. Данные на диске остаются в текущем состоянии.**

Особый случай: обновление остановилось на Pod, который не становится Ready. После возврата рабочего шаблона может потребоваться удалить этот Pod, чтобы контроллер создал исправную замену.

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

В `get service` значение `CLUSTER-IP` должно быть `None`: отсутствие виртуального IP здесь ожидаемо. `nslookup` выполняется внутри кластера, потому что кластерный DNS обычно недоступен с ноутбука.

## Если реплика не работает или не находится по имени

При диагностике разделяй управление экземпляром, хранилище и обнаружение по сети. StatefulSet → нужный Pod → его PVC и PV → подключённый volume объясняют запуск и данные. Service → selector → EndpointSlice → readiness → DNS объясняют сетевую доступность.

| Наблюдение | С чего начать |
|---|---|
| Создан только `database-0`, остальные отсутствуют | Готовность первого Pod и Events: OrderedReady может ждать |
| Pod есть, но `Pending` или `ContainerCreating` | Его Events и PVC. Диагностика storage из записи 13 |
| Pod Ready, но DNS-имя реплики не находится | Существование headless Service, `serviceName`, namespace, selector и EndpointSlice |
| Неготовый Pod не находится через DNS | Условия публикации адресов и необходимость `publishNotReadyAddresses` для этого приложения |
| Имя находится, но соединение не проходит | Актуальный IP, порт процесса, готовность приложения и сетевой путь |
| После замены Pod файлы отсутствуют | Тот ли PVC подключён, сохранился ли он, куда приложение записывало данные |

Если имя разрешается, проверь само соединение и логи нужного Pod.

## Проверь себя

**1. Нужно ли выбирать StatefulSet для любого приложения с PVC?**

<details>
<summary>Показать ответ</summary>

Нет. PVC доступен и обычному Pod, и Deployment. StatefulSet нужен, когда важны индивидуальные реплики: их постоянные имена, связь со своими данными или порядок операций.

</details>

**2. Удалили `database-1`, сохранив её PVC. Что сохранится, а что может измениться у замены?**

<details>
<summary>Показать ответ</summary>

Замена получит тот же ordinal, имя `database-1`, DNS-имя и PVC `data-database-1`. UID будет новым, IP и Node тоже могут измениться. Замена получает прежние данные, если хранилище сохранилось и доступно для подключения.

</details>

**3. Есть три реплики и один `volumeClaimTemplates` с именем `data`. Сколько PVC будет создано и получит ли `database-1` claim от `database-2`?**

<details>
<summary>Показать ответ</summary>

Три: data-database-0, data-database-1 и data-database-2. Реплика database-1 использует свой claim, а не claim соседа.

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

Нет. Replicas описывает желаемое количество, а OrderedReady ждёт готовности предшествующих реплик. Начни с причины неготовности первого Pod.

</details>

**7. StatefulSet уменьшили с трёх реплик до двух, затем снова увеличили до трёх. Что по умолчанию произойдёт с данными `database-2`?**

<details>
<summary>Показать ответ</summary>

Её Pod удалится, но PVC data-database-2 останется. При увеличении новый database-2 подключит сохранённый claim.

</details>

**8. Откатили шаблон StatefulSet после ошибочного обновления приложения. Что произойдёт с изменёнными данными на диске?**

<details>
<summary>Показать ответ</summary>

Данные останутся в текущем состоянии. Для их восстановления нужен механизм самого хранилища или приложения, например восстановление из резервной копии.

</details>
