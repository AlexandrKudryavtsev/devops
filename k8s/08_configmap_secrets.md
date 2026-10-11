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

`---` разделяет объекты в одном YAML-файле:

```yaml
# app-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-settings
  namespace: study
data:                 # Строковые настройки ConfigMap
  LOG_LEVEL: "info"
---
apiVersion: v1
kind: Secret
metadata:
  name: app-credentials
  namespace: study
type: Opaque          # Secret с произвольными ключами
stringData:           # Значения обычным текстом
  username: "appuser"
  password: "practice-pass"
```

При чтении Secret через API его значения находятся в `data` в формате base64. **Base64 — способ записи данных, а не шифрование:** значение можно декодировать обратно. Для защиты Secret нужны права доступа и настройки хранения.

## Передача данных контейнеру

Есть два способа: **переменные окружения** и **файлы**. Покажем оба в одном примере:

```text
ConfigMap app-settings → переменная LOG_LEVEL внутри контейнера
Secret app-credentials → файлы username и password внутри контейнера
```

ConfigMap и Secret поддерживают оба способа передачи.

```yaml
# config-demo.yaml
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
          envFrom:             # Все ключи ConfigMap станут переменными окружения
            - configMapRef:
                name: app-settings
          volumeMounts:
            - name: credentials # Имя тома из volumes
              mountPath: /etc/app-credentials # Папка с файлами
              readOnly: true    # Запретить запись через это подключение
      volumes:                 # Объявление томов на уровне Pod
        - name: credentials    # Произвольное имя тома
          secret:
            secretName: app-credentials # Secret, из которого берём данные
```

BusyBox позволяет проверить настройки командами. `sleep 3600` оставляет контейнер работающим на час.

### Как подключаются файлы

**Volume (том) объявляется в Pod, а контейнер подключает его в свою файловую систему.** В этом примере файлы создаются из ключей Secret. Основы томов и пример временного `emptyDir` разобраны отдельно в [записи 12](12_volumes.md).

По умолчанию **все ключи Secret становятся файлами с такими же именами**:

```text
/etc/app-credentials/username  содержит appuser
/etc/app-credentials/password  содержит practice-pass
```

Приложение получает пароль, читая соответствующий файл.

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
        - key: username # Ключ в Secret
          path: login   # Имя файла внутри тома
        - key: password
          path: password
```

При прежнем `mountPath` получатся:

```text
/etc/app-credentials/login     содержит appuser
/etc/app-credentials/password  содержит practice-pass
```

**Когда задан `items`, в volume попадут только перечисленные ключи.** Поэтому для логина и пароля указываем оба.

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

## Что будет, если изменить настройки

После изменения ConfigMap или Secret результат зависит от способа передачи данных:

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
