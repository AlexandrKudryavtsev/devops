# Динамические секреты

## 1. Что такое Dynamic Secrets

**Dynamic Secrets** - секреты, которые Vault генерирует по запросу клиента и выдает на ограниченное время.

В отличие от статических секретов, динамические credentials обычно не существуют заранее: Vault создает их во внешней системе непосредственно при запросе.

**Основные преимущества:**

- **Временный доступ** - credentials имеют ограниченный срок действия (TTL).
- **Изоляция** - разным приложениям или экземплярам могут выдаваться уникальные credentials.
- **Автоматический отзыв** - Vault управляет прекращением действия credentials.
- **Аудит** - уникальные учетные данные помогают связывать действия с конкретными клиентами.
- **Снижение рисков** - уменьшается необходимость использовать общие долгоживущие пароли.

Динамические секреты поддерживаются различными Secrets Engines. В качестве основного примера далее рассматривается **PostgreSQL через Database Secrets Engine**. Сам механизм Dynamic Secrets не привязан к PostgreSQL.

### Static vs Dynamic Secrets

| Static Secrets | Dynamic Secrets |
|---|---|
| Создаются заранее | Генерируются по запросу |
| Обычно возвращают одно сохраненное значение | Могут выдавать уникальные credentials |
| Хранятся, например, в KV v2 | Генерируются специализированными Secrets Engines |
| Требуют отдельного механизма ротации | Имеют управляемый жизненный цикл |
| Не имеют lease как KV-секреты | Сопровождаются lease |

---

## 2. Архитектура и принцип работы

Vault выступает посредником между клиентом и внешней системой, в которой необходимо предоставить временный доступ.

```text
Application
    │
    │ Запрос credentials
    ▼
Vault Server
    │
    ├── Authentication / Token
    ├── Policies
    │
    ▼
Secrets Engine
    │
    ├── Конфигурация интеграции
    ├── Правила генерации
    └── Управление Lease
    │
    │ API / SQL
    ▼
External System
    │
    │ Создание временных credentials
    ▼
Vault
    │
    │ Credentials + Lease
    ▼
Application
```

### Последовательность работы

1. Клиент аутентифицируется в Vault и получает Vault Token.
2. Запрашивает динамический секрет через соответствующий Secrets Engine.
3. Vault проверяет доступ по Policies.
4. Secrets Engine взаимодействует с внешней системой и создает credentials.
5. Vault возвращает credentials вместе с информацией о lease.
6. Vault управляет временем действия секрета и его отзывом.

**Важно:** Vault Token и Dynamic Secret - разные сущности с независимыми сроками действия. Vault Token разрешает обращаться к Vault API, а динамические credentials позволяют обращаться к внешнему сервису.

---

## 3. Secrets Engines для динамических секретов

**Secrets Engine** - подключаемый модуль Vault, отвечающий за хранение, генерацию или обработку определенного типа секретов.

Динамические секреты поддерживаются несколькими реализациями Secrets Engines:

- **Database** - временные учетные данные PostgreSQL, MySQL, MongoDB и других поддерживаемых БД.
- **AWS** - динамические credentials для доступа к ресурсам AWS.
- **PKI** - выпуск сертификатов с ограниченным сроком действия.
- **SSH** - выдача подписанных SSH-сертификатов для временного доступа.
- **Другие специализированные движки** - управление credentials внешних систем через соответствующие плагины.

У каждого движка собственный API и конфигурация. Поэтому Connection и Database Role, рассматриваемые далее, - это не универсальные сущности всех Secrets Engines, а особенности Database Secrets Engine.

---

## 4. Database Secrets Engine

**Database Secrets Engine** - реализация Secrets Engine, позволяющая Vault управлять учетными данными баз данных.

Поддерживаются два основных механизма:

- **Dynamic Roles** - Vault создает новых временных пользователей БД по запросу.
- **Static Roles** - Vault управляет паролем заранее существующего пользователя и может автоматически его ротировать.

