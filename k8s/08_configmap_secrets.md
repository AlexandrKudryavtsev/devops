# 8. ConfigMap и Secret: как передать настройки приложению

## Какую проблему решаем

Приложению нужны три настройки:

- `LOG_LEVEL=info` — насколько подробные логи писать;
- `username=appuser` — логин для подключения к базе;
- `password=practice-pass` — пароль для подключения к базе.

Хранить их внутри образа неудобно: для смены настроек пришлось бы пересобирать образ. Вместо этого создаём отдельные объекты Kubernetes:

| Объект | Что положим в него |
|---|---|
| ConfigMap | Уровень логирования — обычную настройку |
| Secret | Логин и пароль — данные для доступа |

**Объекты хранят данные. Чтобы приложение получило эти данные, нужно отдельно описать подключение в Pod.**

## Объекты с данными

Пример ConfigMap и Secret; `---` разделяет два объекта:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-settings
  namespace: study
data:
  LOG_LEVEL: "info"
---
apiVersion: v1
kind: Secret
metadata:
  name: app-credentials
  namespace: study
type: Opaque
stringData:
  username: "appuser"
  password: "practice-pass"
```

`data` ConfigMap содержит строки. `Opaque` означает обычный Secret с произвольными ключами. `stringData` позволяет записать значения Secret обычным текстом; пароль в примере учебный.

При чтении Secret через API его значения находятся в `data` в формате base64. **Base64 — способ записи данных, а не шифрование:** значение можно декодировать обратно. Для защиты Secret нужны права доступа и настройки хранения. См. [документацию Secret](https://kubernetes.io/docs/concepts/configuration/secret/).

Все примеры используют namespace `study` из предыдущих записей.

## Передача данных контейнеру

Есть два способа: **переменные окружения** и **файлы**. Покажем оба в одном примере:

```text
ConfigMap app-settings → переменная LOG_LEVEL внутри контейнера
Secret app-credentials → файлы username и password внутри контейнера
```

Оба объекта поддерживают оба способа. Здесь выбрано такое сочетание для обучения.

Пример Deployment с обоими способами передачи:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: config-demo
  namespace: study
spec:
  replicas: 1
  selector:
    matchLabels:
      app: config-demo
  template:
    metadata:
      labels:
        app: config-demo
    spec:
      containers:
        - name: app
          image: busybox:1.36
          command: ["sh", "-c", "sleep 3600"]
          envFrom:
            - configMapRef:
                name: app-settings
          volumeMounts:
            - name: credentials
              mountPath: /etc/app-credentials
              readOnly: true
      volumes:
        - name: credentials
          secret:
            secretName: app-credentials
```

BusyBox — учебный контейнер, в котором можно выполнить команды и проверить данные. `sleep 3600` оставляет его работающим на час. Подключение к базе здесь не выполняется: мы проверяем передачу настроек.

### Откуда берётся переменная

`envFrom` берёт все ключи указанного ConfigMap и делает их переменными окружения контейнера. Поэтому внутри него будет `LOG_LEVEL=info`.

### Откуда берутся файлы

**Volume (том) объявляется в Pod, а контейнер подключает его в свою файловую систему.** В этом примере файлы создаются из ключей Secret; основы томов и пример временного `emptyDir` разобраны отдельно в [записи 12](12_volumes.md).

Конфигурация состоит из двух частей:

- `volumes` в спецификации Pod объявляет volume и указывает, откуда взять данные;
- `volumeMounts` в описании конкретного контейнера указывает, какой volume подключить и в какую папку.

| Поле | Что означает |
|---|---|
| `volumes[].name: credentials` | Имя volume. Слово `credentials` выбрано нами и может быть другим |
| `secretName: app-credentials` | Имя существующего Secret, из которого берём данные |
| `volumeMounts[].name: credentials` | Какой volume подключить. Должно совпадать с именем в `volumes` |
| `mountPath: /etc/app-credentials` | Папка, где контейнер увидит файлы |
| `readOnly: true` | Подключить только для чтения |

По умолчанию **все ключи Secret становятся файлами с такими же именами**:

```text
/etc/app-credentials/username  содержит appuser
/etc/app-credentials/password  содержит practice-pass
```

Приложение должно уметь читать эти файлы. Kubernetes не подставляет пароль в код приложения автоматически.

### Проверить результат

```bash
kubectl exec deployment/config-demo -n study -- printenv LOG_LEVEL
# Ожидаем: info

kubectl exec deployment/config-demo -n study -- cat /etc/app-credentials/username
# Ожидаем: appuser

kubectl exec deployment/config-demo -n study -- ls /etc/app-credentials
# Ожидаем два файла: password и username
```

