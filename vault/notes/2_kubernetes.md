# Интеграция Vault и Kubernetes

## 1. Назначение интеграции

Интеграция Vault с Kubernetes позволяет приложениям **безопасно получать секреты без хранения постоянных Vault-токенов и паролей в манифестах Kubernetes**.

Основные возможности:

- **Workload Authentication** - аутентификация Pod в Vault через Kubernetes ServiceAccount.
- **Access Control** - разграничение доступа к секретам через Vault Policies.
- **Secrets Injection** - автоматическая доставка секретов в контейнеры.
- **Secret Rotation** - обновление секретов без обязательного пересоздания Pod.
- **Dynamic Secrets** - получение временных credentials с управляемым сроком действия.

При этом Vault может работать как внутри Kubernetes-кластера, так и вне его.

---

## 2. Архитектура интеграции

В интеграции участвуют следующие компоненты:

- **Vault Server** - HTTP-сервер, отвечающий за аутентификацию, авторизацию и работу с секретами.
- **Kubernetes Auth Method** - механизм аутентификации в Vault через ServiceAccount JWT.
- **Vault Agent** - клиент, автоматически получающий и обновляющий секреты.
- **Vault Agent Injector** - Kubernetes Mutating Admission Webhook, который добавляет Vault Agent в Pod.
- **Kubernetes ServiceAccount** - идентичность приложения внутри Kubernetes.

Общая схема:

```text
                     Kubernetes Cluster
┌──────────────────────────────────────────────────┐
│                                                  │
│  Kubernetes API Server                           │
│       │                                          │
│       │ Admission Webhook                        │
│       ▼                                          │
│  Vault Agent Injector                            │
│       │                                          │
│       │ Изменяет Pod Spec при создании           │
│       ▼                                          │
│  Application Pod                                 │
│  ├── Application Container                       │
│  ├── Vault Agent Sidecar                         │
│  ├── Vault Agent Init Container                  │
│  └── Shared Volume (/vault/secrets)              │
│                 │                                │
└─────────────────┼────────────────────────────────┘
                  │ HTTP(S)
                  ▼
              Vault Server
                  │
                  ├── Kubernetes Auth
                  ├── Vault Policies
                  └── Secrets Engines
```

**Главное различие:** Injector изменяет конфигурацию Pod, а Vault Agent непосредственно взаимодействует с Vault Server и доставляет секреты.

---

## 3. Kubernetes Auth Method

**Kubernetes Auth** - реализация Vault Auth Method, которая позволяет приложениям аутентифицироваться через Kubernetes ServiceAccount JWT.

### Принцип работы

```text
Application Pod
      │
      │ ServiceAccount JWT
      ▼
Vault Kubernetes Auth
      │
      │ TokenReview
      ▼
Kubernetes API Server
      │
      │ Подтверждение идентичности
      ▼
Vault Kubernetes Auth Role
      │
      │ Проверка ServiceAccount и Namespace
      ▼
Vault Token + Policies
```

Последовательность:

1. Kubernetes выдает Pod-у JWT его ServiceAccount.
2. Vault Agent передает JWT в Vault.
3. Vault проверяет JWT через Kubernetes TokenReview API.
4. Vault проверяет, соответствует ли ServiceAccount настройкам Kubernetes Auth Role.
5. При успехе Vault выдает собственный токен с назначенными Policies.

После аутентификации клиент обращается к Secrets Engines уже с Vault Token.

### ServiceAccount и TokenReview

**ServiceAccount** - идентичность workload внутри Kubernetes.

JWT ServiceAccount-а обычно доступен внутри Pod по пути:

```text
/var/run/secrets/kubernetes.io/serviceaccount/token
```

**TokenReview** - API Kubernetes, позволяющий проверить действительность JWT и установить идентичность его владельца.

Для обращения к TokenReview Vault должен иметь соответствующие разрешения Kubernetes RBAC. Обычно используется ClusterRole `system:auth-delegator`.

Важно различать две идентичности:

- **ServiceAccount приложения** - JWT которого Vault проверяет.
- **ServiceAccount Vault Server** - может использоваться Vault для авторизованного обращения к Kubernetes TokenReview API.

### Kubernetes Auth Role

**Kubernetes Auth Role** - конфигурация внутри Auth Method, связывающая допустимые Kubernetes ServiceAccounts с выдаваемыми Vault Policies.

Например:

```bash
vault write auth/kubernetes/role/myapp-role \
  bound_service_account_names=myapp \
  bound_service_account_namespaces=demo \
  token_policies=myapp-policy \
  token_ttl=1h
```

Параметры:

- `bound_service_account_names` - разрешенные ServiceAccounts.
- `bound_service_account_namespaces` - разрешенные namespaces.
- `token_policies` - Policies выдаваемого Vault-токена.
- `token_ttl` - первоначальный срок действия токена.

В production также следует учитывать ограничение JWT по `audience`.

**Kubernetes Auth Role не является Kubernetes RBAC Role.** Это конфигурация Vault, которая определяет правила аутентификации и параметры выдаваемого Vault-токена.

