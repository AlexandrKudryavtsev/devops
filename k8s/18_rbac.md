# 18. RBAC: кто, что и в какой области может делать через Kubernetes API

## Какую проблему решаем

До этого мы описывали приложение, его ресурсы, размещение и доступ по сети. Но к Kubernetes API обращаются разные клиенты: разработчик смотрит Pod, CI/CD обновляет Deployment, а программа внутри Pod может читать объекты кластера. Каждому нужен свой набор разрешённых действий.

**RBAC — система авторизации запросов к Kubernetes API на основе ролей.** Она отвечает на вопрос: кто может выполнить конкретное действие над конкретным ресурсом и в какой области? Например, разработчику можно разрешить просмотр Pod в учебном namespace, а приложению — чтение нужных объектов без права менять их. Сетевой доступ из записи 16 и разрешение API-запроса проверяются отдельно: возможность подключиться к API Server ещё не означает право читать Secret.

## Механизм: личность, действие и разрешение

Сначала API Server определяет личность клиента — это **аутентификация**, вопрос «кто делает запрос?». Затем проверяет разрешение — это **авторизация**, вопрос «может ли этот клиент выполнить действие?». RBAC участвует во второй проверке: наличие действительного токена или сертификата само по себе не даёт права на все объекты.

```text
клиент → API Server → аутентификация → авторизация через RBAC
                                              ↓
                                  действие разрешено или отклонено
```

Для ресурсного запроса важны субъект и его группы, действие, API group, ресурс и namespace, а иногда имя объекта или подресурс. Например, чтение одного Pod `backend-abc` и получение списка Pod — разные действия: `get` и `list`. Если запрос изменяет объект, после разрешения остаются другие проверки, включая admission и проверку корректности объекта; RBAC-разрешение не гарантирует успешное создание.

В RBAC описание прав отделено от их выдачи. Role или ClusterRole содержит правила, а binding связывает этот набор с субъектами:

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

Сама роль никому ничего не выдаёт, а имя вроде `pod-reader` не имеет встроенного смысла: поведение определяют `rules`. Binding ссылается на роль по имени; он не копирует её правила, поэтому изменение роли меняет разрешения связанных с ней субъектов.

## Role: какие ресурсы и действия разрешены

В примере приложению `backend` нужно получать сведения о Pod в namespace `study`. Роль описывает только чтение этих объектов:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pod-reader
  namespace: study
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
```

`apiGroups: [""]` обозначает основную, или core, API group: сюда относятся Pod, Service, ConfigMap и Secret. Для Deployment нужна группа `apps`, для Job — `batch`; в правиле указывается группа без версии, например `apps`, а не `apps/v1`. Поле `resources` содержит имя ресурса API, обычно во множественном числе: `pods`, а не YAML-kind `Pod`.

Все части правила должны соответствовать запросу: разрешение `get pods` в core group не даёт `get deployments` в apps. `metadata.namespace` ограничивает область Role, а `verbs` определяет допустимые действия:

| Verb | Смысл |
|---|---|
| `get` | Получить один объект |
| `list` | Получить список объектов |
| `watch` | Наблюдать за изменениями объектов |
| `create` | Создать объект |
| `update` | Обновить объект целиком |
| `patch` | Частично изменить объект |
| `delete` | Удалить объект |

`kubectl get pod NAME` требует `get`, а `kubectl get pods` — `list`; наблюдение через `-w` использует `watch` и может начинаться с чтения текущего состояния. Роль из примера не разрешает создание, изменение и удаление Pod. При этом одной команде kubectl иногда нужны несколько API-действий: например, применение манифеста может включать чтение, создание или patch, поэтому права оценивают по запросам, а не по названию команды.

## ServiceAccount и RoleBinding: кому выдать права

Субъектом binding может быть `User`, `Group` или `ServiceAccount`. Пользователи и группы представлены именами, полученными при аутентификации; создание binding для `User alice` не создаёт пользователя и его учётные данные. ServiceAccount, напротив, является объектом Kubernetes и принадлежит namespace: `backend` в `study` и `backend` в `production` — разные личности. Следующие два объекта создают ServiceAccount и связывают его с предыдущей Role:

```yaml
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
subjects:
  - kind: ServiceAccount
    name: backend
    namespace: study
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: pod-reader
```

`subjects` отвечает на вопрос «кому?», а `roleRef` — «какой набор разрешений?». `roleRef.apiGroup` указывает группу объектов RBAC, а `rules[].apiGroups` внутри Role — группу ресурсов, к которым выдаётся доступ; это разные поля с разными задачами. В binding можно перечислить несколько субъектов, и каждый получит разрешения указанной роли.

**Namespace RoleBinding определяет область выдаваемых прав, а namespace ServiceAccount в subjects — личность получателя.** Они могут различаться: RoleBinding в `study` способен выдать права там ServiceAccount из другого namespace. Если `roleRef.kind: Role`, эта Role должна находиться в namespace самого binding; обратиться таким способом к Role из соседнего namespace нельзя.

## От имени кого обращается приложение в Pod

Чтобы Pod использовал ServiceAccount `backend`, у него задают `spec.serviceAccountName`. В Deployment поле находится в шаблоне; фрагмент для добавления к его описанию:

```yaml
spec:
  template:
    spec:
      serviceAccountName: backend
