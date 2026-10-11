# 18. RBAC: кто, что и в какой области может делать через Kubernetes API

## Какую проблему решаем

До этого мы описывали приложение, его ресурсы, размещение и доступ по сети. Но к Kubernetes API обращаются разные клиенты: разработчик смотрит Pod, CI/CD обновляет Deployment, а программа внутри Pod может читать объекты кластера. Каждому нужен свой набор разрешённых действий.

**RBAC (Role-Based Access Control) — система авторизации запросов к Kubernetes API на основе ролей.** Она отвечает на вопрос: кто может выполнить конкретное действие над конкретным ресурсом и в какой области? Например, разработчику можно разрешить просмотр Pod в учебном namespace, а приложению — чтение нужных объектов без права менять их. Сетевой доступ из [записи 16](16_network_policy.md) позволяет установить соединение. RBAC определяет допустимые действия через API.

## Механизм: личность, действие и разрешение

Сначала API Server определяет личность клиента — это **аутентификация**, вопрос «кто делает запрос?». Затем проверяет разрешение — это **авторизация**, вопрос «может ли этот клиент выполнить действие?». RBAC участвует во второй проверке.

```text
клиент → API Server → аутентификация → авторизация через RBAC
                                              ↓
                                  действие разрешено или отклонено
```

Для ресурсного запроса важны личность клиента и его группы, действие, API group, ресурс и namespace, а иногда имя объекта или подресурс. Например, чтение одного Pod `backend-abc` и получение списка Pod — разные действия: `get` и `list`.

Role или ClusterRole содержит правила. **Binding** — привязка роли к получателям прав. В примере получатель — **ServiceAccount**, учётная запись приложения в Kubernetes:

```text
ServiceAccount backend ← RoleBinding → Role pod-reader
        кто?              связать       что разрешено?
```

| Объект | Что описывает |
|---|---|
| `Role` | Набор разрешений для ресурсов конкретного namespace |
| `ClusterRole` | Набор разрешений, определённый на уровне кластера |
| `RoleBinding` | Кому выдать разрешения в одном namespace |
| `ClusterRoleBinding` | Кому выдать разрешения на уровне кластера |

Права определяют `rules` роли. Binding ссылается на роль по имени, поэтому изменение её правил меняет права всех связанных получателей.

## Role: какие ресурсы и действия разрешены

В примере приложению `backend` нужно получать сведения о Pod в namespace `study`. Роль описывает только чтение этих объектов:

```yaml
# backend-rbac.yaml — Role
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pod-reader
  namespace: study
rules:
  - apiGroups: [""]              # Core API group
    resources: ["pods"]          # Имя ресурса в API
    verbs: ["get", "list", "watch"] # Разрешённые действия
```

`apiGroups: [""]` обозначает основную, или core, API group: сюда относятся Pod, Service, ConfigMap и Secret. Для Deployment нужна группа `apps`, для Job — `batch`. В правиле группа указывается без версии: `apps`. В `resources` нужно имя ресурса API во множественном числе: `pods`.

Все части правила должны совпасть с запросом: группа, ресурс и действие. `metadata.namespace` ограничивает область Role. Основные действия:

| Verb | Смысл |
|---|---|
| `get` | Получить один объект |
| `list` | Получить список объектов |
| `watch` | Наблюдать за изменениями объектов |
| `create` | Создать объект |
| `update` | Обновить объект целиком |
| `patch` | Частично изменить объект |
| `delete` | Удалить объект |

`kubectl get pod NAME` требует `get`, а `kubectl get pods` — `list`. Наблюдение через `-w` использует `watch` и может начинаться с чтения текущего состояния. Одной команде kubectl могут понадобиться несколько действий. Например, `apply` может включать чтение, создание или patch. Проверяй права на API-запросы, которые выполняет команда.

## ServiceAccount и RoleBinding: кому выдать права

Субъектом binding может быть `User`, `Group` или `ServiceAccount`. Пользователи и группы представлены именами, полученными при аутентификации. Учётные данные пользователя создаются отдельно от binding. ServiceAccount, напротив, является объектом Kubernetes и принадлежит namespace: `backend` в `study` и `backend` в `production` — разные личности. Следующие два объекта создают ServiceAccount и связывают его с предыдущей Role:

```yaml
# backend-rbac.yaml — продолжение: ServiceAccount и RoleBinding
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: backend
  namespace: study
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: backend-can-read-pods
  namespace: study
subjects:                  # Получатели прав
  - kind: ServiceAccount
    name: backend
    namespace: study
roleRef:                   # Роль, которую выдаём
  apiGroup: rbac.authorization.k8s.io # Группа объектов RBAC
  kind: Role
  name: pod-reader
```

**`roleRef.apiGroup` — группа самой роли, `rules[].apiGroups` — группа ресурсов, к которым выдаётся доступ.** В binding можно перечислить несколько субъектов, и каждый получит разрешения указанной роли.

