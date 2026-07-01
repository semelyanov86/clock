# CLAUDE.md — проект `clock` (Divoom Times Frame)

Инструкции для будущих сессий Claude Code в этом проекте. Язык общения — русский,
код и комментарии — английский (см. глобальный `~/.claude/CLAUDE.md`).

## Что это за проект

Разработка кастомного **циферблата (watchface / dial)** для устройства
**Divoom Times Frame**, на котором будут показываться:

1. **Финансовая информация из Freedom24** (Tradernet API) — портфель / тикеры / P&L.
2. **Часы** (время, дата, день недели).
3. **Погода** (текущая + прогноз).

Референс желаемого вида — встроенная тема **«HUD Finance» (ClockId 218)**, которая
сейчас стоит на устройстве (см. `reference/device-clock-218-hud-finance.json`). Её
финансовые виджеты тянут данные из облака Divoom; наша задача — показывать **свои**
данные из Freedom24, поэтому встроенные финансовые компоненты не подходят напрямую
(см. раздел «Архитектура циферблата»).

## Устройство (факты, проверено)

| Параметр        | Значение                                              |
|-----------------|-------------------------------------------------------|
| Модель          | Divoom Times Frame (`DeviceType: "Frame"`)            |
| IP              | `192.168.178.40`                                      |
| Порт LAN API    | `9000` (HTTP)                                          |
| DeviceId        | `300378939`                                           |
| Логический холст| **800 × 1280** (портрет)                              |
| Шрифтов на устр.| 161 (`reference/device-fonts.json`)                   |
| Текущая тема    | ClockId 218 «HUD Finance» (`NameEn: "HUD Finance"`)   |
| Роутер (шлюз)   | AVM FRITZ!Box 7690 @ `192.168.178.1` (встроенный WireGuard) |

Проверка живости: `ping 192.168.178.40` или `curl` (см. README, раздел «Проверка связи»).

## MCP-сервер `divoom-lan`

