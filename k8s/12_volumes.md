# 12. Volumes: тома Pod, эфемерное хранилище и emptyDir

## Какую проблему решаем

У каждого контейнера своя файловая система: файл в `/tmp` одного контейнера доступен только ему. При перезапуске контейнера его записываемый слой создаётся заново. Для обмена файлами и хранения данных вне этого слоя нужны тома.

**Volume (том) объявляется на уровне Pod, а каждый контейнер отдельно подключает его в свою файловую систему.** Том позволяет, например, обмениваться файлами между контейнерами или сохранять их при перезапуске контейнера. Срок жизни данных зависит от источника тома.

## Две части подключения: volumes и volumeMounts

```text
Pod
├── volumes: shared → источник emptyDir
├── контейнер writer
│   └── volumeMounts: shared → /out
└── контейнер reader
    └── volumeMounts: shared → /in

writer: /out/marker ── один файл в томе shared ── reader: /in/marker
```

`name` связывает объявление тома с подключением. `mountPath` — путь внутри конкретного контейнера. У двух контейнеров он может различаться. Контейнер получает доступ к тому через собственный `volumeMounts`.

В Deployment эти поля находятся внутри шаблона Pod: `spec.template.spec.volumes` и `spec.template.spec.containers[].volumeMounts`. Каждый созданный Pod получает свои объявления томов.

## Volume не обязательно означает постоянный диск

Volume — общий механизм подключения данных. `emptyDir`, `secret`, `configMap` и `persistentVolumeClaim` — разные источники тома, которые задаются внутри `volumes`.

| Источник | Для чего нужен | Что происходит при замене Pod |
|---|---|---|
| `emptyDir` | Временные файлы, кеш, обмен файлами между контейнерами | У нового Pod новый пустой том |
| `secret`, `configMap` | Передать настройки в виде файлов | Новый Pod получает файлы из соответствующего объекта API |
| `persistentVolumeClaim` | Подключить постоянное хранилище через PVC | Новый Pod может подключить тот же PVC с прежними данными |

**Эфемерные тома связаны с жизнью конкретного Pod. Постоянное хранилище существует независимо от него.** Объявление подключения всё равно находится в Pod: после его удаления исчезает это подключение, но судьба данных определяется источником.

Secret и ConfigMap уже встречались в [записи 8](08_configmap_secrets.md). Их тома эфемерные, но сами объекты API не удаляются вместе с Pod. Постоянное хранилище, PV и PVC разберём отдельно в [записи 13](13_pv_pvc.md).

## emptyDir: пустой при создании, общий внутри Pod

Правильное имя поля в YAML — **`emptyDir`**, с заглавной `D`, без дефиса. `emptyDir: {}` создаёт первоначально пустой том при назначении Pod на узел. PV, PVC и StorageClass для него не нужны.

| Событие | Что будет с файлом в emptyDir |
|---|---|
| Контейнер записал файл | Другой контейнер с подключением того же тома может прочитать файл |
| Контейнер завершился, kubelet запустил его снова в том же Pod | Файл остаётся |
| Pod удалён или вытеснен с узла | Данные теряются |
| Создан новый Pod, даже с прежним именем | У него новый пустой emptyDir |

По умолчанию том использует локальное хранилище узла. Он подходит для временных данных: при замене Pod создаётся новый пустой том.

## Пример: два контейнера подключают один том

В namespace `study` создадим Pod с двумя контейнерами BusyBox:

```yaml
# emptydir-demo.yaml
apiVersion: v1
kind: Pod
metadata:
  name: emptydir-demo
  namespace: study
spec:
  restartPolicy: Always
  containers:
    - name: writer
      image: busybox:1.36
      command:
        - sh
        - -c
        - |
          while [ ! -f /tmp/restart-request ]; do sleep 1; done
      volumeMounts:
        - name: shared    # Подключить объявленный ниже том
          mountPath: /out  # Путь в контейнере writer
    - name: reader
      image: busybox:1.36
      command: ["sh", "-c", "while true; do sleep 3600; done"]
      volumeMounts:
        - name: shared    # Тот же том
          mountPath: /in   # Путь в контейнере reader
          readOnly: true   # Запретить запись через это подключение
  volumes:                # Тома этого Pod
    - name: shared        # Имя для ссылок из volumeMounts
      emptyDir: {}        # Первоначально пустой временный том
```

Здесь **один том и два подключения**. `writer` записывает файлы через `/out`, `reader` читает их через `/in`.

