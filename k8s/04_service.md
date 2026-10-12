# 4. Service: стабильный адрес перед меняющимися Pod

## Какую проблему решает Service

Pod могут удаляться и пересоздаваться с другими IP. Клиенту неудобно следить за каждым экземпляром. **Service даёт стабильную точку доступа к изменяющемуся набору backend.**

Backend здесь — получатель трафика за Service. Даже Pod с frontend-приложением является backend относительно своего Service.

## Механизм

1. Pod имеют метки, например `app=web`.
2. Service с selector `app=web` выбирает подходящие Pod в своём namespace.
3. Kubernetes отражает адреса backend и их готовность в EndpointSlice.
4. Сетевой механизм направляет соединения к подходящим backend. В обычной конфигурации используются готовые backend.

```text
Клиент → стабильное имя / IP Service → один из готовых backend Pod
```

Deployment поддерживает набор Pod. Service выбирает получателей трафика по меткам Pod в своём namespace.

## ClusterIP, NodePort и LoadBalancer

| Тип | Точки доступа |
|---|---|
| `ClusterIP` | Внутренний виртуальный IP и DNS-имя Service |
| `NodePort` | Сохраняет внутренний доступ и добавляет вход через IP узла и порт |
| `LoadBalancer` | Сохраняет внутренний доступ и предоставляет адрес балансировщика для входящих соединений |

ClusterIP — тип по умолчанию. Для обычного ClusterIP Service имя `web` внутри того же namespace разрешается кластерным DNS в IP Service.

NodePort предоставляет вход через `NODE_IP:nodePort`. Клиент должен иметь сетевой доступ к этому адресу. Доступ извне зависит от маршрутов и настроек сети. В Minikube на него также влияет драйвер.

## Порты: три разные роли

```yaml
ports:
  - port: 8080      # Порт Service: web:8080
    targetPort: 80  # Порт приложения: PodIP:80
    nodePort: 30080 # Порт на узле: NodeIP:30080
```

Внутренний путь: `web:8080 → PodIP:80`. Путь через NodePort: `NodeIP:30080 → backend PodIP:80` через сетевой механизм Service. В обоих случаях Service направляет соединение на `targetPort` выбранного Pod.

`targetPort` может быть именем, например `http`. Тогда в Pod должен быть объявлен соответствующий именованный порт:

```yaml
ports:
  - name: http
    containerPort: 80
```

`containerPort` описывает порт контейнера. Приложение само слушает этот порт, а Service обеспечивает доступ через свой адрес.

## Минимальный Service

```yaml
# web-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: study
spec:
  type: ClusterIP
  selector:
    app: web
  ports:
    - port: 8080
      targetPort: 80
```

Для варианта NodePort меняют `type` на `NodePort` и добавляют, например, `nodePort: 30080` в элемент `ports`. Стандартный диапазон NodePort — 30000–32767, но он может быть настроен иначе.

## LoadBalancer: доступ через балансировщик

**Service типа LoadBalancer запрашивает балансировщик, который принимает соединения и направляет их к backend Service.** В облаке его обычно предоставляет интеграция с провайдером. В собственном кластере нужна установленная реализация, например MetalLB.

У Service из примера для этого меняют `spec.type`:

```yaml
# web-service.yaml — изменение типа Service
spec:
  type: LoadBalancer # Предоставить адрес балансировщика для этого Service
```

Клиент обращается к адресу балансировщика на `port` Service — в примере `8080`. Трафик доходит до Pod на `targetPort: 80`. Реализация может направлять его через NodePort или прямо к Pod.

Адрес появляется в колонке `EXTERNAL-IP` команды `kubectl get svc web -n study`. Это может быть IP или DNS-имя. Значение `<pending>` означает, что адрес ещё не назначен. Если реализации LoadBalancer нет, сам по себе этот YAML адрес не создаст. Доступность адреса из интернета или только из частной сети зависит от настроек балансировщика.

**LoadBalancer также может предоставлять вход к прокси Gateway.** Тогда балансировщик доставляет трафик к прокси, а правила Gateway API выбирают Service по домену и пути. Этот механизм разбирается в [записи 7](07_gateway_api.md).

## Основные команды

```bash
kubectl get service web -n study
kubectl describe service web -n study
kubectl get pods -n study -l app=web --show-labels
kubectl get endpointslices -n study -l kubernetes.io/service-name=web -o yaml

# Проверить Service из временного клиентского Pod
kubectl run service-client -n study --image=busybox:1.36 \
  --restart=Never --rm -i -- wget -qO- http://web:8080
```

`--restart=Never` здесь создаёт обычный Pod. `--rm` удаляет временный Pod после завершения, `-i` подключает stdin. Команда wget выполняется внутри клиента в кластере.

Если запрос не проходит, проверь: существует ли Service → совпадают ли selector и метки Pod → готовы ли backend → верны ли порты → откуда выполняется запрос.

## Проверь себя

**1. Deployment заменил Pod, IP изменился. Нужно ли редактировать Service?**

<details>
<summary>Показать ответ</summary>

Нет, если метки нового Pod по-прежнему подходят. Kubernetes обновляет backend адреса.

</details>

**2. Service существует, но selector не совпадает ни с одним Pod. Что будет?**

<details>
<summary>Показать ответ</summary>

Адрес Service будет существовать, но направлять запросы будет некуда: список backend останется пустым.

</details>

**3. При `port: 8080, targetPort: 80` какой порт использовать в запросе к Service?**

<details>
<summary>Показать ответ</summary>

8080. Приложение за Service принимает соединение на 80.

</details>

**4. Почему запрос к ClusterIP с ноутбука не равнозначен запросу из Pod?**

<details>
<summary>Показать ответ</summary>

ClusterIP рассчитан на внутреннюю сеть кластера. У ноутбука может не быть маршрута к ней.

</details>

**5. Для Service задан `type: LoadBalancer`, но в кластере нет его реализации. Появится ли внешний адрес?**

<details>
<summary>Показать ответ</summary>

Нет. Нужна интеграция с облачным провайдером или другая реализация LoadBalancer.

</details>

**6. У LoadBalancer Service заданы `port: 8080` и `targetPort: 80`. На какой порт балансировщика обращается клиент?**

<details>
<summary>Показать ответ</summary>

На `8080` — порт Service.

</details>