### Настройка Kubernetes Auth

```bash
# Включение Auth Method
vault auth enable kubernetes

# Адрес Kubernetes API
vault write auth/kubernetes/config \
  kubernetes_host="https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT}"
```

При работе Vault внутри Kubernetes он может использовать локальные ServiceAccount JWT и CA-сертификат. Для внешнего Vault потребуется соответствующая конфигурация доступа к Kubernetes API.

Для ручной аутентификации используется HTTP API:

```http
POST /v1/auth/kubernetes/login
```

Тело запроса содержит `role` и `jwt`.

Результат - Vault Token, его Policies и параметры времени жизни.

---

## 4. Способы доставки секретов в Kubernetes

Аутентификация через Kubernetes Auth **сама по себе не доставляет секреты приложению**. Она только позволяет получить Vault Token.

Для доставки секретов используются отдельные механизмы.

Основные реализации:

- **Vault Agent Injector** - автоматически внедряет Vault Agent в Pod, который записывает секреты в общий volume.
- **Vault Secrets Operator (VSO)** - Kubernetes Operator, синхронизирующий секреты из Vault с Kubernetes-ресурсами, обычно с обычными Kubernetes Secrets.
- **Vault Secrets Store CSI Provider** - интеграция через Secrets Store CSI Driver, монтирующая секреты в файловую систему Pod.
- **Прямое использование Vault API/SDK** - приложение самостоятельно аутентифицируется и запрашивает секреты.

Выбор зависит от требований к безопасности, ротации, формату доставки и поддержке Vault внутри приложения.

---

## 5. Vault Agent и Vault Agent Injector

### Vault Agent

**Vault Agent** - клиент Vault, который автоматизирует работу с секретами.

Основные возможности:

- **Auto-Auth** - автоматическая аутентификация в Vault.
- **Token Renewal** - продление Vault-токенов.
- **Template Rendering** - получение секретов и формирование конфигурационных файлов.
- **Secrets Refresh** - обновление секретов и управление leases.
- **Caching** - опциональное кэширование токенов и секретов.

Vault Agent может работать не только в Kubernetes: это самостоятельный компонент Vault.

### Vault Agent Injector

**Vault Agent Injector** - реализация Kubernetes Mutating Admission Webhook, автоматически добавляющая Vault Agent и необходимые volumes в Pod.

Сам Injector не читает секреты приложения. Он модифицирует Pod Spec при создании Pod.

Обычно внедряются:

- **Init Container (`vault-agent-init`)** - получает начальные секреты до запуска основных контейнеров.
- **Sidecar (`vault-agent`)** - продолжает работу после запуска приложения, обновляя секреты и управляя аутентификацией.
- **Shared Volume** - общий том, через который приложение получает секреты.

```text
Application Pod
│
├── Init Container: vault-agent-init
│   └── Получает начальные секреты
│
├── Container: application
│   └── Читает /vault/secrets/config
│
├── Sidecar: vault-agent
│   └── Обновляет секреты
│
└── Shared Volume
    └── /vault/secrets/
```

Init-контейнер не является обязательным для каждой конфигурации, а sidecar можно отключить, если требуется только первоначальная доставка секретов.

### Mutating Admission Webhook

**Mutating Admission Webhook** - механизм Kubernetes, позволяющий внешнему сервису изменить спецификацию объекта во время обработки запроса к Kubernetes API.

В случае Injector:

1. Kubernetes API Server получает запрос на создание Pod.
2. Вызывает зарегистрированный Mutating Webhook.
3. Injector проверяет аннотации Pod.
4. При необходимости возвращает изменения Pod Spec.
5. Kubernetes создает Pod уже с добавленными контейнерами и volumes.

Injector действует **на этапе создания Pod**, поэтому существующие Pod не получают Vault Agent автоматически после изменения настроек Injector.

---

## 6. Конфигурация Vault Agent Injector

Инъекция обычно управляется аннотациями Pod. Для всех аннотаций используется префикс `vault.hashicorp.com/`. Обычно в работе используются такие аннотации:

- `agent-inject` - включить инъекцию Vault Agent
- `role` - указать Vault Auth Role 
- `agent-inject-secret-NAME` - путь секрета в Vault
- `agent-inject-template-NAME` - шаблон содержимого файла 
- `agent-inject-file-NAME` - имя выходного файла
- `secret-volume-path` - путь монтирования секретов
- `agent-pre-populate-only` - использовать только init-контейнер
- `template-static-secret-render-interval` - интервал обработки статических секретов

### Пример Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: myapp
  namespace: demo
  annotations:
    vault.hashicorp.com/agent-inject: "true"
    vault.hashicorp.com/role: "myapp-role"

    vault.hashicorp.com/agent-inject-secret-config: "secret/data/myapp"
    vault.hashicorp.com/agent-inject-template-config: |
      {{- with secret "secret/data/myapp" -}}
      DB_PASSWORD={{ .Data.data.password }}
      {{- end }}

    vault.hashicorp.com/template-static-secret-render-interval: "30s"