Для проверки перезапуска создадим `/tmp/restart-request`: при его появлении цикл `writer` завершится. Файл находится вне тома и исчезнет при перезапуске контейнера. Данные в том записываем отдельной командой.

### 1. Проверить обмен файлами

Если namespace ещё не создан, сначала выполни `kubectl create namespace study`.

```bash
kubectl apply -f emptydir-demo.yaml
kubectl wait --for=condition=Ready pod/emptydir-demo -n study --timeout=60s

kubectl exec emptydir-demo -n study -c writer -- sh -c 'echo shared-data > /out/marker'
kubectl exec emptydir-demo -n study -c reader -- cat /in/marker
# Ожидаем: shared-data
```

Флаг `-c` выбирает контейнер. Имя файла одинаковое, но путь к нему зависит от `mountPath` выбранного контейнера.

### 2. Перезапустить только writer

```bash
kubectl exec emptydir-demo -n study -c writer -- touch /tmp/restart-request
kubectl get pod emptydir-demo -n study -w
```

Дождись увеличения `RESTARTS` и возврата к `2/2` готовым контейнерам. Останови наблюдение через `Ctrl+C`. Затем прочитай файл с обеих сторон:

```bash
kubectl exec emptydir-demo -n study -c writer -- cat /out/marker
kubectl exec emptydir-demo -n study -c reader -- cat /in/marker
# Оба чтения: shared-data
```

Kubelet перезапустил контейнер благодаря `restartPolicy: Always`. Pod остался прежним, поэтому его `emptyDir` сохранился.

### 3. Удалить Pod и создать замену

```bash
kubectl delete pod emptydir-demo -n study
kubectl apply -f emptydir-demo.yaml
kubectl wait --for=condition=Ready pod/emptydir-demo -n study --timeout=60s
kubectl exec emptydir-demo -n study -c reader -- ls -A /in
# Ожидаем: пустой вывод, файла marker нет
```

Pod создан напрямую, поэтому замену создаёт повторный `apply`. У нового Pod новый UID — уникальный идентификатор объекта — и новый `emptyDir`. Для двух Pod Deployment с `emptyDir` тоже получатся два независимых тома, даже если в шаблоне оба называются `shared`.

После проверки удали учебный Pod:

```bash
kubectl delete pod emptydir-demo -n study
```

## Хранение в памяти и ограничение размера

Вместо `emptyDir: {}` можно задать:

```yaml
emptyDir:
  medium: Memory # Хранить файлы в оперативной памяти
  sizeLimit: 64Mi # Ограничить размер тома
```

`medium: Memory` использует tmpfs — файловую систему в оперативной памяти. Записанные файлы расходуют память и учитываются в лимите памяти записавшего их контейнера. `sizeLimit` задаёт верхнюю границу размера. Доступная ёмкость зависит от свободных ресурсов узла. Ограничение можно задать и для обычного дискового `emptyDir`. В обоих случаях том живёт до удаления Pod.

## Проверь себя

**1. В Pod объявлен том shared. Получат ли его все контейнеры автоматически?**

<details>
<summary>Показать ответ</summary>

Нет. Каждый контейнер подключает нужный том через собственный `volumeMounts`.

</details>

**2. У writer том подключён к /out, у reader — к /in. Где reader увидит файл /out/marker, созданный writer?**

<details>
<summary>Показать ответ</summary>

В /in/marker, если оба подключают один том. MountPath задаётся отдельно для каждого контейнера.

</details>

**3. Контейнер перезапустился, а затем весь Pod удалили и создали снова с тем же именем. Когда потеряется файл в emptyDir?**

<details>
<summary>Показать ответ</summary>

При удалении прежнего Pod. Перезапуск контейнера внутри того же Pod сохраняет том. Новый Pod получает новый пустой emptyDir.

</details>

**4. В Deployment две реплики и том emptyDir с именем shared. Общие ли у реплик файлы?**

<details>
<summary>Показать ответ</summary>

Нет. У каждого Pod свой emptyDir. Совпадение имени тома в разных Pod не связывает их хранилища.

</details>

**5. Чем отличается срок жизни данных в emptyDir и в постоянном хранилище через PVC?**

<details>
<summary>Показать ответ</summary>

`emptyDir` удаляется вместе с Pod. Постоянное хранилище через сохраняемый PVC позволяет новому Pod подключить прежние данные.

</details>
