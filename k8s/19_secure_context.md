# 19. SecurityContext: права процесса и доступ к файловой системе

## Какую проблему решаем

В [записи 18](18_rbac.md) мы ограничивали действия клиента в Kubernetes API. Но внутри контейнера работает обычный процесс: у него есть Linux-пользователь, группы, права на файлы и возможности выполнять привилегированные операции. Разрешение читать Pod через API ничего не говорит о том, может ли этот процесс писать в `/etc` внутри контейнера.

**`securityContext` задаёт условия запуска и ограничения процесса в контейнере.** Главный вопрос: какие права действительно нужны приложению для работы? Здесь рассматриваем Linux-контейнеры на Linux-узлах.

```text
процесс внутри контейнера
├── кто запускает?          → UID и GID
├── какие привилегии есть?   → capabilities и privilege escalation
├── куда можно писать?      → файловая система и подключения томов
└── какие syscall доступны? → seccomp
```

Настройки должны соответствовать приложению: если оно пишет временные файлы, ему нужен доступный каталог; если образ рассчитан на root, смена UID может потребовать подготовки прав в образе.

## Два уровня: Pod и контейнер

| Где задаётся | Для чего используется | Примеры полей |
|---|---|---|
| `spec.securityContext` | Общие настройки контейнеров Pod и работа с правами томов | `runAsUser`, `runAsGroup`, `runAsNonRoot`, `fsGroup`, `seccompProfile` |
| `spec.containers[].securityContext` | Настройки конкретного контейнера | `runAsUser`, `runAsGroup`, `runAsNonRoot`, `allowPrivilegeEscalation`, `capabilities`, `readOnlyRootFilesystem`, `privileged`, `seccompProfile` |

Если поле поддерживается на обоих уровнях, значение контейнера имеет приоритет для этого контейнера. Например, при UID `1000` у Pod и UID `1001` у одного контейнера именно этот контейнер запускается как `1001`.