## Если приложению нужны другие имена файлов: `items`

Допустим, приложение ожидает файл `login`, а ключ в Secret называется `username`. Можно задать имя файла через **`items` — список ключей, которые нужно превратить в файлы**.

Объявление volume в этом случае:

```yaml
volumes:
  - name: credentials
    secret:
      secretName: app-credentials
      items:
        - key: username
          path: login
        - key: password
          path: password
```

`key` — какой ключ взять из Secret. `path` — как назвать файл внутри volume. При прежнем `mountPath` получатся:

```text
/etc/app-credentials/login     содержит appuser
/etc/app-credentials/password  содержит practice-pass
```

**Когда задан `items`, в volume попадут только перечисленные ключи.** Здесь указаны оба, потому что приложению нужны и логин, и пароль. Если убрать запись для `password`, приложение не получит пароль через этот volume.

## Если нужно добавить файлы в существующую папку: `subPath`

Допустим, в образе уже есть `/etc/app/settings.yaml`, а логин и пароль тоже нужно разместить в `/etc/app`.

Если подключить весь volume в `/etc/app`, существующий `settings.yaml` станет не виден: подключённый volume закроет содержимое папки.

**`subPath` выбирает отдельный файл или подпапку внутри volume для подключения.** С его помощью можно добавить только нужные файлы, сохранив видимость остальных.

Подключение файлов `login` и `password` из предыдущего раздела:

```yaml
volumeMounts:
  - name: credentials
    subPath: login             # взять файл login из volume
    mountPath: /etc/app/login  # подключить его по этому пути
    readOnly: true
  - name: credentials
    subPath: password
    mountPath: /etc/app/password
    readOnly: true
```

Результат:

```text
/etc/app/
├── settings.yaml  ← существующий файл из образа
├── login          ← файл из volume
└── password       ← файл из volume
```

`subPath` указывает **что взять внутри volume**, а `mountPath` — **где это будет видно в контейнере**. Для отдельного файла `mountPath` задаёт полный путь к файлу, а не только папку.

## Что будет, если изменить настройки

Изменение ConfigMap или Secret само по себе не запускает обновление Deployment. Результат зависит от того, как переданы данные:

| Способ | Что произойдёт |
|---|---|
| Переменная окружения | Работающий процесс сохранит старое значение. Например, после смены ConfigMap на `debug` он всё ещё увидит `info` |
| Volume подключён целой папкой | Kubernetes обновит файлы с задержкой. Приложение должно перечитать их, чтобы использовать новые значения |
| Файл подключён через `subPath` | Автоматического обновления файла не будет. Нужно пересоздать Pod |

Если приложение читает файлы только при запуске, обновление обычного volume тоже не заставит его применить настройки. Для Deployment пересоздать Pod можно так:

```bash
kubectl rollout restart deployment/config-demo -n study
kubectl rollout status deployment/config-demo -n study --timeout=60s
```

Если контейнер не запускается, проверь существование ConfigMap и Secret, их имена, namespace и нужные ключи. Причину ищи через `kubectl describe pod POD -n study`, где `POD` — имя проблемного Pod.

Особенности обновления описаны в документации [ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/) и [Secret](https://kubernetes.io/docs/concepts/configuration/secret/#using-secrets-as-files-from-a-pod).

## Проверь себя

**1. Secret создан. Получит ли приложение пароль без изменений в Pod?**

<details>
<summary>Показать ответ</summary>

Нет. Нужно явно передать данные контейнеру через окружение или файлы.

</details>

**2. Какие имена должны совпасть: имя Secret и volume или имена в `volumes` и `volumeMounts`?**

<details>
<summary>Показать ответ</summary>

Имена в `volumes` и `volumeMounts`. Имя Secret указывается отдельно в `secretName`.

</details>

**3. Приложению нужны логин и пароль. В `items` указан только `username`. Получит ли оно пароль через этот volume?**

<details>
<summary>Показать ответ</summary>

Нет. Добавь `password` в `items` или убери `items`, чтобы подключить все ключи.

</details>

**4. Зачем использовать `subPath`, если можно подключить весь volume в папку?**

<details>
<summary>Показать ответ</summary>

Чтобы подключить отдельные файлы и сохранить видимость существующих файлов этой папки.

</details>

**5. ConfigMap изменён с `info` на `debug`. Как работающий контейнер с `envFrom` получит новое значение?**

<details>
<summary>Показать ответ</summary>

Пересоздай Pod Deployment через `rollout restart`, затем проверь результат. Окружение работающего процесса автоматически не обновляется.

</details>
