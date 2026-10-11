# 2. Развёртывание: от YAML до управляемых Pod

## Какую проблему решает Deployment

Если запустить Pod напрямую и затем удалить его, для нового запуска придётся создать Pod заново. Для приложения обычно нужен управляемый набор экземпляров и механизм обновления.

**Deployment описывает количество и шаблон Pod, управляя ими через ReplicaSet.**

```text
Deployment → управляет ReplicaSet → поддерживает Pod → содержит контейнеры
```

## Как читать манифест

```yaml
# web.yaml
apiVersion: apps/v1  # Версия API для этого типа ресурса
kind: Deployment     # Тип ресурса
metadata:            # Данные самого Deployment
  name: web
  namespace: study
  labels:
    app: web
spec:                # Желаемая конфигурация
  replicas: 2        # Требуемое число Pod
  selector:          # Метки управляемых Pod
    matchLabels:
      app: web
  template:          # Шаблон новых Pod
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: nginx
          image: nginx:1.27-alpine
          ports:
            - name: http
              containerPort: 80
```

Kubernetes заполняет `status` — фактическое состояние объекта. В манифесте для создания его обычно опускают.

## Метки и владение

Метки шаблона должны удовлетворять selector. Например, selector `app=web` подходит к Pod с метками `app=web, environment=training`.

**`metadata.labels` Deployment и `spec.template.metadata.labels` — метки разных объектов.** Первые принадлежат Deployment, вторые попадут на создаваемые Pod. Они не копируются друг в друга автоматически.

Владелец Pod из этой цепочки — ReplicaSet, а владелец ReplicaSet — Deployment. Эти связи записаны в `ownerReferences`. Метки сами по себе не означают владение.

## Что происходит после применения

1. `apply` передаёт конфигурацию в API.
2. Контроллер Deployment создаёт или согласует ReplicaSet.
3. ReplicaSet создаёт недостающие Pod.
4. Scheduler назначает узлы, kubelet обеспечивает запуск.
5. Kubernetes сообщает фактическое состояние через `status`.

Если Pod удалить, ReplicaSet создаст замену. Новому Pod потребуется время на размещение и запуск.

## Основные команды

```bash
# Предварительно проверить конфигурацию без сохранения объекта
kubectl apply --dry-run=client -f web.yaml

# Увидеть различия с кластером
kubectl diff -f web.yaml

# Создать или обновить ресурс
kubectl apply -f web.yaml

# Дождаться развёртывания
kubectl rollout status deployment/web -n study --timeout=60s

# Посмотреть всю цепочку
kubectl get deployment,replicaset,pods -n study -l app=web
```

`-f` задаёт файл. `--dry-run=client` проверяет манифест локально. `diff` показывает различия с объектом в кластере. После `apply` готовность проверяют отдельно через `rollout status`.

**Идемпотентность** означает, что повторное применение той же конфигурации даёт тот же результат. При том же имени и namespace `apply` работает с существующим Deployment.

**Изменение `replicas` меняет количество Pod. Изменение `template` запускает rollout — обновление Pod по новому шаблону.** `containerPort` описывает порт контейнера. Для доступа через стабильный адрес нужен Service.

## Проверь себя

**1. Ты повторно применил тот же YAML. Появится второй Deployment?**

<details>
<summary>Показать ответ</summary>

Нет, при том же имени и namespace применяется конфигурация существующего объекта.

</details>

**2. Ты удалил Pod из Deployment. Кто создаёт замену?**

<details>
<summary>Показать ответ</summary>

Контроллер ReplicaSet, поддерживающий нужное число Pod.

</details>

**3. На Deployment стоит `app=web`, но в шаблоне Pod этой метки нет. Получат ли Pod её автоматически?**

<details>
<summary>Показать ответ</summary>

Нет. Метки Deployment и метки шаблона Pod задаются отдельно. Если шаблон не соответствует selector Deployment, такая конфигурация будет отвергнута.

</details>

**4. Почему после успешного `apply` нужно проверить rollout?**

<details>
<summary>Показать ответ</summary>

API может принять конфигурацию, но Pod могут не разместиться, не скачать образ или не стать готовыми.

</details>