- **Репозиторий:** склонирован в `tools/mcp-divoom-lan/` (upstream:
  https://github.com/DivoomDevelop/mcp-divoom-lan). Node.js/TypeScript, собирается в
  `tools/mcp-divoom-lan/dist/index.js`.
- **Зарегистрирован** в `/data/web/clock/.mcp.json` (project scope), env:
  `DIVOOM_DEVICE_HOST=192.168.178.40`, `DIVOOM_DEVICE_PORT=9000`, `DIVOOM_TIMEOUT_MS=45000`.
- **Пересборка после изменений в исходнике:** `cd tools/mcp-divoom-lan && npm run build`.
- Инструменты MCP доступны в новой сессии Claude Code (сервер поднимается при старте
  сессии). В рамках одной уже запущенной сессии новый MCP не подхватывается — нужен
  рестарт сессии.

### Транспорт / протокол LAN API (важно)

Сервер общается с устройством по HTTP:

- **JSON-команды:** `POST http://192.168.178.40:9000/divoom_api`, тело
  `{ "Command": "<Namespace/Action>", "ReturnCode": 0, ... }`. В **запросе** корневой
  `ReturnCode` всегда `0`; в **ответе** `ReturnCode: 0` означает успех, иначе смотри
  `ReturnMessage`.
- **Файловые (multipart) эндпоинты:** `/create_local_clock`, `/patch_local_clock`,
  `/replace_clock_dial_bg`, `/upload`. Первая часть — JSON (`name="json"`,
  `filename="cmd.json"`), вторая — один файл. Каждая часть несёт свой `Content-Length`.
  За один запрос — **ровно один файл**.

### Инструменты MCP (18 шт.)

**Чтение (безопасно):**
- `watchface_get_local` — `Device/GetLocalClockInfo` (текущий или конкретный ClockId).
- `watchface_get_fonts_local` — `Device/GetLocalFontList` (что реально стоит на устройстве).
- `watchface_get_brightness` — `Sys/GetBrightness`.
- `watchface_get_store_market_list` — `Device/GetStoreClockMarketList`.
- `watchface_protocol_quick_reference` — сводка правил протокола.

**Управление (видимый эффект — подтверждать у пользователя):**
- `watchface_set_clock_select` — `Channel/SetClockSelectId` (переключить активный циферблат).
- `watchface_set_brightness` — `Channel/SetBrightness` (0–100).
- `watchface_onoff_screen` — `Channel/OnOffScreen` (1=вкл, 0=выкл).

**Запись/ассеты (осторожно, меняет данные на устройстве):**
- `watchface_patch_local` — `Device/PatchLocalClockInfo`. Точечные правки полей через
  `itemPatchList` (по индексу) или `itemPatchByRoleList` (по роли). С `dialAssetsPath`
  переключается в multipart и заливает фон/бандл.
- `watchface_replace_dial_bg_file` — заменить **только** кэш фоновой картинки (не меняет
  `DeviceImageUrl` в cfg). JPEG/WebP, ≤ 500 KiB, рекоменд. 800×1280.
- `watchface_create_local_clock` — создать новый локальный циферблат (фон + ItemList).
- `watchface_upload_file` — `POST /upload` произвольного файла.
- `watchface_reset_local_then_cloud` — `Device/ResetLocalClockFromServer`,
  **деструктивно** (удаляет локальные файлы перед обновлением из облака).
- `watchface_raw_command` — произвольный `Command` на `/divoom_api`.

**Каталоги-помощники (локальные, без сети):**
- `watchface_disp_catalog` — каталог `disp` id (194 записи).
- `watchface_font_catalog` — подбор `font` id под сценарий (digits / cjk / latin / …).
- `watchface_template_search` — ~20 готовых скелетов циферблатов.
- `watchface_layout_suggest` — медианная геометрия (size/x/y/w/h) для конкретного `disp`.

### MCP-ресурсы (9 шт.)

`divoom://guide/quick-reference`, `divoom://skill/watchface-customization`,
`divoom://font/catalog`, `divoom://font/guide`, `divoom://disp/catalog`,
`divoom://watchface/schema`, `divoom://watchface/example-minimal`,
`divoom://guide/ai-watchface`, `divoom://templates/curated`.

## Шпаргалка по `disp` id (для нашего дизайна)

Полный каталог: `tools/mcp-divoom-lan/resources/disp-catalog.json` или инструмент
`watchface_disp_catalog`. Самое нужное:

**Часы / дата:**
- `4` HOUR_MIN, `5` HOUR_MIN_SEC, `242` HOUR_MIN_COLOR (раздельный цвет час/двоеточие/мин)
- `406–409` поразрядные цифры часов/минут (для крупных «ламповых» часов)
- `6` ENG_WEEK, `37` ENG_WEEK_THREE, `153` YEAR_MON_DAY, `193` MONTH_DAY_YEAR,
  `241` CALENDAR_DATES (сетка календаря), `36` MON_YEAR

**Погода:**
- `54` WEATHER_WORD, `55/63/64` WEATHER_GIF (иконки), `240/260` WebP-погода
- `32/254/339` TEMP_DIGIT (температура), `96/97` TODAY_MAX/MIN_TEMP,
  `157` LOWEST_TO_HIGHEST_TEMP («12C-23C»)
- `82–95` многодневный прогноз (завтра / послезавтра / +3 / +4: погода и темпы)
- `149` влажность, `204` восход/закат

**Произвольный текст (КЛЮЧЕВОЕ для Freedom24):**
- `154` NET_TEXT_MESSAGE — «пользовательские сетевые данные» (поле UserMessage в Config)
- `155` USER_TEXT_MESSAGE — «пользовательские данные»
- `49` TEXT_MESSAGE, `56` MUL_TEXT_MESSAGE (многострочный)

**Финансовые компоненты HUD-темы (НЕ кормятся нашими данными):**
- `261` DIAL_COMPONENT_START + `262–270` — суб-циферблаты, тянут данные из облака Divoom.
- Сетевые картинки-спектры: `13/125/126/173/174/175` (NET*_PIC).

**Шрифты на устройстве (примеры id):** `24` = «DS Digital» (цифры/часы), `6` = «Career»
(использует HUD-тема), `26` = «SourceHanSans» (CJK). Перед фиксацией шрифта сверяйся с
`watchface_get_fonts_local` — список на устройстве является подмножеством каталога.

## Архитектура циферблата (результаты спайка 2026-06-07)

Два жёстких факта определяют архитектуру:

1. **Встроенные финансовые виджеты HUD-темы берут данные из облака Divoom** — это
   суб-циферблаты (`Config: SubClockId_N=<id>`), не наши слоты. Свои данные Freedom24
   в них не влить.
2. **Pull-модель НЕ работает на Times Frame (проверено 3 независимыми тестами).**
   Локальный HTTP-сервер с логом, устройство переводилось на наш URL разными способами —
   **ни одного запроса** от `192.168.178.40` (firewall off, сокет `0.0.0.0`, маршрут
   прямой; причина не в сети):
   - **NET_PIC `image_addr` = `http://<lan>/x.gif`** → нет фетча даже после
     `Channel/SetClockSelectId`. Сетевые картинки (`group1/M00/...`) идут через файл-сервер
     Divoom.
   - **`Draw/SendHttpItemList`** (механизм Times Gate из проекта `Firemoon777/divoom-timegate-bg3`,
     где часы опрашивают `TextString`-URL и читают `{"DispData":...}`) → на Frame
     **недоступен**: порт `80/post` закрыт, `9000/post` → 404, на `9000/divoom_api`
     команда отбивается `"Only accept JSON parameters"`. У Frame нет Pixoo-движка.
   - **Watchface-элемент `disp 154` (NET_TEXT_MESSAGE) с `TextString`+`update_time`** в
     `Device/CreateLocalClock` → циферблат создаётся (`ClockId 60000`), переключается,
     но URL **не опрашивается**.

   Вывод: **Times Frame ≠ Times Gate.** Network-виджеты Frame (RSS, финансы, погода)
   настраиваются через приложение/облако Divoom и тянутся инфраструктурой Divoom; задать
   свой URL по LAN-API нельзя. Единственная непроверенная лазейка для pull — настроить
   «network text/RSS» виджет на наш URL **через мобильное приложение Divoom** (если оно
   пускает произвольные URL); это вне нашего кода и, вероятно, всё равно через облако Divoom.

**Следствие:** надёжный, управляемый нами способ показать свои данные (Freedom24, RSS habr,
курсы, цитата, погода) — **рендерить картинку 800×1280 и ПУШить её на устройство по LAN**
(`/replace_clock_dial_bg`, `/create_local_clock`, `/patch_local_clock` — все проверены,
`ReturnCode 0`). Нативный слой часов (`disp 4/242`) можно класть поверх, чтобы время шло
само между пушами. Вид — богатый (как на фото).

### Анимация и живые часы (проверено на устройстве 2026-06-07)

- **Анимация ДА — но только элементом, не фоном.** Animated WebP, залитый как **фон**
  (`DialAssets:"image"`), показывается **одним кадром** (статикой). Тот же animated WebP
  как **элемент** — слот **NET_PIC (`disp 13`)** с `image_addr`/`bundle_image` на webp-лист
  внутри `clock_bg.tar.gz` (`DialAssets:"bundle"`) — **анимируется** (подтверждено визуально).
  Полноэкранный анимированный элемент `0,0,800,1280` поверх статичного `clock_bg.jpg` =
  анимированный циферблат.
- **GIF при заливке отклоняется** валидатором (фон — JPEG/WebP; элементы бандла —
  JPEG/WebP/PNG). Любой GIF → animated WebP: `ffmpeg -i in.gif -loop 0 out.webp` или
  `convert in.gif out.webp`. Размер фона ≤ 500 KiB; лист бандла валиден по магии формата.
- **Живые часы — нативным слоем.** Кладём элемент `disp 4` (HOUR_MIN) поверх (`hier:2`,
  `transp:100`, шрифт напр. `24` = DS Digital). Прибор сам рисует текущее время и обновляет
  его между нашими пушами картинки — перезаливать ради времени не нужно. (Таймзона/синхро
  времени настраивается в приложении Divoom.)
- **Рабочий рецепт сборки циферблата:** `clock_bg.tar.gz` (USTAR+gzip) = `clock_bg.jpg`
  (статичный фон с данными Freedom24/погода/RSS/курсы/цитата) + `anim.webp` (анимированный
  декор-элемент, если нужен) → `watchface_create_local_clock` с `DialAssets:"bundle"` и
  `ItemList` [NET_PIC-элемент(ы) + `disp 4` часы] → `watchface_set_clock_select` на новый
  `ClockId`. Геометрия в холсте 800×1280; `alig` 3/4/5; `hier` 0/1/2.

> Тестовые локальные циферблаты на устройстве: `ClockId 60000` «SPIKE NetText»,
> `60001` «SPIKE Anim+Clock», `60002` «SPIKE ElemAnim» — удалить в приложении Divoom
> (или перезапишутся будущими локальными циферблатами).

### Принятая топология (РЕШЕНО): Contabo + WireGuard через FRITZ!Box

Устройство `192.168.178.40` доступно только в домашней сети; Contabo из интернета до него
не дотянется напрямую, а домашний ПК не всегда включён. Решение — использовать **роутер
(он всегда включён)**:

- **Роутер:** AVM **FRITZ!Box 7690** (шлюз `192.168.178.1`, FRITZ!OS 8.x) — имеет
  **встроенный WireGuard**.
- **Contabo (всегда онлайн):** Go-сервис делает ВСЁ — Freedom24 (read-only) + погода +
  RSS Habr + курсы + цитата → рендер картинки 800×1280 → пуш на устройство.
- **WireGuard-туннель Contabo ↔ FRITZ!Box 7690:** даёт Contabo доступ в домашнюю LAN,
  и Go-сервис пушит картинку прямо на `192.168.178.40:9000` через туннель.
- Плюсы: ничего не торчит в открытый интернет (шифрованный туннель), нового железа не
  нужно, ПК может быть выключен, обновления 24/7. Всё на Contabo, как и хотел пользователь.
- Топология туннеля: вероятно немецкий канал за CGNAT/DS-Lite → удобнее **FRITZ!Box как
  WireGuard-клиент, Contabo как сервер** (FRITZ!Box инициирует исходящее к публичному IP
  Contabo; Contabo маршрутит обратно в `192.168.178.0/24`). Точные шаги WireGuard +
  деплой Go-сервиса оформляются отдельным промптом для отдельного проекта на Contabo.
- **План Б** (если не хочется настраивать VPN на роутере): дешёвый always-on узел дома
  (Raspberry Pi ~€30) как пушер. С FRITZ!Box не требуется.

Рендерер (сборка картинки) + пуш нужны в любом случае — их пишем первыми и сначала
гоняем с этой машины (она сейчас включена), затем код 1-в-1 переезжает на Contabo.

## Go-сервис: скиллы, правила разработки и деплой

Рендерер картинки 800×1280 + пушер на устройство пишется на **Go**. Локально установлены
Go 1.25.1 и `task` (go-task) 3.51.1.

### Подключённые Go-скиллы (правила для кода проекта)

В `.claude/skills/` (симлинки на `.agents/skills/`, лок-файл `skills-lock.json`) лежат
**12 Go-скиллов**. Это правила, которым ДОЛЖЕН следовать код. Вызывай нужный скилл при работе
над соответствующей задачей.

| Скилл | О чём |
|-------|-------|
| `golang-project-layout` | структура (cmd/internal/pkg), 12-factor, имя модуля |
| `golang-design-patterns` | functional options, конструкторы, ресурсы, graceful shutdown, resilience |
| `golang-code-style` | стиль: ранний return, объявления переменных, длина строк, switch |
| `golang-error-handling` | оборачивание `%w`, `errors.Is/As`, single-handling rule, `slog` |
| `golang-testing` | table-driven тесты, subtests, бенчмарки, fuzzing, покрытие |
| `golang-modernize` | идиомы Go 1.21–1.26, миграция deprecated API |
| `golang-popular-libraries` | подбор библиотек (сначала stdlib) |
| `golang-documentation` | godoc-комментарии, README, CHANGELOG |
| `golang-continuous-integration` | GitHub Actions: test / lint / SAST / release |
| `golang-troubleshooting` | методология отладки, pprof, race, delve |
| `golang-patterns`, `golang-pro` | сводные идиомы (concurrency, интерфейсы, generics) |

Источники: `samber/cc-skills-golang` (10 шт.) + `affaan-m/everything-claude-code`,
`jeffallan/claude-skills`.

### Ключевые правила (выжимка — соблюдать всегда)

- **Архитектуру согласовать с пользователем** перед структурированием; не over-engineer'ить
  (маленькому сервису не нужны слои абстракций). 12-factor: конфиг из ENV, логи в stdout,
  stateless, graceful shutdown.
- **Layout:** `main` — только в `cmd/<name>/` (parse flags → wire deps → `Run()`); логика в
  `internal/`. Имя модуля = URL репозитория, lowercase, дефисы.
- **Конструкторы:** functional options (`WithX(...) Option`); без `init()` и мутабельных
  глобалов; зависимости инжектить (accept interfaces, return structs).
- **Ошибки:** всегда проверять; оборачивать `fmt.Errorf("ctx: %w", err)`; строки lowercase
  без пунктуации; `errors.Is/As`; правило одного обработчика — **либо** логировать, **либо**
  возвращать (не оба); `panic` — только на невозможных инвариантах, не на ожидаемых ошибках.
- **Контекст и лимиты:** `context.Context` первым параметром; таймаут на КАЖДЫЙ внешний вызов
  (Freedom24, погода, RSS, пуш на устройство); ретраи проверяют `ctx.Err()`.
- **Логирование:** структурный `slog`, не `fmt.Println` / `log.Printf`.
- **Тесты:** table-driven + subtests, `go test -race -shuffle=on`; покрытие 80%+ (крит. логика
  100%); HTTP — через `httptest`. TDD приветствуется.
- **Стиль:** ранний return, без лишнего `else`, `switch` вместо if-else цепочек, ≤4 параметра
  (иначе options-struct), `gofmt -s`.

### Инструменты (ставить локально по мере надобности)

- `golangci-lint` v2.6+ (с линтером `modernize`):
  `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`
- `govulncheck`: `go install golang.org/x/vuln/cmd/govulncheck@latest`
- опц.: `dlv` (отладка), `air` (live-reload). Конфиг линтера — `.golangci.yml` в корне.

### Сервер деплоя — Contabo VDS (проверено 2026-06-07)

`ssh sergeyem.ru` → пользователь `sergey` (passwordless sudo). Сводка для контекста:

| Параметр | Значение |
|----------|----------|
| Хост | `vds01`, IP `79.143.176.198`, домен `sergeyem.ru` |
| ОС / железо | Ubuntu 24.04.4 LTS, KVM/QEMU, 4 vCPU, 7.8 GB RAM, 145 GB (free ~117), без swap |
| Веб-стек | nginx (active), PHP 8.5 + Laravel Octane (RoadRunner), MySQL, Redis |
| **Go** | **1.26.4** в `/usr/local/go` (official tarball; PATH через `/etc/profile.d/go.sh`, `GOPATH=~/go`). Обновление: скачать новый tarball с go.dev → проверить sha256 → `sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf <tarball>` |
| Docker / task | отсутствуют |
| WireGuard | **НЕ настроен** (нет `/etc/wireguard`) — нужен для доступа к Divoom `192.168.178.40` через FRITZ!Box (см. топологию выше) |
| Управление | **Ansible** (конфиги помечены `# Ansible managed`; плейбуки лежат вне сервера → ручные правки конфигов могут быть перезатёрты) |

**Конвенция деплоя сайтов (повторить для clock-сервиса):**
- Релизы: `/var/www/<site>/current` + персистентный `/var/www/<site>/shared`, владелец
  `site_<name>:site_<name>`.
- systemd-юнит `/etc/systemd/system/<name>.service`: выделенный `User=`/`Group=site_<name>`,
  `WorkingDirectory=.../current`, hardening (`NoNewPrivileges`, `ProtectSystem=strict`,
  `ReadWritePaths=...`), `Restart=always`, логи в journald / `/var/log/...`.
- nginx (если нужен HTTP) — `/etc/nginx/sites-available/<site>.conf` → симлинк в
  `sites-enabled`, TLS через certbot (webroot `/var/www/letsencrypt`), snippets
  `security-headers.conf` / `deny-internal.conf`.

> Для clock-сервиса инфра (site-пользователь, systemd-юнит, WireGuard-туннель) **ещё не
> заведена** — отдельный шаг. Сервис — фоновый демон (пуш картинки на устройство), HTTP-сайт
> ему, скорее всего, не нужен. Рекомендуемый деплой: собрать статический бинарь
> (`CGO_ENABLED=0`, `task build:linux`) → доставить → запустить под systemd.

### Taskfile

Команды собраны в `Taskfile.yml` (go-task). `task` без аргументов — список всех команд.
- **Сервер:** `task ssh` (интерактивный вход), `task ssh:run -- "<cmd>"`, `task logs`.
- **Go-loop:** `build`, `run`, `test`, `test:race`, `test:cover`, `cover:html`, `bench`,
  `fmt`, `vet`, `lint`, `vuln`, `tidy`, `audit` (полный гейт).
- **Деплой:** `build:linux`, `deploy` (пока заливает в `/tmp`, install ждёт инфры).

## Источники данных

- **Freedom24 / Tradernet API** — REST/WebSocket/FIX. Доки:
  https://freedom24.com/tradernet-api . Есть Python SDK и community PHP SDK
  (`MasyaSmv/freedom-broker-api`).
  - **Сессия только читает, НИКОГДА не торгует.** Код шлёт исключительно read-команды
    (`getUserPositions`, `getSecurityInfo`, `getMarketReviews`) и не отправляет торговых
    приказов. Режим авторизации управляется `FREEDOM_VIEW_ONLY` (опция `WithViewOnly`).
    > **РЕШЕНО на E2E (2026-06-07): по умолчанию `FREEDOM_VIEW_ONLY=false` (полная сессия).**
    > В режиме `viewOnlyMode:true` Tradernet **скрывает детализацию портфеля** —
    > `getUserPositions`/`getOPQ` возвращают пустой `pos[]`, а агрегат (`portfolioValue`/
    > `Stotal` ≈ €1230) — это НЕ настоящий баланс (реальный ≈ €11 760). Позиции и верный
    > тотал приходят только в полной сессии. Согласовано с пользователем: показывать
    > позиции важнее, безопасность обеспечена тем, что сервис не торгует.
    Пример запроса:

    ```json
    {
      "cmd": "authByLogin",
      "params": {
        "login": "user@example.com",
        "password": "***",
        "viewOnlyMode": false,
        "rememberMe": 1,
        "getAccounts": false,
        "userId": 1234
      }
    }
    ```

    Поток: при `getAccounts: true` приходит список аккаунтов → выбираем нужный `userId`
    → повторный `authByLogin` с этим `userId` и `getAccounts: false`. `rememberMe: 1`
    держит сессию ~2 недели.
  - **Альтернатива по ключам:** раздел «Ключ API» в Tradernet (public/private ключи,
    напр. `FREEDOM_PUBLIC_KEY` / `FREEDOM_PRIVATE_KEY`).
  - **Секреты в репозиторий не коммитить** — только `.env` / переменные окружения.
- **Погода** — либо нативная погода Times Frame (настраивается в приложении Divoom и
  читается нативными disp), либо внешний провайдер (OpenWeather/Open-Meteo) при рендере
  картинкой в варианте A.

## Правила безопасности при работе с устройством

1. **Read-before-write:** перед любой записью — `watchface_get_local`. Если `ItemList`
   пуст — НЕ писать, сперва переключиться на редактируемый циферблат
   (`watchface_set_clock_select`). Сервер сам блокирует patch при пустом ItemList.
2. **Не создавать** новый циферблат (`watchface_create_local_clock`) без явного запроса
   пользователя.
3. `watchface_reset_local_then_cloud` — **деструктивно**, использовать только по явной
   просьбе.
4. `watchface_set_clock_select` и `watchface_onoff_screen` дают видимый эффект на экране —
   подтверждать намерение.
5. В `patch.*` **не передавать `item_id`** (затрёт device-side id и сломает привязки меню),
   кроме явного переименования слота.
6. Картинки: фон — JPEG (`FF D8`) или WebP, ≤ 500 KiB, 800×1280. Элементы в бандле
   (`clock_bg.tar.gz`) — JPEG/WebP/PNG. Видимым элементам ставить `transp: 100` (иначе
   невидимы). `hier` только `0/1/2`. `alig`: `3`=центр, `4`=лево, `5`=право.

## Структура репозитория

```
/data/web/clock
├── .mcp.json                 # регистрация MCP-сервера divoom-lan (project scope)
├── .env.example              # имена ENV-переменных (секреты — в .env, он в .gitignore)
├── .gitignore                # .env, bin/, preview*.jpg, coverage
├── go.mod                    # module github.com/semelyanov86/clock
├── Taskfile.yml              # команды go-task: task ssh, build, run, preview, push, audit, deploy…
├── skills-lock.json          # лок-файл источников Go-скиллов
├── .claude/skills/           # 12 Go-скиллов (симлинки на .agents/skills/)
├── .agents/skills/           # исходники Go-скиллов
├── CLAUDE.md                 # этот файл
├── README.md                 # обзор проекта
├── cmd/clock/main.go         # точка входа: флаги/env → wiring → app.Run; режимы --once/--fake/--push
├── internal/
│   ├── config/               # Config из ENV (12-factor) + валидация
│   ├── model/                # домен (Weather, Portfolio, Instrument, NewsItem, Quote, Snapshot) + sample
│   ├── divoom/               # LAN-клиент устройства: JSON (/divoom_api) + multipart (create/replace)
│   ├── weather/              # Open-Meteo (без ключа)
│   ├── favqs/                # favqs.com (цитаты, англ.)
│   ├── freedom/              # Tradernet сессия: authByLogin→SID; getUserPositions, getMarketReviews, getSecurityInfo
│   ├── claudeusage/          # лимиты Claude (/usage): OAuth-токен → unified rate-limit заголовки
│   ├── render/               # рендер 800×1280 → JPEG; fonts/*.ttf встроены через go:embed
│   └── app/                  # снапшот-стор + планировщик фетчеров + кадровый цикл + пуш
├── reference/                # снимки реального состояния устройства
│   ├── device-clock-218-hud-finance.json   # текущая тема (референс вёрстки)
│   └── device-fonts.json                    # 161 шрифт на устройстве
└── tools/
    └── mcp-divoom-lan/       # склонированный + собранный MCP-сервер
```

## Полезные команды

```bash
# Все команды проекта (go-task)
task                      # список команд
task ssh                  # интерактивный вход на сервер Contabo (sergeyem.ru)
task audit                # полный гейт: fmt + vet + lint + race-тесты + vuln

# Пересобрать MCP-сервер
cd tools/mcp-divoom-lan && npm run build

# Быстрая проверка связи (read-only)
curl -s -X POST http://192.168.178.40:9000/divoom_api \
  -H 'Content-Type: application/json; charset=UTF-8' \
  -d '{"Command":"Sys/GetBrightness","ReturnCode":0}'

# Прочитать текущий циферблат
curl -s -X POST http://192.168.178.40:9000/divoom_api \
  -H 'Content-Type: application/json; charset=UTF-8' \
  -d '{"Command":"Device/GetLocalClockInfo","ReturnCode":0,"UseCurrentDisplayClock":1}'
```

## Go-сервис `clock` — реализован (2026-06-07)

Каркас написан и собирается; рендер проверен (превью 800×1280, < 500 KiB). E2E с реальным
устройством и секретами — отдельный шаг (см. ниже).

**Зафиксированные решения (согласованы с пользователем):**
- **Раскладка — гибрид:** закреплённая шапка (область нативных часов + дата RU + погода
  сейчас + прогноз 3ч/3дня), снизу ротация 3 страниц по кадрам: «Портфель», «Рынки»
  (ETF×4 + Brent + курсы к ₽), «Календарь + новости + цитата».
- **Часы — нативный слой устройства** (`disp 4`, создаётся один раз через
  `/create_local_clock`, дальше тикает сам). Геометрия — `render.NativeClockSlot()`.
  ⚠️ **СМЕНА ФОНА (выверено вживую 2026-06-07):** `/replace_clock_dial_bg` на Times Frame
  меняет лишь КЭШ и НЕ перерисовывает живой экран — отображается фон, «вшитый» в циферблат.
  Рабочая схема смены страницы: **`PatchLocalClockInfo` (multipart `/patch_local_clock`,
  меняет реальный backdrop, ItemList сохраняется) + `SetClockSelect` (форс перерисовки).**
  Это `divoom.PatchDialBg` + `SetClockSelect` в `app.pushFrame`. Каждый кадр = одна запись
  фона во флеш — поэтому интервал держим помягче (на сервере `FRAME_INTERVAL=30s`).
  ⚠️ **Условие:** в приложении Divoom выставить таймзону **Europe/Berlin** и **24ч** —
  нативный слой берёт время и формат из настроек прибора.
- **Погода — Open-Meteo, без ключа** (Гамбург, lat 53.55 / lon 9.99).
- **Цитаты — favqs.com (англ.)**, через `FAVQS_API_TOKEN` (страница `/api_keys`); без токена —
  фолбэк `/qotd`.
- **Анимация новостей** — сменой кадра при каждом пуше (новость/цитата ротируются по
  `cycleIndex(frame, …)`), без animated WebP.
- **Динамика везде:** ▲ зелёный рост / ▼ красный падёж.

**Freedom24 / Tradernet — схема, ВЫВЕРЕННАЯ вживую (E2E 2026-06-07):**
- Транспорт: HTTPS POST, поле формы `q = JSON.stringify({cmd, SID, params})`. Gateway —
  `https://tradernet.com/api`. **Gateway за Cloudflare:** дефолтный Go `User-Agent` → HTTP 403.
  Клиент ОБЯЗАТЕЛЬНО шлёт браузерный `User-Agent` + `Accept` (иначе всё отбивается 403).
- `authByLogin` (`viewOnlyMode` из `FREEDOM_VIEW_ONLY`, `rememberMe:1`) → `{"success":true,
  "logged":true,"SID":"…","userId":…}`; **SID в `q` каждого запроса** (cookie-jar тоже держим).
  `userId` из ответа захватывается для `requestedUserId`. Ошибки: `{"error"/"errMsg":…,"code":N}`.
- **Портфель — `getUserPositions`** (`params:{requestedUserId}`), НЕ getOPQ (getOPQ в view-only
  отдаёт пустой `pos[]`). Корень: `pos[]`, `acc[]` (кэш), `money_detailed`, `net_assets`, `totals`.
  Позиция: `i` тикер, `q` кол-во, `market_value` (в валюте позиции), `mkt_price`, `close_price`
  пред. закрытие, `bal_price_a` средняя цена покупки, **`profit_close` ОБЩИЙ P&L**,
  **`open_bal`** (=0 → бесплатная бонусная акция, отфильтровываем), `curr`/`base_currency`, `currval`.
  ⚠️ **ТОТАЛ — из `net_assets.net_assets`** (=`totals.total_trade_positions`, авторитетная сумма,
  ровно как в приложении Freedom24), а НЕ ручной суммой позиций. Валюта тотала — `net_assets.currency`
  (обычно USD); конвертим в EUR по курсам из **`money_detailed[<cur>].rate`** (валюта→RUB, у каждой
  валюты свой; `currval` в позициях НЕнадёжен — расходится по строкам даже в одной валюте, поэтому не
  использовать его для тотала). Фолбэк тотала (если нет net_assets/totals) — ручная сумма позиций+кэш.
  ⚠️ **«Динамика» позиции = ОБЩИЙ P&L** (`profit_close`/`open_bal`), НЕ дневное изменение: держим
  низколиквидные ETF (4GLD/IQQ0), которые маркируются по вчерашнему закрытию → `mkt_price==close_price`
  → дневная дельта = ложный 0. Тотал-дельта = сумма `profit_close` в EUR. (Согласовано, E2E 2026-07-01.)
  ⚠️ только в полной сессии (см. `FREEDOM_VIEW_ONLY` выше).
- `getSecurityInfo` (котировки, **без авторизации**): цена = `ltp`, пред.закрытие = `ClosePrice`,
  валюта = `base_currency`. **`pcp` нет, `chg` ненадёжен** → дельту считаем из `ltp−ClosePrice`.
  Символы: рубль = **RUR** (`EUR/RUR` и т.п.), Brent-прокси = **`BRNT.EU`** (прямого бенчмарка нет;
  поиск тикеров — команда `tickerFinder`).
- `getMarketReviews` (новости) → `list[]` с `title`/`date`. favqs + Open-Meteo — ок.

**Запуск:**
- `task preview` — 3 страницы на фейковых данных → `preview_*.jpg` (без сети/устройства).
- `task preview:live` / `task push` — реальные данные (читает `.env`), пуш на устройство.
- `task run` — сервисный цикл; `task build` / `build:linux` / `deploy`.
- Превью без секретов: `./bin/clock --once --fake --frame {0|1|2} --out f.jpg`.

**Шрифты встроены через `go:embed`** (`internal/render/fonts/DejaVu*`) → бинарь самодостаточный,
системные шрифты на сервере не нужны.

**Безопасность (после security-review, 2026-06-07):**
- **Строгая валидация ENV:** значение, заданное, но некорректное, — ошибка старта (не тихий
  дефолт); проверяются порт, интервалы, lat/lon, таймзоны, схема `FREEDOM_API_URL`.
- **`.env` → `chmod 600`** (см. `.env.example`); на сервере — systemd `EnvironmentFile` 0600.
- **Логи Freedom не текут:** тело ответа логируется только при `FREEDOM_LOG_BODIES=true`
  (для E2E), и даже тогда **SID редактируется**; иначе пишется лишь длина.
- **MCP-инструмент укреплён** (`tools/mcp-divoom-lan`): per-call `target.host` разрешён только
  из allowlist (`DIVOOM_ALLOWED_HOSTS`, по умолчанию = `DIVOOM_DEVICE_HOST`); чтение файлов
  опц. ограничено `DIVOOM_ASSET_ROOT` и всегда лимитировано `DIVOOM_MAX_UPLOAD_BYTES` (10 MiB).
  Шаблон конфига — `.mcp.json.example`. Транзитивные npm-уязвимости устранены (`npm audit fix`).
- **HTTP-клиенты:** таймаут + `io.LimitReader` (4 MiB) на каждом ответе; ошибки оборачиваются `%w`.

### Виджет «Лимиты Claude» (opt-in, 2026-06-07)

Опциональная 4-я страница ротации: два гейджа — **5-часовой блок** и **недельное окно**
использования лимитов Claude (как в `/usage`). Пакет `internal/claudeusage` читает OAuth-токен
Claude Code из `~/.claude/.credentials.json` и шлёт микро-`messages`-запрос (`max_tokens:1`,
системный промпт Claude Code, `anthropic-beta: oauth-2025-04-20`) на `api.anthropic.com`, забирая
заголовки `anthropic-ratelimit-unified-5h/7d-utilization` + `-reset`. **Подтверждено вживую**
(`count_tokens` заголовки НЕ отдаёт; `messages` — отдаёт).

- **Opt-in:** `CLAUDE_USAGE_ENABLED=true` (по умолчанию выкл). Когда выкл/нет данных — страница
  не показывается (динамический счётчик страниц в `render.go`; smoke-тест покрывает 3- и 4-стр.).
- **«Серая зона»:** подписочный OAuth-токен вне Claude Code + недокументированные заголовки —
  могут сломаться. Каждый опрос тратит ~1 токен той же квоты, что показывает.
- **Где креды:** токен читается на каждый опрос. **Авто-рефреш — через официальный `claude` CLI (РЕШЕНО, 2026-06-10).**
  Историю: сначала пробовали refresh_token-обмен прямо в Go-сервисе, но token-эндпоинт `https://platform.claude.com/v1/oauth/token`
  **отбивает «сырой» запрос HTTP 429** (видимо, отличает настоящий Claude Code от самодельного; браузерный UA не помог), и так
  13/13 попыток даже с backoff. При этом `claude -p "ok"` обновляет токен с этого же сервера без проблем. **Поэтому пивот:**
  токен продлевает сам CLI через systemd-сервис-демон `claude-token-refresh.{sh,service}` (см. `deploy/`): **event-driven,
  без polling** — спит ровно до `expiresAt`, будит `claude -p` один раз для refresh, снова спит ~8ч; backoff (15м→30м→60м→2ч)
  при сбое. `claude` НЕ обновляет здоровый токен (рефрешит только у/после истечения), поэтому будим в момент истечения →
  разрыв виджета ~10–15 сек на цикл (пока `claude` отрабатывает) — это ок.
  Go-рефреш оставлен в коде, но **выключен по умолчанию** (`CLAUDE_OAUTH_REFRESH=false`); ре-арм/проверка вручную:
  `clock --refresh-claude-token`. Если в нём, юнит `clock.service` должен иметь `ReadWritePaths=/home/sergey/.claude`.
  **Урок:** не долбить token-эндпоинт сырыми запросами — серия 429 ничего не даёт (ночь 8→9 июня: 192 × 429 без backoff,
  токен так и не обновился). Только официальный клиент.
- **ENV:** `CLAUDE_USAGE_ENABLED`, `CLAUDE_CREDENTIALS_PATH` (деф. `~/.claude/.credentials.json`),
  `CLAUDE_USAGE_MODEL` (деф. `claude-haiku-4-5-20251001`), `CLAUDE_API_URL`, `CLAUDE_USAGE_INTERVAL` (деф. 5m),
  `CLAUDE_OAUTH_REFRESH` (деф. **false** — рефреш делает CLI-таймер, см. выше), `CLAUDE_OAUTH_TOKEN_URL`, `CLAUDE_OAUTH_CLIENT_ID`.
- Превью: `./bin/clock --once --fake --frame 3 --out preview_claude.jpg`.

## Статус

- [x] MCP-сервер установлен, собран, зарегистрирован в `.mcp.json`.
- [x] Связь с устройством проверена (read-only + end-to-end через MCP stdio).
- [x] API и возможности изучены, снимки устройства сохранены в `reference/`.
- [x] Go-скиллы подключены (`.claude/skills`, `skills-lock.json`), правила в CLAUDE.md.
- [x] Сервер Contabo изучен (`ssh sergeyem.ru`), конвенции деплоя задокументированы.
- [x] `Taskfile.yml` создан (`task ssh` + Go-loop + preview + push + deploy).
- [x] Архитектура Go-сервиса согласована и реализована (`go.mod`, `cmd/clock`, `internal/*`).
- [x] Дизайн циферблата согласован (гибрид) и отрендерен (превью проверены).
- [x] Интеграция Freedom24 — **выверена вживую (E2E 2026-06-07):** WAF-фикс (браузерный UA),
      `getUserPositions` вместо getOPQ, дельта из `ltp−ClosePrice`, символы RUR/`BRNT.EU`,
      фильтр бонусных акций (`open_bal=0`), полная сессия (`FREEDOM_VIEW_ONLY=false`).
- [x] Интеграция погоды (Open-Meteo) + favqs (цитаты) + unit-тесты (httptest, `-race`).
- [x] Рендер/пуш циферблата реализованы (multipart 1-в-1 по firmware, тесты).
- [x] **E2E данных:** все 4 страницы отрендерены на реальных данных (`live_*.jpg`), баланс
      €11 760 + позиции, рынки, погода, новости, цитата, лимиты Claude — всё корректно.
- [ ] **E2E устройства:** первый `task push` → запомнить `ClockId` в `DIVOOM_CLOCK_ID`;
      выставить на приборе Берлин+24ч. (изображения в часы пока НЕ отправлялись.)
- [x] Виджет «Лимиты Claude» (opt-in): пакет `internal/claudeusage`, 4-я страница ротации,
      probe заголовков подтверждён вживую (см. подраздел выше). Включается `CLAUDE_USAGE_ENABLED=true`.
- [x] Деплой на Contabo: WireGuard-туннель до `192.168.178.40`, site-юзер `sergey`, systemd-юнит
      `clock.service`, авто-деплой через GitHub Actions. Рефреш OAuth-токена Claude решён в коде
      (см. подраздел выше + `deploy/clock.service` с `ReadWritePaths=/home/sergey/.claude`).