spec:
  serviceAccountName: myapp
  containers:
    - name: app
      image: busybox:1.36
      command: ["sh", "-c", "sleep 3600"]
```

После успешной инъекции секрет будет доступен приложению в файле:

```text
/vault/secrets/config
```

Содержимое:

```text
DB_PASSWORD=...
```

### Agent Templates

**Vault Agent Templates** - механизм преобразования ответа Vault API в конфигурационный файл приложения. В нем используется синтаксис Go Templates с функциями Vault Agent. Такой же синтаксис используется в Helm.

Для KV v2:

```yaml
vault.hashicorp.com/agent-inject-template-config: |
  {{- with secret "secret/data/myapp" -}}
  DB_PASSWORD={{ .Data.data.password }}
  {{- end }}
```

`.Data.data.password` используется потому, что KV v2 возвращает значения внутри вложенного объекта `data`.

Без пользовательского шаблона Agent может записать весь ответ Vault, включая метаданные, в стандартном формате.

---

## 7. Обновление и ротация секретов

Vault Agent способен обновлять секреты в общем volume без пересоздания Pod.

### Статические секреты

Для статических секретов KV v2 Agent периодически повторно получает данные и обновляет файл при изменении содержимого.

Интервал управляется параметром:

```yaml
vault.hashicorp.com/template-static-secret-render-interval: "30s"
```

По умолчанию используется интервал около пяти минут.

### Динамические секреты

При работе с динамическими credentials Vault Agent учитывает lease и его параметры.

В зависимости от типа секрета и конфигурации Agent может:

- Продлевать действующий lease.
- Повторно получать credentials, когда требуется обновление.
- Перезаписывать файл с актуальными данными.

### Обновление файла и приложение

**Обновление файла не означает автоматического обновления конфигурации приложения.**

Если приложение прочитало секрет только при старте, оно продолжит использовать старое значение, даже когда файл изменится.

Для корректной ротации приложение должно поддерживать один из механизмов:

- Повторное чтение файла.
- Отслеживание изменений конфигурации.
- Reload по сигналу или команде.
- Переподключение к внешнему сервису с новыми credentials.

Перед автоматической ротацией динамических credentials необходимо учитывать существующие подключения, срок действия старых credentials и особенности работы приложения.

---

## 9. Диагностика и эксплуатация

### Проверка компонентов

```bash
# Vault и Injector
kubectl get pods -n vault

# Состояние приложения
kubectl get pod myapp -n demo

# Контейнеры Pod
kubectl get pod myapp -n demo \
  -o jsonpath='{.spec.containers[*].name}{"\n"}'

# Init-контейнеры
kubectl get pod myapp -n demo \
  -o jsonpath='{.spec.initContainers[*].name}{"\n"}'
```

### Логи и состояние

```bash
# Логи Injector
kubectl logs -n vault deployment/vault-agent-injector

# Логи Agent
kubectl logs -n demo myapp -c vault-agent

# События Pod
kubectl describe pod myapp -n demo

# Конфигурация webhook
kubectl get mutatingwebhookconfigurations
```

### Проверка TokenReview RBAC

```bash
kubectl auth can-i create tokenreviews.authentication.k8s.io \
  --as=system:serviceaccount:vault:vault
```

### Production-рекомендации

- Использовать TLS для связи Vault Agent с Vault Server.
- Назначать отдельные ServiceAccounts приложениям.
- Ограничивать Kubernetes Auth Roles конкретными ServiceAccounts и namespaces.
- Использовать минимально необходимые Vault Policies.
- Защищать файлы с секретами правами доступа.
- Не раскрывать Vault Tokens и секреты в логах.
- Контролировать доступность Injector и срок действия его TLS-сертификатов.
- Учитывать поведение `failurePolicy` при сбоях webhook.
- Проектировать ротацию с учетом возможностей самого приложения.

---

## Саммари

**Kubernetes Auth и доставка секретов - два независимых механизма.**

```text
Kubernetes ServiceAccount JWT
            ↓
Vault Kubernetes Auth
            ↓
Vault Auth Role
            ↓
Vault Token + Policies
            ↓
Vault Secrets Engine
            ↓
Vault Agent
            ↓
Shared Volume
            ↓
Application
```

- **Kubernetes Auth** - позволяет Pod подтвердить свою личность перед Vault.
- **Vault Auth Role** - связывает идентичность Kubernetes с назначаемыми Policies.
- **Vault Policy** - разрешает конкретные операции с секретами.
- **Vault Agent** - автоматизирует аутентификацию, получение и обновление секретов.
- **Vault Agent Injector** - изменяет Pod Spec через Mutating Admission Webhook.
- **Vault Secrets Operator / CSI Provider** - альтернативные способы доставки секретов.

Ключевой принцип: **приложение использует собственную Kubernetes-идентичность для доступа к Vault, а доставка секретов может быть автоматизирована без хранения постоянного Vault-токена в конфигурации приложения.**
