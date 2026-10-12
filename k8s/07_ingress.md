# Справка к записи 7. Ingress: API заморожен

**Ingress API больше не развивается. Для новых конфигураций Kubernetes рекомендует Gateway API.** Ingress остаётся в Kubernetes, а эта справка нужна для чтения и сопровождения существующих конфигураций.

## Как устроен Ingress

Ingress задаёт правила выбора Service по домену и пути HTTP-запроса. Ingress Controller реализует эти правила в прокси. Поле `ingressClassName` указывает на IngressClass, связанный с нужным контроллером.

Как и в [примере Gateway API](07_gateway_api.md), используем Service `web` и `reports` в namespace `study`, оба с портом `8080`. `training` — условное имя класса установленного Ingress Controller.

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

`Prefix` сравнивает путь по сегментам. `/reports/2026` попадёт в `reports`, `/reports-old` — в `web`. Backend получит исходный путь целиком. В `backend.service.port` указывают порт Service.

## Связь с Gateway API

В Gateway API точку входа описывает `Gateway`, HTTP-маршруты — `HTTPRoute`. В Ingress домены, пути и настройки TLS собраны в одном объекте. Для работы каждого подхода нужен поддерживающий его контроллер.

При переходе правила переносят в объекты Gateway API. Особые настройки из аннотаций Ingress разбирают отдельно: их перенос зависит от выбранных реализаций.