```

ServiceAccount должен существовать в namespace Pod. Создание binding для `backend` не переключает уже существующие Pod на него: binding выдаёт права личности, а `serviceAccountName` определяет, какую личность получает Pod. Если поле не задано, используется ServiceAccount `default` этого namespace, который сам по себе не получает права чтения Pod или Secret.

```text
Pod → serviceAccountName: backend → учётные данные backend
                                           ↓
                             запрос приложения к API Server
                                           ↓
                 ServiceAccount → binding → правила роли
```

В современных версиях Kubernetes Pod по умолчанию получает короткоживущий токен ServiceAccount через projected volume; kubelet обновляет его. Приложение или его Kubernetes-клиент использует этот токен для API-запросов. Когда такой доступ не нужен, `automountServiceAccountToken: false` в спецификации Pod отключает автоматическое подключение учётных данных, но не отменяет bindings самого ServiceAccount. Механизм описан в [документации ServiceAccount](https://kubernetes.io/docs/concepts/security/service-accounts/).

Получение Secret самим приложением через API требует соответствующего разрешения. Передача Secret в контейнер через `env` или volume из записи 8 проходит другим путём: данные подготавливает kubelet, и приложению для такого использования не требуется отдельное `get secrets` от имени его ServiceAccount. Поэтому доступ к API и уже переданные контейнеру данные нужно рассматривать отдельно.

## Role, ClusterRole и область binding

Role подходит для ресурсов одного namespace. ClusterRole не имеет namespace и может описывать разрешения для ресурсов, принадлежащих namespace, например Pod, или ресурсов уровня кластера, например Node. **ClusterRole определяет набор правил, а способ привязки определяет, где субъект сможет ими пользоваться.**

| Роль и binding | Область выданных разрешений |
|---|---|
| Role + RoleBinding | Namespace RoleBinding; Role находится там же |
| ClusterRole + RoleBinding | Ресурсы только в namespace RoleBinding |
| ClusterRole + ClusterRoleBinding | Ресурсы во всех namespace и кластерные ресурсы, указанные в правилах |

Например, ClusterRole `pod-reader` с правилами чтения Pod можно использовать повторно: RoleBinding в `study` выдаст чтение только в `study`, а ClusterRoleBinding — во всех namespace. ClusterRoleBinding ссылается только на ClusterRole; привязать через него обычную Role нельзя. Расширение области не добавляет новые действия: роль с `get/list/watch pods` по-прежнему не разрешает удалять Deployment.

Для чтения Node нужен ClusterRole, потому что Node не принадлежит namespace. В следующем примере ClusterRoleBinding выдаёт ServiceAccount `backend` дополнительные права чтения узлов:

```yaml
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

У этих двух объектов нет `metadata.namespace`; namespace в `subjects` всё ещё нужен для определения ServiceAccount. Если заменить ClusterRoleBinding на RoleBinding в `study`, доступ к Node не появится: кластерный ресурс нельзя поместить в область одного namespace. Четыре объекта и правила их сочетания описаны в [документации RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/).

## Как складываются права и где проходит граница правила

**Разрешения RBAC складываются; запрещающих правил в RBAC нет.** Учитываются bindings как самой личности, так и её групп. Если одна роль разрешает читать Pod, а другая — удалять их, субъект получает оба разрешения. Добавление более узкой роли не отменяет широкую, а удаление одного binding не уберёт доступ, если его продолжает давать другой.

Если ни одно подходящее правило не разрешает запрос, RBAC его не разрешает. Поэтому в примере с единственным RoleBinding для `pod-reader` ServiceAccount сможет читать Pod в `study`, но не удалять их или читать в другом namespace. Этот вывод предполагает отсутствие дополнительных разрешений для него и его групп.

Права на ресурс также не открывают автоматически его подресурсы. Например, `get pods` позволяет читать объект Pod, но для журналов нужен отдельный ресурс `pods/log` с действием `get`. Для добавления чтения журналов в `rules` роли нужен ещё один элемент:

```yaml
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get"]
```

Принцип **least privilege** означает выдавать конкретные ресурсы, действия и область, которые нужны клиенту. Приложению, читающему Pod, достаточно `get/list/watch pods` в нужном namespace; административная роль для этой задачи избыточна. `*` в resources или verbs расширяет разрешения и на соответствующие новые ресурсы или действия, поэтому явный список точнее выражает цель.

Изменение `rules` роли влияет на все её bindings, а изменение `subjects` binding — на список получателей. `roleRef` существующего binding неизменяем: для ссылки на другую роль нужен новый binding или пересоздание прежнего. Эти изменения меняют доступ к API, но сами по себе не запускают обновление приложения.

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

