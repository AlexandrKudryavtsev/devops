# 7. Gateway API: точка входа и маршруты к Service

## Какую проблему решает Gateway API

Если приложений несколько, нужна общая точка входа, которая направляет HTTP-запросы в разные Service по домену и пути. **Gateway API описывает точку входа и правила маршрутизации. Контроллер настраивает по ним прокси или облачный балансировщик.** Kubernetes рекомендует Gateway API для новых конфигураций входящего трафика. Прежний подход разобран в [краткой справке про Ingress](07_ingress.md).

Для работы нужны установленные определения типов ресурсов Gateway API — **CRD** — и совместимый контроллер. Установка контроллера и способ публикации точки входа зависят от выбранной реализации.

## Пример с двумя маршрутами

Допустим, в namespace `study` есть Service `web` и `reports`, каждый с портом `8080` и готовыми backend. `web` может использовать ClusterIP Service из [записи 4](04_service.md). Для домена `store.practice.test` направим `/reports` в `reports`, остальные пути — в `web`. Точка входа будет принимать HTTP на порту `80`.

В примере `training` — условное имя существующего GatewayClass. Доступные классы показывает `kubectl get gatewayclasses`.

```yaml
# web-gateway.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: web-entry
  namespace: study
spec:
  gatewayClassName: training # Заменить на класс установленного контроллера
  listeners:                 # На каких портах и по каким протоколам принимать трафик
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: Same         # Подключать маршруты из namespace этого Gateway
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: web-routing
  namespace: study
spec:
  parentRefs:                # К какой точке входа подключить маршрут
    - name: web-entry
      sectionName: http      # Имя listener в Gateway
  hostnames:
    - store.practice.test    # Домен из HTTP Host
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /reports  # /reports и его вложенные пути
      backendRefs:
        - name: reports      # Service в namespace HTTPRoute
          port: 8080         # port Service
    - matches:
        - path:
            type: PathPrefix
            value: /         # Остальные пути этого домена
      backendRefs:
        - name: web
          port: 8080
```

## GatewayClass, Gateway и HTTPRoute

- **GatewayClass** определяет, какой контроллер обслуживает Gateway этого класса. Этот объект существует на уровне кластера.
- **Gateway** описывает точку входа. Его `listeners` задают порты, протоколы и допустимые маршруты.
- **HTTPRoute** связывает домены и пути с backend Service. `parentRefs` подключает маршрут к Gateway, `backendRefs` выбирает получателя трафика.

Один Gateway может обслуживать несколько HTTPRoute. В примере Gateway, маршрут и оба Service находятся в `study`.

## Как выбирается маршрут

**Адрес определяет, куда подключиться, а HTTP Host и путь — какое правило выбрать.** DNS-запись домена должна вести на доступный адрес точки входа. Само поле `hostnames` DNS-запись не создаёт.

`PathPrefix` сравнивает путь по сегментам: `/reports` подходит для `/reports/2026`, а `/reports-old` попадёт под `/`. При совпадении нескольких префиксов выбирается самый длинный. Тип `Exact` задаёт точное совпадение пути и имеет приоритет над префиксом.

Backend получает исходный путь: `reports` получит `/reports/2026` целиком. Для изменения пути используют фильтр `URLRewrite`, если его поддерживает реализация. Пример настроен на HTTP. Для HTTPS у Gateway настраивают listener с протоколом HTTPS и TLS-сертификатом.

## Как связан LoadBalancer

Контроллер может создать для прокси Service типа `LoadBalancer` или настроить облачный балансировщик напрямую. Конкретный способ зависит от реализации Gateway API.

Общая точка входа обслуживает оба приложения. Backend Service `web` и `reports` могут оставаться `ClusterIP` — отдельный внешний балансировщик каждому из них не требуется.

## Основные команды

```bash
kubectl get gatewayclasses
kubectl get gateways,httproutes -n study
kubectl describe gateway web-entry -n study
kubectl describe httproute web-routing -n study
kubectl get gateway web-entry -n study -o yaml
```

В `status.addresses` Gateway ищут адрес точки входа. Его condition `Programmed=True` сообщает, что конфигурация передана обслуживающей инфраструктуре. В `status.parents` HTTPRoute смотрят `Accepted=True` — маршрут принят Gateway, и `ResolvedRefs=True` — ссылки на backend и другие объекты корректны.

Если внешний запрос не проходит, проверь адрес точки входа → состояние Gateway и HTTPRoute → совпадение домена и пути → порт Service и готовность его backend.

## Проверь себя

**1. Определения Gateway API установлены, но совместимого контроллера нет. Кто применит правила к прокси?**

<details>
<summary>Показать ответ</summary>

Применить их некому. Нужен контроллер, обслуживающий выбранный GatewayClass.

</details>

**2. Service имеет `port: 8080` и `targetPort: 80`. Какой порт указать в `backendRefs` HTTPRoute?**

<details>
<summary>Показать ответ</summary>

`8080` — порт Service.

</details>

**3. В какие Service попадут `/reports/2026` и `/reports-old` при правилах из примера?**

<details>
<summary>Показать ответ</summary>

`/reports/2026` — в `reports`, `/reports-old` — в `web`.

</details>

**4. Домен добавили в `hostnames`. Появится ли DNS-запись автоматически?**

<details>
<summary>Показать ответ</summary>

Нет. DNS-запись на адрес точки входа настраивают отдельно.

</details>

**5. Нужно ли менять backend Service на LoadBalancer, чтобы HTTPRoute направлял в него запросы?**

<details>
<summary>Показать ответ</summary>

Нет. Backend Service может оставаться ClusterIP.

</details>
