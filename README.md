# clock — кастомный циферблат для Divoom Times Frame

Проект для разработки собственного **циферблата (watchface)** на устройство
**Divoom Times Frame**, который показывает:

- 💹 **финансовую информацию из Freedom24** (Tradernet API, read-only);
- 🕐 **часы** (время, дата, день недели);
- 🌤️ **погоду** (текущая + прогноз).

Управление устройством идёт через MCP-сервер
[`mcp-divoom-lan`](https://github.com/DivoomDevelop/mcp-divoom-lan), который оборачивает
LAN-API Divoom в инструменты для Claude Code.

> Референс желаемого вида — встроенная тема **«HUD Finance»**, которая уже стоит на
> устройстве. Её финансовые виджеты тянут данные из облака Divoom; наша цель — показывать
> **свои** данные из Freedom24.

## Оборудование

| Параметр          | Значение                                   |
|-------------------|--------------------------------------------|
| Модель            | Divoom Times Frame (`DeviceType: "Frame"`) |
| IP в локальной сети | `192.168.178.40`                         |
| Порт LAN API      | `9000` (HTTP)                              |
| DeviceId          | `300378939`                                |
| Разрешение холста | 800 × 1280 (портрет)                       |
| Роутер (шлюз)     | AVM FRITZ!Box 7690 @ `192.168.178.1` (WireGuard) |

## Структура репозитория

```
.
├── .mcp.json                 # регистрация MCP-сервера divoom-lan (project scope)
├── CLAUDE.md                 # инструкции и техсправка для сессий Claude Code
├── README.md                 # этот файл
├── reference/                # снимки реального состояния устройства
│   ├── device-clock-218-hud-finance.json   # текущая тема (референс вёрстки)
│   └── device-fonts.json                    # 161 шрифт, установленный на устройстве
└── tools/
    └── mcp-divoom-lan/       # склонированный и собранный MCP-сервер (Node.js/TS)
```

## Установка и настройка

MCP-сервер уже склонирован, собран и зарегистрирован. Если нужно повторить с нуля:

```bash
# 1. Клонировать и собрать MCP-сервер
git clone https://github.com/DivoomDevelop/mcp-divoom-lan.git tools/mcp-divoom-lan
cd tools/mcp-divoom-lan && npm install && npm run build

# 2. Зарегистрировать MCP-сервер в проекте (создаёт .mcp.json)
cd /data/web/clock
claude mcp add divoom-lan --scope project \
  -e DIVOOM_DEVICE_HOST=192.168.178.40 \
  -e DIVOOM_DEVICE_PORT=9000 \
  -e DIVOOM_TIMEOUT_MS=45000 \
  -- node /data/web/clock/tools/mcp-divoom-lan/dist/index.js
```

Инструменты MCP появляются в **новой** сессии Claude Code (сервер стартует при запуске
сессии).

## Проверка связи

Read-only команды, безопасны:

```bash
# Яркость + тип устройства
curl -s -X POST http://192.168.178.40:9000/divoom_api \
  -H 'Content-Type: application/json; charset=UTF-8' \
  -d '{"Command":"Sys/GetBrightness","ReturnCode":0}'

# Текущий циферблат целиком
curl -s -X POST http://192.168.178.40:9000/divoom_api \
  -H 'Content-Type: application/json; charset=UTF-8' \
  -d '{"Command":"Device/GetLocalClockInfo","ReturnCode":0,"UseCurrentDisplayClock":1}'
```

✅ Связь проверена: устройство отвечает (`ReturnCode: 0`, `DeviceType: "Frame"`),
MCP-сервер протестирован end-to-end по stdio (18 инструментов, 9 ресурсов, живой вызов
вернул HTTP 200).

## Возможности устройства (через MCP)

**Чтение:** инфо о циферблате, список шрифтов, яркость, маркет.
**Управление:** переключение циферблата, яркость, вкл/выкл экрана.
**Запись:** патч полей циферблата, замена фона, создание локального циферблата, загрузка
файлов, сброс.
**Каталоги-помощники:** `disp` (194 типа элементов), шрифты, готовые шаблоны, подсказки по
вёрстке.

Подробности по протоколу, инструментам и `disp` id — в [`CLAUDE.md`](./CLAUDE.md).

Циферблат собирается из элементов `ItemList`, у каждого свой тип `disp` (что рисуем:
часы, дата, погода, картинка), геометрия (`x/y/w/h`, холст 800×1280), шрифт и цвета.
Время/дата/погода умеют обновляться **нативно на устройстве**. А свои данные (Freedom24,
RSS, курсы, цитата) **рендерим в картинку и пушим** на устройство — pull (часы сами тянут
наш URL) на Times Frame **не работает**, проверено 3 способами (см. [`CLAUDE.md`](./CLAUDE.md)).

## Источники данных

### Freedom24 (Tradernet API) — режим только для чтения

Циферблат **только читает** финансовые данные и **никогда не совершает сделок**, поэтому
авторизация выполняется в **read-only режиме**: команда `authByLogin` с параметром
**`viewOnlyMode: true`** («авторизация только в режиме просмотра аккаунта»).

```json
{
  "cmd": "authByLogin",
  "params": {
    "login": "user@example.com",
    "password": "***",
    "viewOnlyMode": true,
    "rememberMe": 1,
    "getAccounts": false,
    "userId": 1234
  }
}
```

- При `getAccounts: true` приходит список аккаунтов → выбираем `userId` → повторный вызов
  с этим `userId` и `getAccounts: false`. `rememberMe: 1` держит сессию ~2 недели.
- Альтернатива — авторизация по public/private ключам (раздел «Ключ API» в Tradernet).
- Доки: <https://freedom24.com/tradernet-api>. Есть Python SDK и community PHP SDK.
- **Секреты не коммитим** — только `.env` / переменные окружения.

### Погода

Нативная погода Times Frame (настраивается в приложении Divoom) либо внешний провайдер
(OpenWeather / Open-Meteo) при рендере фона картинкой.

## Архитектура (РЕШЕНО)

Устройство доступно только в домашней сети, Contabo — снаружи, домашний ПК не всегда
включён. Решение — задействовать всегда-включённый роутер **FRITZ!Box 7690** с встроенным
**WireGuard**:

```
Contabo (всегда онлайн): Go-сервис
   ├─ Freedom24 (read-only) + погода + RSS Habr + курсы + цитата
   ├─ рендерит картинку 800×1280
   └─ пушит на часы ──┐
                      │  WireGuard-туннель Contabo ↔ FRITZ!Box 7690
   FRITZ!Box 7690 ◄───┘  (роутер всегда включён)
        │ домашняя сеть
   Divoom Times Frame (192.168.178.40:9000)
```

- **Pull не работает** на Times Frame (часы не тянут наш URL — проверено 3 способами,
  см. [`CLAUDE.md`](./CLAUDE.md)) → используем **push рендереной картинки**.
- **WireGuard** даёт Contabo доступ в домашнюю LAN; ничего не торчит в открытый интернет.
- Нового железа не нужно, ПК может быть выключен, обновления 24/7.
- **Живые часы** — нативным `disp`-слоем поверх картинки (устройство само обновляет время
  между пушами данных).
- Рендерер + пуш пишем первыми и сначала гоняем с этой машины; затем код переезжает на
  Contabo. Шаги WireGuard и деплой Go-сервиса — отдельным промптом/проектом.
- План Б: дешёвый always-on узел дома (Raspberry Pi) как пушер — с FRITZ!Box не нужен.

## Статус

- [x] MCP-сервер установлен, собран, зарегистрирован.
- [x] Связь с устройством проверена.
- [x] API и возможности изучены, снимки устройства сохранены в `reference/`.
- [x] Зафиксирован read-only способ авторизации Freedom24 (`viewOnlyMode: true`).
- [x] Проверено: pull на Times Frame не работает → выбран **push рендереной картинки**.
- [x] Решена топология: Contabo (Go) + WireGuard через FRITZ!Box 7690.
- [x] Проверено на устройстве: **анимация** возможна (animated WebP как **элемент** NET_PIC
  в бандле; фоном — статика); **живые часы** — нативным слоем `disp 4`.
- [ ] Рендерер картинки 800×1280 (часы-оверлей + погода + RSS + курсы + цитата + Freedom24).
- [ ] Интеграция Freedom24 (read-only), погоды, RSS Habr, курсов, цитаты.
- [ ] WireGuard FRITZ!Box ↔ Contabo + деплой Go-сервиса на Contabo.

## Ссылки

- MCP-сервер: <https://github.com/DivoomDevelop/mcp-divoom-lan>
- Визуальный редактор циферблатов: <https://divoomdevelop.github.io/divoom-watchface-visual-editor_v2/>
- Freedom24 Tradernet API: <https://freedom24.com/tradernet-api>
