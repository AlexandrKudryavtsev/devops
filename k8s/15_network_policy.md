# 15. NetworkPolicy и Calico: какой трафик разрешён между Pod

## Какую проблему решаем

Service из записи 4 позволяет найти приложение и направить запрос к его Pod. Но наличие адреса не отвечает на другой вопрос: кто должен иметь возможность подключиться? Например, backend нужен frontend, а доступ остальных Pod к нему нужно ограничить.

**NetworkPolicy описывает разрешённый сетевой трафик для выбранных Pod.** В обычной модели Kubernetes Pod могут взаимодействовать, пока для соответствующего направления не задана изоляция. Политика позволяет выбрать получателей или отправителей по меткам и открыть только нужные связи и порты.

```text
frontend ── TCP/8080 ──→ backend  ✓
other    ── TCP/8080 ──→ backend  ✕
```

## NetworkPolicy и Calico: описание и исполнение

NetworkPolicy — объект Kubernetes API, а фильтрацию выполняет сетевой плагин с поддержкой этих политик. В исходном примере используется Calico: он получает правила из API и обеспечивает их действие в сети. Если такой поддержки нет, объект может успешно создаться, но трафик останется доступным.

```text
YAML → API Server → Calico применяет правила → соединение разрешено или заблокировано
```

Политики работают на уровне сетевых соединений: адресов, протоколов и портов. Они не определяют права пользователя приложения и не выбирают HTTP-путь запроса. Разделение между API и исполнителем описано в [документации Calico](https://docs.tigera.io/calico/latest/network-policy/get-started/kubernetes-policy/kubernetes-network-policy).

## Ingress и egress — направления относительно Pod

**Ingress — входящие соединения к выбранному Pod; egress — исходящие соединения от него.** Один запрос `frontend → backend` является egress для frontend и ingress для backend. Поэтому направление всегда нужно читать относительно Pod, выбранных самой политикой.

```text
frontend ──────────→ backend
  egress              ingress
```

В записи 7 объект Ingress задавал HTTP-маршруты к Service. Здесь слово `ingress` обозначает направление сетевого трафика в правилах NetworkPolicy. Разрешение входящего трафика к backend само по себе не ограничивает его исходящие соединения.

## Как читать политику

В примере политика относится к namespace `study`. Backend слушает TCP/8080, а Pod имеют метки `app=backend` и `app=frontend`.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: backend-policy
  namespace: study
spec:
  podSelector:
    matchLabels:
      app: backend
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: frontend
      ports:
        - protocol: TCP
          port: 8080
```

Читай описание через три вопроса: **какие Pod выбраны, какое направление ограничено и какие соединения разрешены**. В этом примере вход к backend разрешён от frontend из того же namespace на TCP/8080. Если других разрешающих политик нет, остальные Pod не смогут открыть это соединение.

| Поле | Что определяет |
|---|---|
| `metadata.namespace` | Namespace политики и выбираемых ею Pod |
| `spec.podSelector` | К каким Pod применяется политика: здесь `app=backend` |
| `policyTypes` | Какие направления изолируются: здесь только Ingress |
| `ingress[].from` | Откуда разрешены входящие соединения |
| `egress[].to` | Куда разрешены исходящие соединения |
| `ports` | Разрешённые протоколы и порты назначения |

Верхний `podSelector` выбирает Pod, **к которым применяется политика**, а selector внутри `from` выбирает разрешённых отправителей. Одинаковое название поля не означает одинаковую роль. Один `podSelector` внутри `from` или `to`, без `namespaceSelector`, выбирает соседей только в namespace самой политики.

**NetworkPolicy выбирает Pod, а не Service.** Service определяет backend для отправки трафика, политика — допустимость соединения с этими Pod. Если Service имеет `port: 80` и `targetPort: 8080`, клиент обращается на 80, а правило для backend описывает порт приложения 8080; знание имени Service не обходит фильтрацию.

В одном правиле источник и порт должны совпасть одновременно. Если `ports` не указан, правило разрешает соединения от выбранных источников без ограничения портами. Если `from` не указан, правило разрешает вход с любых источников на указанные порты; отсутствие обеих частей делает правило разрешением любого входа.

## Изоляция и объединение политик

Для каждого направления изоляция определяется отдельно. Если Pod не выбран ни одной политикой с типом Ingress, стандартные NetworkPolicy не ограничивают его вход; как только хотя бы одна такая политика его выбрала, разрешён только вход, открытый применимыми правилами. Для Egress действует тот же механизм независимо от Ingress.

**Разрешения всех применимых политик объединяются, порядок их создания не задаёт приоритет.** Если одна политика открыла backend для frontend, а другая — для monitoring, разрешены оба источника. Более узкая политика не отменяет разрешение более широкой: результат нужно оценивать по всему набору политик, выбирающих Pod.

Для соединения `frontend → backend` должны быть выполнены оба условия:

- egress frontend допускает соединение;
- ingress backend допускает соединение.

Неизолированное направление допускает его по умолчанию. Ответный трафик разрешённого соединения допускается автоматически: отдельное разрешение нового соединения `backend → frontend` ради HTTP-ответа не требуется.

## Default deny: сначала изолировать, затем открыть нужные связи

Общую изоляцию в обоих направлениях задаёт политика без разрешающих правил. `podSelector: {}` выбирает все Pod в `study`, включая появившиеся позже, но не Pod всего кластера.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-all
  namespace: study
spec:
  podSelector: {}
  policyTypes:
    - Ingress
    - Egress
```

Эта политика не создаёт разрешений, а остальные политики могут добавлять их. Поэтому существующий `backend-policy` продолжит открывать ingress backend от frontend, но исходящее соединение frontend теперь тоже нужно разрешить. Если указать только Ingress, исходящий трафик этим объектом не изолируется.

Пустой список `ingress: []` не разрешает входящие соединения, а `ingress: [{}]` содержит одно правило без ограничений и открывает весь вход. Это разные конфигурации; такой же принцип действует для `egress`. Default deny относится к поддерживаемому политиками трафику: поведение TCP-соединений между обычными Pod нельзя определять по результату `ping` или запроса с Node.

Следующая политика разрешает исходящее соединение frontend к backend. Верхний selector теперь выбирает отправителя, а `egress.to` — backend, к которому ему можно подключиться.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: frontend-egress
  namespace: study
spec:
  podSelector:
    matchLabels:
      app: frontend
  policyTypes:
    - Egress
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: backend
      ports:
        - protocol: TCP
          port: 8080
```

Для цепочки `frontend → backend:8080 → database:5432` каждую связь открывают по тому же принципу. После общего default deny доступ backend к базе потребует egress backend и ingress database на TCP/5432. Разрешение первого звена не открывает второе автоматически.

## DNS и выбор Pod в другом namespace

После запрета egress обращение по имени Service может перестать работать ещё до попытки подключения к приложению: клиенту нужно отправить DNS-запрос. Для обычного кластерного DNS разрешают исходящие UDP/53 и TCP/53 к DNS Pod. Ниже пример для DNS Pod с меткой `k8s-app=kube-dns` в namespace `kube-system`; перед применением проверь реальные метки через команды в следующем разделе.

Политика ниже добавляет DNS-доступ всем Pod namespace `study`. Остальные ограничения сохраняются.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-dns
  namespace: study
spec:
  podSelector: {}
  policyTypes:
    - Egress
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
```

**Два selector в одном элементе `to` означают «И»: нужные Pod в выбранном namespace.** `namespaceSelector` проверяет метки Namespace, `podSelector` — метки Pod внутри него. Если разделить их на два элемента списка, получится «ИЛИ»: все Pod выбранного namespace либо подходящие Pod namespace самой политики. Правила `from` читаются так же.

Если используется NodeLocal DNSCache или другая схема DNS, разрешение должно соответствовать фактическому адресу и способу доступа к resolver. Сначала посмотри `/etc/resolv.conf` клиентского Pod: правило для обычных DNS Pod может не описывать этот путь. Формат selectors и правил определён в [справочнике NetworkPolicy](https://kubernetes.io/docs/reference/kubernetes-api/networking/network-policy-v1/).

## Основные команды

```bash
# Посмотреть политики и прочитать конкретное правило
kubectl get networkpolicies -n study
kubectl describe networkpolicy backend-policy -n study

# Применить манифест политики
kubectl apply -f backend-policy.yaml

# Проверить метки, с которыми работают selectors
kubectl get pods -n study --show-labels -o wide
kubectl get namespaces --show-labels
kubectl get pods -n kube-system -l k8s-app=kube-dns --show-labels

# Проверить DNS-настройки и соединение из существующего Pod
kubectl exec POD -n study -- cat /etc/resolv.conf
kubectl exec POD -n study -- wget -T 3 -qO- http://BACKEND_IP:8080
```

`backend-policy.yaml` — файл с манифестом из примера, `POD` — имя существующего клиентского Pod, `BACKEND_IP` — адрес backend из `get pods -o wide`. `-T 3` ограничивает ожидание wget тремя секундами.

## Если результат не совпадает с ожиданием

Сначала проверь работу приложения и Service, затем выбранные Pod и весь набор применимых политик. NetworkPolicy не создаёт backend и не запускает слушающий процесс; недоступность сервера ещё не доказывает, что трафик заблокирован именно политикой.

| Наблюдение | Что проверить |
|---|---|
| Запрос не проходил ещё до политик | Работу backend, Service, EndpointSlice, порты и DNS |
| Запрещённый клиент продолжает подключаться | Поддержку NetworkPolicy плагином, метки выбранных Pod и разрешения других политик |
| Ingress разрешён, но соединение не проходит | Egress отправителя и правильный порт назначения |
| По IP Pod работает, по имени Service — нет | DNS-доступ, `/etc/resolv.conf` и правила egress к resolver |
| После замены Pod доступ изменился | Метки нового Pod и соответствие selectors |

`describe networkpolicy` помогает прочитать принятую конфигурацию, но не подтверждает фактическую фильтрацию пакетов. Политики применяются асинхронно; для проверки нужны новые соединения из Pod с разрешёнными и неразрешёнными метками. Правила изоляции и объединения разрешений описаны в [документации Kubernetes](https://kubernetes.io/docs/concepts/services-networking/network-policies/).

## Проверь себя

**1. NetworkPolicy успешно создана, но сетевой плагин не поддерживает её применение. Будут ли запрещённые соединения блокироваться?**

<details>
<summary>Показать ответ</summary>

Нет. API хранит описание, а фактическую фильтрацию должен выполнять совместимый сетевой плагин, например Calico. Успешный apply сам по себе не подтверждает исполнение правил.

</details>

**2. В нашей политике верхний selector выбирает backend, а selector внутри from — frontend. К кому применяется ограничение и подойдёт ли frontend из другого namespace?**

<details>
<summary>Показать ответ</summary>

Изолируется ingress backend. Отправитель frontend выбирается в namespace политики, потому что в from указан только podSelector. Для выбора отправителей другого namespace нужен namespaceSelector; правило открывает именно TCP/8080 backend, хотя клиент использует порт Service 80.

</details>

**3. После разрешающей политики создали deny-all. Отменятся ли прежние разрешения из-за порядка создания?**

<details>
<summary>Показать ответ</summary>

Нет. Разрешения применимых политик объединяются, а deny-all не добавляет разрешённых соединений. Но он может изолировать ещё не ограниченное направление, например egress frontend, и тогда этому направлению тоже нужны разрешения.

</details>

**4. Ingress backend допускает frontend, но egress frontend изолирован и не содержит нужного разрешения. Пройдёт ли запрос? Нужно ли отдельно открывать новое обратное соединение для HTTP-ответа?**

<details>
<summary>Показать ответ</summary>

Запрос не пройдёт: оба направления должны допускать соединение. После разрешения исходного соединения его ответный трафик допускается автоматически; отдельное разрешение нового backend → frontend для ответа не требуется.

</details>

**5. После default deny frontend обращается к backend по IP Pod, но не по имени Service. С какого слоя начать?**

<details>
<summary>Показать ответ</summary>

С DNS: проверить resolver в /etc/resolv.conf и разрешение egress к нему по UDP/53 и TCP/53. Доступ к backend не открывает доступ к DNS автоматически. Правило должно соответствовать схеме DNS конкретного кластера.

</details>

**6. В одном элементе to стоят namespaceSelector для kube-system и podSelector для kube-dns. Что изменится, если сделать их двумя элементами списка?**

<details>
<summary>Показать ответ</summary>

Один элемент требует совпадения обоих условий: DNS Pod внутри kube-system. Два элемента объединяют разрешения: все Pod kube-system либо Pod с нужной DNS-меткой в namespace самой политики. Порты правила по-прежнему ограничивают соединения, но набор допустимых получателей станет другим.

</details>
