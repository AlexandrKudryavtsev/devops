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

Service не создаёт Pod и не управляет количеством реплик. Deployment и Service — отдельные объекты с разными задачами. Service выбирает Pod по меткам, а не по имени Deployment или ReplicaSet.

## ClusterIP и NodePort

| Тип | Точки доступа |
|---|---|
| `ClusterIP` | Внутренний виртуальный IP и DNS-имя Service |
| `NodePort` | Сохраняет внутренний доступ и добавляет вход через IP узла и порт |

ClusterIP — тип по умолчанию. Для обычного ClusterIP Service имя `web` внутри того же namespace разрешается кластерным DNS в IP Service.

NodePort предоставляет вход через `NODE_IP:nodePort`. Клиент должен иметь сетевой доступ к этому адресу. Наличие NodePort не означает автоматическую доступность из интернета; в Minikube доступ также зависит от драйвера и окружения.

## Порты: три разные роли

```yaml
ports:
  - port: 8080
    targetPort: 80
    nodePort: 30080
```

| Поле | Где клиент или backend использует этот порт |
|---|---|
| `port` | Порт Service: например, `web:8080` |
| `targetPort` | Порт приложения в backend Pod: например, `PodIP:80` |
| `nodePort` | Дополнительная точка входа на Node: например, `NodeIP:30080` |

Внутренний путь: `web:8080 → PodIP:80`. Путь через NodePort: `NodeIP:30080 → backend PodIP:80` через сетевой механизм Service. Это не обязательная цепочка отдельных TCP-соединений через все три порта.

`targetPort` может быть именем, например `http`. Тогда в Pod должен быть объявлен соответствующий именованный порт:

```yaml
ports:
  - name: http
    containerPort: 80
```

`containerPort` описывает порт, но не запускает слушающий процесс и не публикует его наружу. Приложение должно само слушать нужный порт.

## Минимальный Service

```yaml
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

Service не получит выбранных Pod для обслуживания трафика. Существование Service не гарантирует наличие backend.

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