Для генерации динамических credentials используются три основные сущности:

| Сущность | Назначение |
|---|---|
| **Database Secrets Engine** | Реализует управление credentials БД |
| **Database Connection** | Описывает способ подключения Vault к конкретной БД |
| **Database Role** | Определяет, как создавать пользователей, какие права выдавать и какой TTL использовать |

### Database Connection

**Database Connection** - именованная конфигурация подключения Database Secrets Engine к внешней базе данных. Она содержит параметры соединения, служебные credentials и разрешенные Database Roles.

Например:

```text
Database Secrets Engine (database/)
└── Connection: postgres-app
    ├── Host: postgres.vault-lab.svc
    ├── Port: 5432
    ├── Username: vault_admin
    └── Allowed Roles:
        └── myapp-db-role
```

**Служебный пользователь** (`vault_admin`) - пользователь самой PostgreSQL, через которого Vault управляет временными ролями. Он должен иметь необходимые полномочия для их создания и отзыва.

Vault Root Token не предоставляет административных прав в PostgreSQL: у этих систем независимые механизмы авторизации.

### Database Role

**Database Role** - конфигурация генерации credentials внутри Database Secrets Engine.

Она задает:

- Какой Database Connection использовать.
- Какие SQL-команды выполнять при создании пользователя.
- Какие права назначать новому пользователю.
- Как отзывать credentials.
- Начальный и максимальный TTL.

Важно различать:

- **Kubernetes Auth Role** - определяет правила аутентификации приложения в Vault.
- **Database Role** - определяет правила генерации credentials в БД.
- **PostgreSQL Role** - пользователь или роль непосредственно внутри PostgreSQL.

Эти сущности относятся к разным уровням и не являются взаимозаменяемыми.

---

## 5. Настройка динамических credentials PostgreSQL

### Подключение Secrets Engine

```bash
vault secrets enable database
```

Движок подключается по API-пути `database/`.

### Настройка Database Connection

```bash
vault write database/config/postgres-app \
  plugin_name=postgresql-database-plugin \
  allowed_roles=myapp-db-role \
  connection_url='postgresql://{{username}}:{{password}}@postgres.vault-lab.svc:5432/postgres?sslmode=verify-full' \
  username=vault_admin \
  password="$DB_ADMIN_PASSWORD"
```

В конфигурации:

- `plugin_name` - плагин для взаимодействия с PostgreSQL.
- `allowed_roles` - список Database Roles, которым разрешено использовать подключение.
- `connection_url` - строка подключения к БД.
- `username` / `password` - служебные credentials для управления пользователями.

`{{username}}` и `{{password}}` в строке подключения - placeholders, которые Vault подставляет из конфигурации Connection.

Для `sslmode=verify-full` необходимо корректно настроить доверие к CA PostgreSQL и имя сервера в сертификате. В учебных средах TLS иногда отключают, но для production требуется защищенное соединение.

**`allowed_roles` не выдает клиентам права доступа к Vault API.** Он ограничивает, какие Database Roles могут использовать данное подключение. Клиентский доступ отдельно регулируется Vault Policies.

### Настройка Database Role

```bash
vault write database/roles/myapp-db-role \
  db_name=postgres-app \
  creation_statements="CREATE ROLE \"{{name}}\" WITH LOGIN PASSWORD '{{password}}' VALID UNTIL '{{expiration}}';" \
  revocation_statements="ALTER ROLE \"{{name}}\" NOLOGIN; DROP ROLE \"{{name}}\";" \
  default_ttl=5m \
  max_ttl=1h
```

**Параметры:**

- `db_name` - имя Database Connection.
- `creation_statements` - SQL-команды создания credentials.
- `revocation_statements` - SQL-команды отзыва credentials.
- `default_ttl` - начальный срок аренды.
- `max_ttl` - максимальный TTL lease при продлении.

В SQL-шаблонах:

- `{{name}}` - сгенерированное имя пользователя.
- `{{password}}` - сгенерированный пароль.
- `{{expiration}}` - время истечения credentials.