**Не все поля можно перенести с одного уровня на другой.** `fsGroup` задаётся у Pod, а `capabilities`, `allowPrivilegeEscalation` и `readOnlyRootFilesystem` — у конкретного контейнера. В Deployment настройки находятся внутри `spec.template.spec`; изменение шаблона запускает rollout, как в [записи 6](06_rollout.md). Описание полей — в [справочнике API Pod](https://kubernetes.io/docs/reference/kubernetes-api/core/pod-v1/).

## UID, GID и требование non-root

| Поле | Что означает |
|---|---|
| `runAsUser: 1000` | Запустить процесс с UID `1000` |
| `runAsGroup: 3000` | Использовать GID `3000` как основную группу процесса |
| `runAsNonRoot: true` | Требовать запуск с UID, отличным от `0` |

`runAsUser` выбирает пользователя, а `runAsNonRoot` проверяет требование. **`runAsNonRoot: true` сам по себе не выбирает ненулевой UID.** Если эффективный пользователь — root, kubelet не запустит контейнер. Если в образе пользователь указан именем и kubelet не может подтвердить его UID, запуск тоже может быть отклонён; явный числовой `runAsUser` устраняет эту неопределённость.

Без переопределения используется пользователь, заданный образом или настройками runtime; контейнер вполне может запуститься как root. UID `1000` не создаёт запись пользователя в `/etc/passwd` и не меняет владельцев файлов образа. Процесс получает заданный UID, а доступ к файлам проверяется по их владельцу, группе и режиму.

## Привилегии: allowPrivilegeEscalation и capabilities

`allowPrivilegeEscalation: false` включает для процесса Linux-флаг `no_new_privs`: запуск другой программы через `exec` не должен давать дополнительные привилегии, например через setuid или файловые capabilities. **Это не убирает уже имеющиеся capabilities и не выбирает пользователя.**

Linux capabilities разделяют привилегированные действия на отдельные возможности. Например, `NET_ADMIN` позволяет выполнять ряд операций с сетевыми настройками. Если такие действия не нужны, контейнеру задают:

```yaml
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
```

`drop: [ALL]` убирает capabilities; обычные действия, например чтение доступного файла или запись в разрешённый каталог, остаются возможными. `CapEff` в `/proc/1/status` показывает эффективный набор capabilities процесса с PID `1`.

`privileged: true` существенно расширяет доступ контейнера и ослабляет ряд ограничений изоляции. Он несовместим с целью нашего примера; используем `privileged: false`. Кроме того, `allowPrivilegeEscalation` всегда считается включённым для privileged-контейнера или процесса с `CAP_SYS_ADMIN`. Эти механизмы описаны в [документации securityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/).

## Файловая система: запрет записи и отдельное место для работы

`readOnlyRootFilesystem: true` делает корневую файловую систему контейнера доступной только для чтения. Если `/tmp` находится в ней, запись в `/tmp` тоже запрещается.

**Подключённый том может оставаться доступным для записи.** Для него отдельно действуют настройки подключения и права файлов. В [записи 12](12_volumes.md) том объявлялся у Pod, а контейнер подключал его через `volumeMounts`; здесь тот же механизм предоставляет приложению рабочий каталог.

```text
контейнер
├── /             → корневая файловая система, только чтение
│   └── /tmp      → запись запрещена, если отдельный том не подключён
└── /work         → подключённый emptyDir, запись разрешена
```

`volumeMounts[].readOnly: true` запрещает запись через конкретное подключение тома. `readOnlyRootFilesystem` и `readOnly` у подключения решают разные задачи. Такой способ выдавать приложению необходимые права соответствует [рекомендациям по безопасности приложений](https://kubernetes.io/docs/concepts/security/application-security-checklist/).

## fsGroup: групповая принадлежность и права тома

`runAsGroup` задаёт основную группу процесса. **`fsGroup` добавляет процессам Pod дополнительную группу и участвует в подготовке прав поддерживаемых томов.** В нашем примере основная группа — `3000`, а группа для работы с томом — `2000`.

Для `emptyDir` Kubernetes подготавливает каталог с группой `2000` и групповым доступом. В этом каталоге новые файлы наследуют группу благодаря биту setgid. Поэтому у файла может быть UID `1000`, GID `2000`, хотя основная группа процесса — `3000`.

`fsGroup` не меняет владельцев файлов корневой файловой системы образа и не отменяет подключение `readOnly`. Для других источников томов результат зависит от поддержки и настроек хранилища; у некоторых CSI-драйверов права подготавливает сам драйвер. Если запись не работает, проверяй реальные UID, группы и права каталога. См. [настройку групп и прав томов](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-pod).

## Seccomp: фильтрация системных вызовов

Системный вызов, или syscall, — обращение процесса к ядру Linux, например для открытия файла или создания процесса. `seccompProfile` выбирает профиль фильтрации таких вызовов.

В примере `type: RuntimeDefault` включает стандартный профиль container runtime. Его конкретные правила зависят от runtime; это отдельный механизм, дополняющий UID, capabilities и права файлов. Он не определяет, какие каталоги доступны для записи. См. [пример использования seccomp](https://kubernetes.io/docs/tutorials/security/seccomp/).

## Цельный пример: non-root и рабочий emptyDir

Сохрани манифест как `security-context-demo.yaml`. BusyBox позволяет проверить права без дополнительных требований веб-сервера или базы данных.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: security-context-demo
  namespace: study
spec:
  nodeSelector:
    kubernetes.io/os: linux
  securityContext:
    runAsUser: 1000
    runAsGroup: 3000
    runAsNonRoot: true
    fsGroup: 2000
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: app
      image: busybox:1.36
      command: ["sh", "-c", "while true; do sleep 3600; done"]
      securityContext:
        privileged: false
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop:
            - ALL
      volumeMounts:
        - name: work
          mountPath: /work
  volumes:
    - name: work
      emptyDir: {}
```

Здесь настройки дополняют друг друга: UID задаёт пользователя, `fsGroup` помогает работать с томом, корневая файловая система закрыта для записи, а `/work` предоставлен отдельно. `emptyDir` остаётся эфемерным: права доступа не меняют срок жизни данных.

### Проверить пользователя, группы и ограничения процесса

Если namespace ещё не существует, сначала выполни `kubectl create namespace study`.

```bash
kubectl apply -f security-context-demo.yaml
kubectl wait --for=condition=Ready pod/security-context-demo -n study --timeout=60s

kubectl exec security-context-demo -n study -c app -- id
# UID: 1000; основная GID: 3000; среди групп есть 2000

kubectl exec security-context-demo -n study -c app -- sh -c \
  'grep -E "^(CapEff|NoNewPrivs|Seccomp):" /proc/1/status'
# CapEff:      0000000000000000
# NoNewPrivs:  1
# Seccomp:     2
```

Имена пользователей, групп и формат вывода могут различаться; сравнивай числовые значения. `Seccomp: 2` означает режим фильтрации, но не раскрывает правила профиля. Эти проверки показывают настройки главного процесса в нашем контейнере, а не проверяют всю безопасность приложения.

### Проверить запрет записи и доступный том

```bash
kubectl exec security-context-demo -n study -c app -- sh -c 'echo blocked > /tmp/marker'
# Ожидаем ошибку Read-only file system и ненулевой код завершения команды

kubectl exec security-context-demo -n study -c app -- sh -c 'echo allowed > /work/marker'
kubectl exec security-context-demo -n study -c app -- cat /work/marker
# Ожидаем: allowed

kubectl exec security-context-demo -n study -c app -- ls -ldn /work /work/marker
# У /work группа 2000; у marker владелец 1000 и группа 2000
```

Первая ошибка ожидаема: завершился процесс учебной команды `exec`, а основной контейнер продолжает работать. Запись в `/work` проверяет отдельное подключение и права тома; она не означает, что приложение может писать в любой каталог.

После проверки удали учебный Pod:

```bash
kubectl delete pod security-context-demo -n study
```

## Если контейнер не запускается или запись запрещена

Как в [записи 3](03_debug.md), сначала проверь Events Pod, затем логи запущенного контейнера и фактические права.

```bash
kubectl describe pod security-context-demo -n study
kubectl logs security-context-demo -n study -c app
kubectl get pod security-context-demo -n study -o yaml
```

| Наблюдение | Что проверить |
|---|---|
| API отклоняет манифест из-за неизвестного поля | Уровень поля: Pod или контейнер; написание и версию API |
| Events сообщают о нарушении `runAsNonRoot` | Эффективный UID: настройки контейнера, Pod и пользователь образа |
| Приложение завершается с `Permission denied` | UID, группы, владельца и режим нужного файла или каталога |
| Запись завершается с `Read-only file system` | Корневую файловую систему и `readOnly` у подключения тома |
| `fsGroup` задан, но запись в PVC не работает | Поддержку драйвера, фактические права и доступность подключения для записи |
| После включения seccomp запрещена операция | Какой syscall нужен приложению и какое правило профиля его ограничивает |
| API отклоняет создание из-за политики Pod Security | Сообщение admission и требования политики namespace |

SecurityContext задаёт настройки запуска. Политики Pod Security могут проверять допустимость этих настроек ещё при создании Pod; они не заменяют сами настройки. Например, профиль Restricted требует ряд ограничений, включая non-root, запрет повышения привилегий и подходящий seccomp-профиль. См. [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/).

## Проверь себя

**1. Приложению разрешено читать Secret через RBAC. Запустится ли оно из-за этого как root?**

<details>
<summary>Показать ответ</summary>

Нет. RBAC регулирует запросы к API. Linux-пользователь процесса определяется образом и настройками запуска, включая securityContext.

</details>

**2. Указали только runAsNonRoot: true, а образ запускается с UID 0. Kubernetes автоматически выберет UID 1000?**

<details>
<summary>Показать ответ</summary>

Нет. RunAsNonRoot проверяет требование; контейнер с UID 0 не запустится. Для явного выбора пользователя нужен runAsUser с подходящим ненулевым UID.

</details>

**3. У Pod runAsUser: 1000, у контейнера app runAsUser: 1001. С каким UID запустится app? Можно ли так же задать fsGroup у контейнера?**

<details>
<summary>Показать ответ</summary>

App запустится с UID 1001: его настройка имеет приоритет. FsGroup доступен только на уровне Pod.

</details>

**4. AllowPrivilegeEscalation: false уже задан. Значит ли это, что capabilities автоматически удалены?**

<details>
<summary>Показать ответ</summary>

Нет. Запрет получения новых привилегий не убирает имеющиеся возможности. Capabilities задаются отдельно; в примере используется drop: [ALL].

</details>

**5. ReadOnlyRootFilesystem: true. Почему запись в /tmp не работает, а в /work работает?**

<details>
<summary>Показать ответ</summary>

В примере /tmp принадлежит корневой файловой системе, доступной только для чтения. В /work подключён отдельный emptyDir с доступом для записи и подходящими правами.

</details>

**6. RunAsGroup: 3000 и fsGroup: 2000. Одинаковая ли у полей задача? Сделает ли fsGroup доступным для записи том с readOnly: true?**

<details>
<summary>Показать ответ</summary>

Нет. RunAsGroup задаёт основную группу; fsGroup добавляет группу и участвует в подготовке прав поддерживаемых томов. Он не отменяет readOnly у подключения.

</details>

**7. Seccomp: 2 в /proc/1/status доказывает, что процесс не может писать в /work?**

<details>
<summary>Показать ответ</summary>

Нет. Это признак фильтрации syscall. Доступ к /work также зависит от подключения, прав файлов и UID/GID процесса; в нашем примере запись туда разрешена.

</details>

**8. В /work успешно записали файл. Переживёт ли он удаление Pod благодаря fsGroup?**

<details>
<summary>Показать ответ</summary>

Нет. FsGroup относится к доступу, а срок жизни определяется источником тома. В примере используется emptyDir, поэтому вместе с Pod теряются и его данные.

</details>
