# 2. Развёртывание: от YAML до управляемых Pod

## Какую проблему решает Deployment

Одиночный Pod можно запустить напрямую, но после удаления у него нет контроллера, который создаст замену. Для приложения обычно нужен управляемый набор экземпляров и механизм обновления.

**Deployment описывает количество и шаблон Pod, управляя ими через ReplicaSet.**

```text
Deployment → управляет ReplicaSet → поддерживает Pod → содержит контейнеры
```

## Как читать манифест

| Поле | Что описывает |
|---|---|
| `apiVersion` | Версию API данного типа ресурса |
| `kind` | Тип ресурса |
| `metadata` | Имя, namespace, labels и другие метаданные объекта |
| `spec` | Желаемую конфигурацию |
| `status` | Наблюдаемое состояние; обычно его заполняет Kubernetes |

Пример Deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: study
  labels:
    app: web
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web
  template:
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

Это учебный пример образа из исходных записей, а не рекомендация версии для production.

## Три поля, на которых держится понимание

- `spec.replicas`: сколько Pod требуется.
- `spec.selector`: каким меткам должны соответствовать управляемые Pod.
- `spec.template`: шаблон новых Pod — их метки и контейнеры.

Метки шаблона должны удовлетворять selector. Например, selector `app=web` подходит к Pod с метками `app=web, environment=training`.

**`metadata.labels` Deployment и `spec.template.metadata.labels` — метки разных объектов.** Первые принадлежат Deployment, вторые попадут на создаваемые Pod. Они не копируются друг в друга автоматически.

Владелец Pod из этой цепочки — ReplicaSet, а владелец ReplicaSet — Deployment. Эти связи записаны в `ownerReferences`; метки сами по себе не означают владение.

## Что происходит после применения

1. `apply` передаёт конфигурацию в API.
2. Контроллер Deployment создаёт или согласует ReplicaSet.
3. ReplicaSet создаёт недостающие Pod.
4. Scheduler назначает узлы, kubelet обеспечивает запуск.
5. Kubernetes сообщает фактическое состояние через `status`.

Если Pod удалить, ReplicaSet создаст замену. Это не означает мгновенного восстановления готовности: новому Pod ещё нужно разместиться и запуститься.

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

`-f` задаёт файл. `--dry-run=client` не сохраняет ресурс, но не заменяет проверку реального запуска. `diff` показывает планируемые различия. `apply` не ждёт готовности приложения.

Повторное применение той же конфигурации к объекту с тем же именем и namespace не создаёт ещё один Deployment. Это идемпотентность.

## Не путай

| Изменение | Результат |
|---|---|
| `spec.replicas` | Изменение количества экземпляров |
| `spec.template`, например `image` | Обновление шаблона и rollout |
| `containerPort` | Описание порта контейнера; само по себе не публикует его |

`spec` — вся желаемая конфигурация. `spec.template` — только её часть. Поэтому фраза «любое изменение spec создаёт ревизию» неверна.

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