В примере создается роль с правом входа, но без дополнительных привилегий на таблицы. Для рабочего приложения права назначаются отдельно через соответствующие SQL-команды.

Для PostgreSQL также необходимо учитывать права служебной роли, зависимости удаляемых объектов и активные подключения.

---

## 6. Получение динамических секретов

После настройки Database Role клиент запрашивает credentials:

```bash
vault read database/creds/myapp-db-role
```

Пример ответа:

```text
lease_id          database/creds/myapp-db-role/...
lease_duration    5m
lease_renewable   true
username          v-myapp-xxxxx
password          <generated-password>
```

Каждый новый запрос к динамической Database Role обычно создает отдельную пару credentials.

При этом Vault:

1. Подключается к PostgreSQL через настроенный Connection.
2. Выполняет SQL создания пользователя.
3. Выдает credentials клиенту.
4. Создает lease для управления их жизненным циклом.

Для запроса клиенту требуется Vault Policy:

```hcl
path "database/creds/myapp-db-role" {
  capabilities = ["read"]
}
```

**Важно:** право `read` на этом API-пути фактически разрешает генерацию новых credentials. Это не простое чтение заранее сохраненного пароля.

---

## 7. TTL, Lease, Renewal и Revocation

**Lease** - механизм Vault для управления жизненным циклом выданного секрета. Каждый динамический секрет связан с lease, содержащим информацию о сроке действия и возможности продления.

### Основные понятия

| Термин | Значение |
|---|---|
| **TTL** | Оставшееся время действия lease |
| **Lease ID** | Идентификатор аренды для управления секретом |
| **Renewal** | Продление срока действия |
| **Revocation** | Отзыв выданного секрета |
| **Default TTL** | Начальный срок действия |
| **Max TTL** | Максимальный срок, ограничивающий продление |

### Renewal

Если lease допускает продление, клиент может запросить увеличение срока действия:

```bash
vault lease renew <lease_id>
```

Продление не обязательно создает новые credentials. Например, Database Secrets Engine может продлить срок действия существующего пользователя PostgreSQL.

Успешность продления зависит от настроек Secrets Engine, ограничений TTL и поддержки операции конкретным плагином.

### Revocation

Vault может отозвать секрет при истечении lease или по административному запросу.

Ручной отзыв:

```bash
vault lease revoke <lease_id>
```

Для PostgreSQL отзыв может включать выполнение SQL-команд, блокирующих или удаляющих временную роль.

```text
Lease Expired / Revoked
          │
          ▼
Vault Database Engine
          │
          │ Revocation Statements
          ▼
PostgreSQL
          │
          ▼
Временная роль теряет доступ
```

**Важный нюанс:** отзыв lease не означает мгновенного исчезновения уже открытых сетевых соединений во всех системах. Фактическое прекращение доступа определяется поведением внешнего сервиса и механизмом отзыва.

Кроме того, если Vault не может связаться с внешней системой, выполнение revocation может завершиться ошибкой и потребовать повторной попытки или вмешательства администратора.

---

## 8. Интеграция с Kubernetes

Kubernetes-приложения могут получать динамические секреты через Vault Agent, Secrets Operator, CSI Provider или непосредственные запросы к Vault API.

При использовании **Vault Agent Injector** доставка выполняется через общий volume Pod.

Пример аннотаций:

```yaml
vault.hashicorp.com/agent-inject: "true"
vault.hashicorp.com/role: "myapp-role"

vault.hashicorp.com/agent-inject-secret-db: "database/creds/myapp-db-role"
vault.hashicorp.com/agent-inject-template-db: |
  {{- with secret "database/creds/myapp-db-role" -}}
  DB_USER={{ .Data.username }}
  DB_PASSWORD={{ .Data.password }}
  {{- end }}
```

После получения credentials Vault Agent формирует файл:

```text
/vault/secrets/db
```

Пример содержимого:

```text
DB_USER=v-myapp-xxxxx
DB_PASSWORD=...
```