**Namespace RoleBinding определяет область выдаваемых прав, а namespace ServiceAccount в subjects — личность получателя.** Они могут различаться: RoleBinding в `study` способен выдать права там ServiceAccount из другого namespace. Если `roleRef.kind: Role`, эта Role должна находиться в namespace самого binding.

## От имени кого обращается приложение в Pod

Чтобы Pod использовал ServiceAccount `backend`, у него задают `spec.serviceAccountName`. В Deployment поле находится в шаблоне. Фрагмент для добавления к его описанию:

```yaml
spec:
  template:
    spec:
      serviceAccountName: backend # Личность для API-запросов приложения
```

ServiceAccount должен существовать в namespace Pod. `serviceAccountName` определяет личность Pod, binding — её права. Если поле не задано, используется ServiceAccount `default` этого namespace, который сам по себе не получает права чтения Pod или Secret.

```text
Pod → serviceAccountName: backend → учётные данные backend
                                           ↓
                             запрос приложения к API Server
                                           ↓
                 ServiceAccount → binding → правила роли
```

Kubelet по умолчанию подключает в Pod короткоживущий токен ServiceAccount и обновляет его. Приложение использует токен для API-запросов. `automountServiceAccountToken: false` отключает автоматическое подключение токена. Права ServiceAccount при этом сохраняются.

## Role, ClusterRole и область binding

Role подходит для ресурсов одного namespace. ClusterRole не имеет namespace и может описывать разрешения для ресурсов, принадлежащих namespace, например Pod, или ресурсов уровня кластера, например Node. **ClusterRole определяет набор правил, а способ привязки определяет, где субъект сможет ими пользоваться.**

| Роль и binding | Область выданных разрешений |
|---|---|
| Role + RoleBinding | Namespace RoleBinding. Role находится там же |
| ClusterRole + RoleBinding | Ресурсы только в namespace RoleBinding |
| ClusterRole + ClusterRoleBinding | Ресурсы во всех namespace и кластерные ресурсы, указанные в правилах |

Например, ClusterRole `pod-reader` с правилами чтения Pod можно использовать повторно: RoleBinding в `study` выдаст чтение только в `study`, а ClusterRoleBinding — во всех namespace. ClusterRoleBinding ссылается только на ClusterRole. Набор действий в обоих случаях определяется правилами роли.

Для чтения Node нужен ClusterRole, потому что Node не принадлежит namespace. В следующем примере ClusterRoleBinding выдаёт ServiceAccount `backend` дополнительные права чтения узлов:

```yaml
# backend-node-rbac.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: node-reader
rules:
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: backend-can-read-nodes
subjects:
  - kind: ServiceAccount
    name: backend
    namespace: study
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: node-reader
```

У этих двух объектов нет `metadata.namespace`. Namespace в `subjects` всё ещё нужен для определения ServiceAccount. Если заменить ClusterRoleBinding на RoleBinding в `study`, доступ к Node не появится: кластерный ресурс нельзя поместить в область одного namespace.

## Как складываются права и где проходит граница правила

**RBAC по умолчанию отклоняет запросы. Роли добавляют разрешения, которые складываются.** Учитываются bindings как самой личности, так и её групп. Если одна роль разрешает читать Pod, а другая — удалять их, субъект получает оба разрешения. Чтобы убрать доступ, нужно удалить его из всех применимых ролей и bindings.

Допустим, у ServiceAccount и его групп есть только RoleBinding из примера. Тогда ему доступно только чтение Pod в `study`. Для других действий или namespace понадобятся дополнительные разрешения.

**Подресурс** — отдельная часть API ресурса. Например, `get pods` читает описание Pod, а `get pods/log` — его журналы. Для чтения журналов добавляют отдельное правило:

```yaml
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get"]
```

**Least privilege — принцип минимальных прав:** выдавай только нужные действия над конкретными ресурсами в нужной области. `*` в resources или verbs распространяет разрешения и на соответствующие ресурсы или действия, которые появятся в будущем.

Изменение `rules` роли влияет на все её bindings, а изменение `subjects` binding — на список получателей. `roleRef` существующего binding неизменяем: для ссылки на другую роль нужен новый binding или пересоздание прежнего.

## Основные команды

```bash
# Текущая личность и её разрешения
kubectl auth whoami
kubectl auth can-i list pods -n study
kubectl auth can-i --list -n study

# Посмотреть роли, привязки и используемый ServiceAccount
kubectl get serviceaccounts,roles,rolebindings -n study
kubectl describe role pod-reader -n study
kubectl describe rolebinding backend-can-read-pods -n study
kubectl get clusterroles,clusterrolebindings
kubectl get pod POD -n study -o jsonpath='{.spec.serviceAccountName}'

# Применить ServiceAccount, Role и RoleBinding из основного примера
kubectl apply -f backend-rbac.yaml

# Проверить конкретное действие от имени backend
kubectl auth can-i list pods -n study --as=system:serviceaccount:study:backend
kubectl auth can-i delete pods -n study --as=system:serviceaccount:study:backend
```

