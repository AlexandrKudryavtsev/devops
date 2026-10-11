# 7. Ingress: выбрать Service по HTTP-запросу

## Какую проблему решает Ingress

Service даёт стабильный доступ к Pod одного приложения. Если приложений несколько, нужна общая точка входа, которая отправляет запросы в разные Service по домену и пути.

**Ingress описывает правила HTTP/HTTPS-маршрутизации. Ingress Controller реализует их в работающем прокси.**

## Механизм

```text
Ingress → правила → Ingress Controller
                           ↑
клиент → точка входа → прокси выбирает backend по host и path
                           ├→ Service web → готовые Pod web
                           └→ Service reports → готовые Pod reports
```

Это схема ответственности. Backend в правиле — Service, но контроллер может передавать запросы прямо на его Pod по адресам endpoints, без прохождения через ClusterIP.

| Объект | Ответственность |
|---|---|
| Deployment | Поддерживать и обновлять Pod |
| Service | Выбирать backend Pod и давать стабильную точку доступа |
| Ingress | Хранить правила выбора Service по HTTP-запросу |
| Ingress Controller | Принимать трафик и реализовывать правила |
| IngressClass | Описывать класс контроллера, на который ссылается Ingress |

Для работы Ingress нужны установленный контроллер и сетевой доступ к его прокси. DNS-запись, ведущую на точку входа, настраивают отдельно.

## Пример с двумя маршрутами

Посмотри доступные классы через `kubectl get ingressclasses` и замени `training` на имя класса своего кластера.

```yaml
# web-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: web-routing
  namespace: study
spec:
  ingressClassName: training # Класс контроллера
  rules:
    - host: store.practice.test # Домен из HTTP Host
      http:
        paths:
          - path: /reports     # Путь запроса
            pathType: Prefix   # Совпадение по сегментам пути
            backend:
              service:
                name: reports # Service в namespace study
                port:
                  number: 8080 # Порт Service, то есть его port
          - path: /
            pathType: Prefix
            backend:
              service:
                name: web
                port:
                  number: 8080
```

Нужны Service `web` и `reports` в namespace `study`, каждый с портом `8080` и рабочими backend. `web` может использовать Service из записи 4. Пример описывает HTTP-маршруты. HTTPS дополнительно требует настройки TLS.

## Три различия, которые нужно запомнить

**1. IP и Host решают разные задачи.** IP определяет, куда установить соединение. HTTP Host помогает прокси выбрать правило. Два доменных имени могут вести на один IP.

**2. Prefix — совпадение по сегментам пути.** `/reports` подходит для `/reports` и `/reports/2026`, но не для `/reports-old`. `/` подходит для всех путей. Из совпавших выбирают самый длинный путь. При равной длине `Exact` имеет приоритет над `Prefix`.

**3. Выбор Service сохраняет путь запроса.** `reports` получит `/reports/2026` целиком. Если приложению нужен путь `/2026`, потребуется **rewrite** — изменение пути средствами контроллера. Настройки rewrite зависят от выбранного контроллера.

## Основные команды

```bash
# Найти класс контроллера
kubectl get ingressclasses

# Проверить правила и нижний уровень
kubectl get ingress -n study
kubectl describe ingress web-routing -n study
kubectl get services -n study
kubectl get endpointslices -n study -l kubernetes.io/service-name=web -o yaml
kubectl get endpointslices -n study -l kubernetes.io/service-name=reports -o yaml

# Проверить HTTP-маршруты без локальной DNS-записи
curl -H 'Host: store.practice.test' http://ENTRY_IP:ENTRY_PORT/
curl -H 'Host: store.practice.test' http://ENTRY_IP:ENTRY_PORT/reports
```

`ENTRY_IP:ENTRY_PORT` — доступный адрес и HTTP-порт **прокси контроллера**, например его NodePort, а не NodePort приложения. Адрес зависит от того, как опубликован контроллер.

Если запрос не проходит, проверь: доступен ли прокси → обрабатывает ли контроллер этот класс → совпадают ли Host и path → существует ли нужный Service и порт → есть ли готовые backend → понимает ли приложение полученный путь.

`describe` показывает конфигурацию и события. Проверка через `curl` покажет, проходит ли запрос по всему маршруту.

## Проверь себя

**1. Ingress создан, но совместимого контроллера нет. Что будет принимать запросы?**

<details>
<summary>Показать ответ</summary>

Сам Ingress ничего не принимает. Нужен контроллер с работающим и доступным прокси.

</details>

**2. Service имеет `port: 8080` и `targetPort: 80`. Какой номер указать в backend Ingress?**

<details>
<summary>Показать ответ</summary>

Указать **8080** в `backend.service.port.number`, потому что backend Ingress ссылается на порт Service (`port`). Service затем направляет запрос на порт приложения 80 (`targetPort`).

</details>

**3. При правилах из примера куда попадут `/reports/2026` и `/reports-old`?**

<details>
<summary>Показать ответ</summary>

Первый путь — в `reports`, второй — в `web` по правилу `/`. Prefix сравнивает сегменты, а не произвольное начало строки.

</details>

**4. Почему запрос на правильный IP с другим Host может не попасть в нужное приложение?**

<details>
<summary>Показать ответ</summary>

Соединение дойдёт до прокси, но правило для `store.practice.test` не совпадёт. Дальнейшее поведение зависит от остальных правил и backend по умолчанию.

</details>

**5. Ingress выбрал `reports`, но приложение обслуживает только `/`. Исправит ли выбор Service путь `/reports`?**

<details>
<summary>Показать ответ</summary>

Нет. Нужна поддержка пути приложением или отдельная настройка rewrite у контроллера.

</details>