Vault Agent управляет аутентификацией в Vault, получением секрета и обновлением шаблона с учетом жизненного цикла lease.

### Особенность динамической ротации

При обновлении credentials приложение должно уметь переключаться на новые учетные данные.

**Обновление файла не означает автоматического переподключения к PostgreSQL.**

Приложению может потребоваться:

- Перечитать файл с credentials.
- Создать новые подключения к базе.
- Закрыть старые подключения.
- Корректно обработать истечение или отзыв credentials.

Иначе автоматическое обновление секретов может привести к ошибкам подключения.

---

## 9. Основные команды CLI

### Database Secrets Engine

```bash
# Включить движок
vault secrets enable database

# Посмотреть подключение
vault read database/config/postgres-app

# Посмотреть Database Role
vault read database/roles/myapp-db-role

# Сгенерировать credentials
vault read database/creds/myapp-db-role
```

### Управление Lease

```bash
# Информация о lease
vault lease lookup <lease_id>

# Продление lease
vault lease renew <lease_id>

# Отзыв lease
vault lease revoke <lease_id>
```

### Авторизация

```bash
# Создать Policy
vault policy write myapp-db-policy policy.hcl

# Просмотреть Policy
vault policy read myapp-db-policy
```

При диагностике важно различать ошибки:

- `permission denied` от Vault - проблема Vault Token или Policy.
- Ошибки SQL permissions - недостаточные полномочия служебного пользователя во внешней БД.
- Ошибки подключения - проблемы сети, DNS, TLS или credentials Database Connection.
- Ошибки revocation - проблемы выполнения процедуры отзыва во внешней системе.

---

## 10. Production-особенности

### Минимальные привилегии

Служебный пользователь Vault во внешней системе должен иметь только необходимые права для создания, изменения и отзыва credentials.

В PostgreSQL `CREATEROLE` не равнозначен `SUPERUSER`, но также требует осторожного использования.

Временным пользователям необходимо выдавать минимальные SQL-привилегии для конкретного приложения.

### Отказоустойчивость

Vault становится зависимостью для выдачи новых credentials.

При недоступности Vault уже выданные credentials могут продолжать работать до истечения срока действия или их отзыва во внешней системе, но создание новых credentials и продление leases может оказаться недоступным.

### Ротация и приложения

Нужно согласовывать:

- TTL динамических credentials.
- Периодичность обновления Vault Agent.
- Время жизни соединений приложения.
- Работу connection pools.
- Поведение приложения при смене credentials.

### Аудит и мониторинг

Рекомендуется отслеживать:

- Ошибки генерации и отзыва секретов.
- Неуспешные подключения к внешним системам.
- Истечение и продление leases.
- Ошибки Vault Agent.
- Количество временных пользователей и их жизненный цикл.

Важно исключать секретные значения из логов и систем мониторинга.

---

## Саммари

**Dynamic Secrets - механизм Vault, позволяющий генерировать временные credentials и управлять их жизненным циклом во внешних системах.**

Обобщенная схема:

```text
Client
  ↓
Vault Token + Policy
  ↓
Secrets Engine
  ↓
Генерация секрета во внешней системе
  ↓
Credentials + Lease
  ↓
Renewal / Revocation
```

В реализации Database Secrets Engine:

```text
Database Role
      ↓
Database Connection
      ↓
External Database
      ↓
Temporary User + Password
      ↓
Lease
```

- **Secrets Engine** - реализует механизм генерации и управления секретами.
- **Database Connection** - определяет, как подключиться к БД.
- **Database Role** - определяет параметры генерации credentials.
- **Vault Policy** - определяет, каким клиентам разрешено запрашивать credentials.
- **Lease** - управляет сроком действия выданного секрета.
- **Revocation** - отзывает доступ во внешней системе.

**Ключевое преимущество Dynamic Secrets - переход от общих долгоживущих паролей к временным, индивидуально выдаваемым credentials с централизованным управлением доступом.**