`POD` — имя существующего Pod. Имя личности ServiceAccount при аутентификации имеет вид `system:serviceaccount:NAMESPACE:NAME`. У `backend` это `system:serviceaccount:study:backend`. Без `--as` проверяется текущий клиент kubectl, а не приложение, указанное в манифесте.

**Impersonation** — выполнение запроса от имени другой личности. Для `--as` вызывающему клиенту нужно право на impersonation. Чтобы учесть права групп проверяемого клиента, используют `--as-group`.

## Если запрос отклонён

Сначала определи личность, которая реально выполняет запрос, затем действие, ресурс, API group и namespace. После этого проверь subjects, roleRef и правила всех применимых ролей. Проверяй `can-i` от имени ServiceAccount приложения.

| Наблюдение | Что проверить |
|---|---|
| `Unauthorized`, обычно HTTP 401 | Учётные данные: токен, сертификат, срок действия и настройки клиента |
| `Forbidden` с сообщением о запрете действия над ресурсом | Реальную личность, группы, binding, область и соответствие rules запросу |
| Pod читается, но журналы недоступны | Разрешение на `pods/log`, а не только `pods` |
| Доступ работает в одном namespace | Область RoleBinding и namespace в самом запросе |
| `can-i` возвращает yes, но создание не проходит | Проверку объекта, admission, квоты и остальные запросы выполняемой команды |

`can-i` проверяет авторизацию одного действия. При фактическом создании или изменении объекта API Server также выполняет **admission** — дополнительные проверки и обработку запроса — и проверяет корректность объекта. Сетевой доступ из Pod проверяется отдельно.

Если ошибка относится к impersonation, сначала проверь право текущего клиента использовать `--as`.

## Особый случай: Secret из API и Secret в контейнере

Чтобы читать Secret через API, приложению нужно соответствующее RBAC-разрешение. Когда Secret передан через `env` или volume, данные подготавливает kubelet. Приложение читает уже полученные значения, и отдельное `get secrets` для этого ему не требуется.

## Проверь себя

**1. Создана Role pod-reader, но ни одного binding на неё нет. Получит ли backend права чтения Pod только из-за наличия роли?**

<details>
<summary>Показать ответ</summary>

Нет. Role описывает разрешения, а binding связывает их с субъектом. Без такой связи роль не выдаёт backend никаких прав.

</details>

**2. RoleBinding выдаёт права ServiceAccount backend, но у Pod не задан serviceAccountName. От чьего имени он обычно обращается к API?**

<details>
<summary>Показать ответ</summary>

От имени ServiceAccount default своего namespace, если использует автоматически предоставленные учётные данные. Binding для backend не переключает личность Pod. Нужный ServiceAccount задают в его спецификации.

</details>

**3. RoleBinding находится в study, а ServiceAccount в subjects — в tools. Где он получает права и где находится Role из roleRef?**

<details>
<summary>Показать ответ</summary>

Права выдаются в study, а получатель — ServiceAccount из tools. Если roleRef ссылается на Role, она тоже должна находиться в study. Namespace получателя и область разрешений — разные части модели.

</details>

**4. ClusterRole разрешает читать Pod. Чем отличаются RoleBinding в study и ClusterRoleBinding на эту роль?**

<details>
<summary>Показать ответ</summary>

RoleBinding выдаёт чтение Pod только в study, ClusterRoleBinding — во всех namespace.

</details>

**5. ClusterRole node-reader содержит get/list/watch nodes. Достаточно ли RoleBinding в study для доступа к Node?**

<details>
<summary>Показать ответ</summary>

Нет. Node — кластерный ресурс, а RoleBinding выдаёт права в пределах одного namespace. Для выдачи этого разрешения нужен ClusterRoleBinding.

</details>

**6. Субъект уже может удалять Pod. Что изменится, если выдать ему ещё одну роль только с get pods?**

<details>
<summary>Показать ответ</summary>

Добавится чтение Pod. Право удаления сохранится, потому что разрешения складываются.

</details>

**7. Выдано get pods. Какое разрешение нужно добавить для чтения журналов?**

<details>
<summary>Показать ответ</summary>

`get` на подресурс `pods/log`.

</details>

**8. В rules указаны apiGroups: [""] и resources: ["deployments"]. Правильно ли это для обычного Deployment apps/v1?**

<details>
<summary>Показать ответ</summary>

Нет. Deployment относится к API group apps, поэтому в apiGroups должно быть apps без версии. Пустая строка обозначает core group. Совпадение только имени ресурса недостаточно.

</details>

**9. Администратор получил yes от kubectl auth can-i list pods. Доказывает ли это доступ приложения backend и успешность его API-запроса?**

<details>
<summary>Показать ответ</summary>

Нет. Без --as проверялась личность текущего клиента. Нужно проверить действие для реальной личности приложения в нужном namespace. Отдельно остаются аутентификация, сетевой путь и другие проверки конкретного запроса.

</details>