`backend-rbac.yaml` — файл с первыми тремя объектами из примера, `POD` — имя существующего Pod. Имя личности ServiceAccount при аутентификации имеет вид `system:serviceaccount:NAMESPACE:NAME`; у `backend` это `system:serviceaccount:study:backend`. Без `--as` проверяется текущий клиент kubectl, а не приложение, указанное в манифесте.

`--as` использует impersonation: вызывающий клиент должен иметь право выполнять такую проверку от чужого имени. Если доступ выдан через группы, при проверке важно учитывать и их, для чего существует `--as-group`; подмена одного имени не всегда воспроизводит весь набор групп реального клиента. Синтаксис и примеры — в [справочнике kubectl auth can-i](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_auth/kubectl_auth_can-i/).

## Если запрос отклонён

Сначала определи личность, которая реально выполняет запрос, затем действие, ресурс, API group и namespace. После этого проверь subjects, roleRef и правила всех применимых ролей. Наличие объекта Role в кластере или положительный `can-i` для администратора ничего не говорит о правах ServiceAccount приложения.

| Наблюдение | Что проверить |
|---|---|
| `Unauthorized`, обычно HTTP 401 | Учётные данные: токен, сертификат, срок действия и настройки клиента |
| `Forbidden` с сообщением о запрете действия над ресурсом | Реальную личность, группы, binding, область и соответствие rules запросу |
| Pod читается, но журналы недоступны | Разрешение на `pods/log`, а не только `pods` |
| Доступ работает в одном namespace | Область RoleBinding и namespace в самом запросе |
| `can-i` возвращает yes, но создание не проходит | Проверку объекта, admission, квоты и остальные запросы выполняемой команды |

`can-i` проверяет авторизацию заданного действия, а не сетевую доступность из Pod и не успешность всей операции. Ошибка самой impersonation тоже не означает, что проверяемому ServiceAccount запрещено читать Pod: сначала нужно, чтобы клиент мог выполнить проверку от его имени.

## Проверь себя

**1. Создана Role pod-reader, но ни одного binding на неё нет. Получит ли backend права чтения Pod только из-за наличия роли?**

<details>
<summary>Показать ответ</summary>

Нет. Role описывает разрешения, а binding связывает их с субъектом. Без такой связи роль не выдаёт backend никаких прав.

</details>

**2. RoleBinding выдаёт права ServiceAccount backend, но у Pod не задан serviceAccountName. От чьего имени он обычно обращается к API?**

<details>
<summary>Показать ответ</summary>

От имени ServiceAccount default своего namespace, если использует автоматически предоставленные учётные данные. Binding для backend не переключает личность Pod; нужный ServiceAccount задают в его спецификации.

</details>

**3. RoleBinding находится в study, а ServiceAccount в subjects — в tools. Где он получает права и где находится Role из roleRef?**

<details>
<summary>Показать ответ</summary>

Права выдаются в study, а получатель — ServiceAccount из tools. Если roleRef ссылается на Role, она тоже должна находиться в study. Namespace получателя и область разрешений — разные части модели.

</details>

**4. ClusterRole разрешает читать Pod. Чем отличаются RoleBinding в study и ClusterRoleBinding на эту роль?**

<details>
<summary>Показать ответ</summary>

RoleBinding выдаёт чтение Pod только в study, ClusterRoleBinding — во всех namespace. Сама ClusterRole не даёт права без binding, а расширение области не добавляет действий сверх rules.

</details>

**5. ClusterRole node-reader содержит get/list/watch nodes. Достаточно ли RoleBinding в study для доступа к Node?**

<details>
<summary>Показать ответ</summary>

Нет. Node — кластерный ресурс, а RoleBinding выдаёт права в пределах одного namespace. Для выдачи этого разрешения нужен ClusterRoleBinding.

</details>

**6. Субъект уже может удалять Pod через одну роль. Отменит ли это новая Role с единственным разрешением get pods? Позволяет ли get pods читать журналы?**

<details>
<summary>Показать ответ</summary>

Новая роль не отменит удаление: разрешения складываются. Чтение журналов требует отдельного get на pods/log; разрешение на сам объект Pod его не включает.

</details>

**7. В rules указаны apiGroups: [""] и resources: ["deployments"]. Правильно ли это для обычного Deployment apps/v1?**

<details>
<summary>Показать ответ</summary>

Нет. Deployment относится к API group apps, поэтому в apiGroups должно быть apps без версии. Пустая строка обозначает core group; совпадение только имени ресурса недостаточно.

</details>

**8. Администратор получил yes от kubectl auth can-i list pods. Доказывает ли это доступ приложения backend и успешность его API-запроса?**

<details>
<summary>Показать ответ</summary>

Нет. Без --as проверялась личность текущего клиента. Нужно проверить действие для реальной личности приложения в нужном namespace; отдельно остаются аутентификация, сетевой путь и другие проверки конкретного запроса.

</details>
