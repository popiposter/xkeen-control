---
goal: "Вернуть штатному XKeen владение инфраструктурой и сделать панель удобной оболочкой"
version: "1.0"
date_created: "2026-10-02"
last_updated: "2026-10-02"
owner: "popiposter/xkeen-control"
status: "Deprecated"
tags: [architecture, refactor, xkeen, routing, reliability]
---

> Historical implementation/audit record. Current behavior and remaining work are in [plan index](README.md) and [ROADMAP](../docs/ROADMAP.md). Chronological checkpoints below are not current runtime state or permission to replay operations.

# Introduction

> **Deprecated 2026-10-03:** operator revised the contract to an unmodified native
> XKeen graphical shell. Use [v2](architecture-native-shell-v2.md) and
> [NATIVE-XKEEN](../docs/NATIVE-XKEEN.md). Shared native admission, patch/profile
> construction and their completion stages below are historical, not requirements.

![Status: Deprecated](https://img.shields.io/badge/status-Deprecated-red)

План основан на [аудите beta.5](audit-xkeen-foundation-2026-10-02.md). Оператор поручил полную последовательную реализацию и подтвердил чистый Entware. Доступ, установка и перезапуски сервисов разрешены; поддержка старых версий панели и миграция исключены. [Обновлённый контракт](../docs/NATIVE-XKEEN.md) имеет приоритет над историческими правилами D.1/D.2. Один обычный checkout, без worktrees.

Архитектурный контракт и отслеживание работ: [Issue #121](https://github.com/popiposter/xkeen-control/issues/121).

## 1. Requirements & Constraints

- **REQ-001**: XKeen устанавливает/обновляет себя, Xray, geodata и зависимости; создаёт native init/hooks и свои cron jobs. Панель вызывает native операции, не реализует их вторично.
- **REQ-002**: Только панель имеет собственный подписанный updater. Сохранить `internal/update`, `internal/release` и protected release pipeline.
- **REQ-003**: Установка/остановка/удаление панели не должны лишать уже настроенный XKeen работоспособности. Первый вход не меняет routing, core, cron или selection owner.
- **REQ-004**: Вся команда upstream отражена в [инвентаре](xkeen-command-inventory-v1.md): доступная typed операция, информационная ссылка, внутренний служебный flag либо явно отложенная capability. Никакой строки произвольной команды из UI.
- **REQ-005**: Поддержать подписки/ключи, массовое управление, видимый input новой URL и default disabled для новых WL узлов. Сохранить ручные enable/disable и тихое игнорирование неподдерживаемых протоколов.
- **REQ-006**: Редактор routing показывает установленные geodata/categories/contents, membership search, свои списки и объяснение first-match результата; сохраняет не редактируемые поля.
- **REQ-007**: Быстрый локальный backup и зашифрованный portable export/import покрывают native config + panel preferences + optional secrets с явным mapping на другом устройстве.
- **REQ-008**: Telegram alerts/typed control используют тех же владельцев операций. Один consumer на bot token. Unified multi-router bot не создаёт несколько competing pollers.
- **REQ-009**: Только один selection writer: native Xray, native XKeen SB либо panel adaptive. Выбор режима и последствия видны пользователю.
- **REQ-010**: Одно изменение — один job с конечным verified/failed/unknown состоянием. HTTP202 и exit0 без readback не означают успех; unknown не запускается повторно автоматически.
- **SEC-001**: Сохранить private listener, auth, CSRF, bounded output, SSRF protections, root-only secrets, отсутствие shell/file-manager API. Новые Telegram commands требуют отдельного обновления `SECURITY.md` до реализации.
- **SEC-002**: Исполнять только установленный поддержанный XKeen и fixed argv; не клонировать/исполнять непроверенный moving branch в panel request. Зафиксировать trust/verification возможностей каждого native канала; неподдержанный режим действия отключать явно.
- **SEC-003**: Диагностика upstream может содержать секреты. Собирать только проверенные поля; raw stdout/log/config не возвращать в API, GitHub или Telegram.
- **CON-001**: Разрешена реализация и проверка на чистом роутере, включая SSH key setup, установку и restart сервисов. Без blanket opkg upgrade, reboot или sustained benchmark.
- **CON-002**: Подготовленный чистый Entware разрешён для аппаратной приёмки. Сохранять текущий восстанавливаемый конфиг перед изменением; старые panel поколения не поддерживать.
- **CON-003**: Go/Node и сборка только вне роутера. Hot state в RAM; durable writes только при изменении настроек/конечном receipt. Нет постоянного flash event log.
- **PAT-001**: Один тонкий native adapter + очередь операций, один владелец config transaction, один selection owner. Не создавать общий новый workflow framework.
- **PAT-002**: Native файлы — исполняемая policy authority; UI projection выводится из них. `nodes.json` остаётся отдельной authority управляемых профилей. Импорт внешних правок explicit с diff, не silent overwrite.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Зафиксировать новую ответственность и прекращение прежнего release/install сценария. Зависимостей нет.
- Приёмка: issue-контракт и документы не содержат действующего указания продолжить старый Setup; прежние immutable releases и результаты сохранены как история.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Обновить master #1, ledger #4 и roadmap: архитектурная пауза, scope новой программы, отсутствие итогового live PASS. Привязать audit к `8adff1e…`, не переносить его выводы как проверку будущего HEAD. | | |
| TASK-002 | В `docs/ARCHITECTURE.md`, `docs/CONTROL-PLANE.md`, `SECURITY.md`, `AGENTS.md` описать native clean-install ownership transition; нынешние инварианты старого кода оставить явно historical до замены, без миграции старого state. Обновить требования «production only by issue» перед новой квалификацией. | | |
| TASK-003 | Независимый рецензент проверяет точный documentation HEAD, воспроизведения AUD-02/AUD-05 и границы native ownership; проверка не ограничивается чтением agent report. После review — только отдельное разрешение merge. | | |

### Implementation Phase 2

- GOAL-002: Получить проверенный контракт штатного XKeen. Зависит от GOAL-001.
- Приёмка: native installation работает без панели; вся матрица команд имеет доказанный profile/status, stdout/exit/postconditions известны. Выбран release channel на основании возможностей, а не названия dev.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | Добавить `internal/xkeen/capabilities.go` с read-only version/build/core/config/cron/modules/hooks discovery. Не требовать byte equality с panel ProductDefault. Сохранить обнаруженные неизвестные поля; возвращать per-capability supported/unsupported/unknown. | | |
| TASK-005 | Создать `docs/XKEEN-COMPATIBILITY.md`: проверить stable2.0 и beta artifact из `5aaece2…` в disposable Linux fixture и затем чистом Keenetic. Включить native install, штатный restart, нужные Hybrid/PBR/IPv6 функции, geodata, CLI update, config preservation. Выбрать stable только если все required capabilities проходят; иначе закрепить beta release profile. | | |
| TASK-006 | Для interactive commands `-i/-ux/-ugc/-channel` выписать точные prompts/EOF/exit поведение. Предпочтительный результат — upstream noninteractive options и structured status. До их появления разрешить только покрытый version-specific dialogue adapter; неожиданный prompt завершает job как unsupported/unknown, не отвечает `yes`. Если нет безопасного adapter, показать native operator step. Не дублировать downloader/installer в панели. | | |
| TASK-007 | Contract fixtures содержат реальные публичные архивы/манифесты и config shapes, а не только два искусственных файла. Проверить sizes, memory, stdout limits, native backup semantics и self-detach; lifecycle вызывать с `XKEEN_FOREGROUND=1`. | | |

### Implementation Phase 3

- GOAL-003: Минимальный вертикальный продукт «XKeen + панель рядом». Зависит от GOAL-002. Первый аппаратный milestone.
- Приёмка: штатный XKeen работает до установки панели, после добавления подписки и после её остановки; без замены native init/netfilter/schedule файлов.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-008 | Добавить `internal/xkeen/operations.go`: enum actions, fixed argv, очередь одного job, bounded private diagnostic sink, RAM admission и independent postconditions. Обычный lifecycle не получает отдельный persistent journal: retained same-boot unknown требует readback, а durable `.pending` остаётся у существующей config transaction (см. `docs/NATIVE-XKEEN.md`). В `internal/httpapi/server.go` экспортировать typed endpoints; query/body не содержат executable/path/shell. Не добавлять второй execution coordinator. | | |
| TASK-009 | Переподключить `nodes.CommandActivator.Restart` в `internal/nodes/transaction.go` к native foreground lifecycle через adapter; оставить validation и readback Xray API. `cmd/xkeen-control/main.go` больше не выбирает panel-owned S05 как нормальный путь. | Implemented/deployed: c805 full gate and panel hash readback; native-init command execution tested in fixtures, no new live restart claimed | 2026-10-02 |
| TASK-010 | Перед первым node Apply выполнить отдельный scoped onboarding: loopback Xray API, managed outbounds/balancer и необходимые routing/observatory references; сохранить native inbounds, interception и unrelated rules, validate full config → native restart → API/balancer readback. Переработать setup-flow.jsx в понятный attach flow с per-capability состояниями; не вызывать старый SetupService. | Done: scoped attachment and pool independently verified | 2026-10-02 |
| TASK-011 | На чистом Keenetic: native install → start → отдельно panel install → две операторские подписки через API → candidate validation → native restart → независимый LAN TCP/UDP/DNS/direct/proxy тест. Затем плановая остановка панели и повторный трафик. Ссылки/секреты не входят в отчёт. | Partial: install/panel/two subscriptions/one-node quality verified; LAN TCP/UDP/DNS still pending | 2026-10-02 |

### Implementation Phase 4

- GOAL-004: Управлять native lifecycle/update/cron без второго обновлятора. Зависит от GOAL-003.
- Приёмка: panel/native вызовы дают одинаковое итоговое native состояние; cron update и ручная операция не портят друг другу конфиг; неизвестный исход читается без replay.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-012 | Реализовать adapter действий `-uk/-ux/-ug`, channel и rollback только в границах доказанного native support. Если upstream не даёт полного atomic rollback, UI показывает verified native outcome/recovery instruction; не обещать прежнюю panel transaction гарантию. Параметры версии и источника только из discovered capability. | | |
| TASK-013 | Управлять native geodata cron, сохраняя unrelated jobs. Для CLI/cron/panel concurrency требуется общий admission seam до операций; приостановка cron и process scan не устраняют гонку с новым CLI. До появления seam разрешена только явно обозначенная exclusive operator maintenance, без заявления concurrency PASS. | | |
| TASK-014 | В `internal/components` удалить нормальные Xray/geodata/XKeen download/install/rollback writers и F3 replacement scheduler после переключения клиентов. Удалить legacy recovery вместе с его неиспользуемыми callers; оставить безопасный read-only inventory и recovery текущих операций. `web/src/components-updates.jsx` показывает native commands/jobs, а panel update остаётся прежним signed path. | | |
| TASK-015 | Удалить migration/adoption старых panel поколений, legacy journal readers и layout whitelists: оператор явно исключил обратную совместимость. Сохранить coherent rollback только операций новой установки. | | |

### Implementation Phase 5

- GOAL-005: Совместимые графические настройки и сохранение узлов. Зависит от GOAL-003; GOAL-004 нужен для concurrency acceptance.
- Приёмка: edit одного поля/набора rules сохраняет unrelated native settings; valid unknown sections не делают весь UI недоступным.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-016 | Добавить `internal/xkeen/config.go`: read/validate/update поддержанных полей xkeen.json, ports/IP lists и native режимов. Projection обнаруживает capability; семантически неизменные неизвестные поля сохраняются. Native mode transitions выполняются командами, не патчем init/hooks. | | |
| TASK-017 | Переработать `internal/appliance/service.go`, `custom_policy.go`, `render.go` и `internal/restore/restore.go`: убрать обязательную полную ProductDefault equivalence для editor. Snapshot active config digest → typed change managed region → validate complete candidate → atomic replace changed files → native restart → verify; при stale digest вернуть конфликт с новым diff. | | |
| TASK-018 | Сохранить `internal/nodes` parser/registry/reconciliation/batch; обеспечить coexistence unmanaged outbounds и coherent import после external CLI edit. Расширять протоколы только отдельным запросом. В `web/src/main.jsx` сохранить input URL без password mask и WL/RU/BY default tests; никогда не сбрасывать explicit enabled choice при refresh. | | |

### Implementation Phase 6

- GOAL-006: Доказуемый выбор узла и отказоустойчивость. Зависит от GOAL-003 и GOAL-005. Исследование модели выполняется offline параллельно GOAL-004.
- Приёмка: один writer; crash-panel/fail-node не оставляет бесконечный stale override; улучшение качества подтверждено сценариями и ограниченными live измерениями.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-019 | В `internal/c1`/`internal/xkeen` ввести явный mode native-xray/native-sb/panel-adaptive. Native `-sb off` используется при переходе в panel-adaptive; обратный переход снимает только panel override и запускает выбранный native mode. Сохранить ручной выбор пользователя. | | |
| TASK-020 | Создать replay corpus для RTT, download/upload, jitter/отказов, ограничений endpoint, startup/restart и starvation. Сравнить native leastPing, native SB и panel adaptive при одинаковых бюджетах. Для AUD-05 кейса после dwell и подтверждающих samples 10x при равных latency/loss должен давать material improvement; noisy ±10% не должен вызывать flapping. Формулу с log(BPS) заменить калиброванной безразмерной моделью только после этого сравнения. | | |
| TASK-021 | В `PrepareAdaptiveGeneration` добавить одну exploration позицию внутри текущего лимита пяти challengers; вести cursor в RAM по stable IDs, не увеличивать 144 MiB/180 s ceiling. После трёх независимых свежих RTT samples разрешать один bounded initial quality run, затем обычный cadence. Показывать planned traffic/day и экономичный preset. | | |
| TASK-022 | Спроектировать release override вне основного процесса: graceful stop clear + проверенный native/Entware supervision hook для crash/hang с heartbeat только в RAM. Он только снимает просроченный panel override, не выбирает узел и не становится вторым optimizer. Если upstream lifecycle не обеспечивает этот контракт, panel-adaptive по умолчанию не включать; безопасный режим native Xray. Проверить outage→recovery при kill процесса в disposable contour и на отдельном аппаратном этапе. | | |
| TASK-023 | UI Performance объясняет measured sample age, reason selected/not selected, effective/native target, health, mode и бюджет. Отличать «лучшая из проверенных» от «лучшая из всех». Ручной тест одного узла остаётся bounded; bulk sustained benchmark убрать из обычного пользовательского сценария. | | |

### Implementation Phase 7

- GOAL-007: Geodata browser и routing для пользователя. Зависит от GOAL-005. Может выполняться параллельно GOAL-006.
- Приёмка: categories и membership соответствуют **установленным** байтам, итоговые rules проходят Xray validation; обновление dat инвалидирует stale preview/index.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-024 | Добавить `internal/geodatareader/`: bounded streaming reader protobuf GeoSite/GeoIP установленного native inventory, ключ snapshot=(имя,размер,SHA256). Не декодировать весь payload в JSON/DOM. Плановые ceilings: 64 MiB/file, 64 MiB дополнительного RSS, 100 entries/page, 256 KiB/API response; отклонить или page-stream при превышении, проверить на реальных файлах. | | |
| TASK-025 | Реализовать category list/content paging и lookup домен/IP/CIDR с точным типом совпадения, IDNA, regex validation и geo attributes. CIDR показывает membership, domain показывает exact/suffix/keyword/regex; не резолвить все домены в IP. Изменение snapshot во время запроса возвращает stale, не смесь версий. | | |
| TASK-026 | В `web/src/routing-policy.jsx` добавить категории, поиск «есть ли адрес в базе?», свои именованные списки, выбор VPN/DIRECT/BLOCK и reorder. Preview показывает человеческий diff и победившее first-match правило, включая защищённые исключения/системные правила. DNS derivation меняет только нужные domains, не навязывает весь ProductDefault. | | |

### Implementation Phase 8

- GOAL-008: Быстрый backup и перенос. Зависит от GOAL-004/005; формат списков согласуется с GOAL-007.
- Приёмка: export A → fresh native install B → preview device mapping → restore → рабочие subscriptions/routing. Неверная passphrase/повреждение не меняют B.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-027 | В `internal/backup` добавить format v2 с native xkeen config/list policies, managed registry, editor/selection preferences, geodata source/category references. Реализовать reader/writer нового portable формата; старый v1 reader не требуется и Argon2id/XChaCha20 envelope; native `-kb/-xb` остаются локальным backup механизмом. Не паковать executable/dat bytes/PID/logs/auth/listener/kernel state. | | |
| TASK-028 | В `internal/restore` реализовать target mapping для LAN interface/native policy/core versions; показывать неподдержанное до Apply. Telegram credentials переносить только отдельной encrypted opt-in секцией; не включать по умолчанию. На неудаче восстановить предыдущий coherent config, native restart и readback. | | |

### Implementation Phase 9

- GOAL-009: Telegram с понятной моделью устройств. Зависит от GOAL-004/006/008.
- Приёмка: чужой user/chat, просроченный callback и повторный update не запускают mutation; router offline отображается как offline, не как command success.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-029 | Расширить `internal/notifications` событиями туннель lost/recovered, subscription refresh failure, selection change и native update failure с dedupe/rate-limit. Добавить `internal/bot` single-router long-poll receiver, allowlisted user+chat, fixed commands status/restart/switch/check-update и короткую confirmation кнопкой для изменения. Никаких shell, raw configs, VPN URLs или plaintext backups. | | |
| TASK-030 | Добавить command ID/TTL/durable terminal receipt; acknowledge Telegram update отдельно от verified router result. Queue использует тот же native operation owner. Перезапуск consumer не повторяет последнюю mutation. | | |
| TASK-031 | Для одного bot на несколько роутеров оформить отдельный optional controller: единственный Telegram receiver, зарегистрированные device IDs, outbound authenticated polling/канал от роутеров, per-device authorization и signed/expiring commands. Controller может жить на постоянно доступном operator host; его отсутствие не мешает VPN. Не разворачивать cloud и не раздавать один token независимым pollers. Это отдельный milestone после single-router, не prerequisite первой установки. | | |

### Implementation Phase 10

- GOAL-010: Финальная независимая ревизия, упрощение и выпуск. Зависит от GOAL-003..009 для полного scope; полезные меньшие milestones могут выпускаться отдельно с точным описанием ограничения.
- Приёмка: поддержанный clean install воспроизводится из пользовательской инструкции; standalone XKeen остаётся работоспособным без панели; нет второго component updater.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-032 | Удалить отключённые duplicate component writers, устаревший Setup UI/recovery UX и неподдержанные обещания из docs. Составить owner/call-path review, проверить CLI↔UI roundtrip и отсутствие ссылок на удалённые API. Старые migration/rollback readers удалить; текущий rollback проверить отдельно. | | |
| TASK-033 | Каждый code PR: focused fixtures, один final exact-HEAD `scripts/dev-check.ps1 -Full`, independent review, затем отдельный operator merge. Повтор полного gate только после изменения HEAD. Docs-only PR — ссылки/согласованность/diff check. | | |
| TASK-034 | Аппаратный gate: install/uninstall panel; clean restart и, только отдельно разрешённый, reboot; native update+cron collision; потеря связи; остановка panel; отказ node/Xray; invalid subscription; config rollback; router-to-router restore. LAN probes из отдельного клиента, ограниченный traffic budget. Зафиксировать фактическое время установки и действий, не обещать «быстро» по числу кнопок. | | |

## 3. Alternatives

- **ALT-001**: Продолжать panel-owned Setup и увеличивать compatibility whitelist. Отклонено: сохраняет две инфраструктурные реализации и повторяет AUD-01/07/09.
- **ALT-002**: Открыть shell/редактор любых файлов из UI. Отклонено: не даёт понятной модели и разрушает границы безопасности.
- **ALT-003**: Удалить весь текущий проект и писать заново. Отклонено: registry, auth, crypto backup, Xray API и UI уже полезны.
- **ALT-004**: Включить одновременно native SB и adaptive. Отклонено: общий override, гонки владельцев и неоднозначная диагностика.
- **ALT-005**: Сразу перейти на stable 2.0 только потому, что speed balancer не нужен. Решение отложено до TASK-005: остальные beta capabilities существенны.
- **ALT-006**: Обязательный облачный fleet backend с первого дня. Отклонено: новая инфраструктура не нужна для одной панели; multi-router controller остаётся optional.

## 4. Dependencies

- **DEP-001**: Точное upstream API/profile version и пригодный machine interface для interactive действий; отсутствие interface блокирует конкретную операцию, не оправдывает новый installer.
- **DEP-002**: Чистый Entware/Keenetic ARM64 для будущей qualification и отдельный LAN-клиент. Чистый ARM64 Entware и SSH подтверждены read-only preflight; аппаратная приёмка впереди. Второй роутер и независимый LAN-клиент пока не подтверждены.
- **DEP-003**: Native/core совместимость с gRPC RoutingService/Observatory; версия выбирается из contract matrix, не произвольный latest.
- **DEP-004**: Согласованный межпроцессный native lock для CLI/cron/panel; независимый override expiry/supervision контракт для autonomous adaptive.
- **DEP-005**: Single-router Telegram bot credentials, и отдельный постоянно доступный controller только если нужен единый bot на несколько устройств. На стадии плана credentials не запрашиваются.

## 5. Files

- **FILE-001**: `internal/components/{setup.go,setup_interception.go,xray.go,xkeen.go,geodata.go,policy.go}` — deprecated duplicate writers / historical recovery boundary.
- **FILE-002**: `internal/xkeen/{reader.go,capabilities.go,operations.go,config.go}` — native adapters; последние три файла новые.
- **FILE-003**: `internal/nodes/{transaction.go,operations.go,parser.go,refresher.go}` — сохранить authority/reconciliation, заменить lifecycle binding.
- **FILE-004**: `internal/appliance/{service.go,custom_policy.go,render.go}`, `internal/routingpolicy/service.go`, `internal/restore/restore.go` — scoped config edits и совместимость.
- **FILE-005**: `internal/c1/{adaptive.go,supervisor.go,coordinator.go,performance_policy.go}` — режимы, калибровка, exploration, failure independence.
- **FILE-006**: `internal/geodatareader/` — новый bounded reader/index API.
- **FILE-007**: `internal/backup/`, `internal/notifications/`, `internal/bot/` — перенос и Telegram, bot package новый.
- **FILE-008**: `cmd/xkeen-control/main.go`, `internal/httpapi/server.go`, `web/src/{setup-flow,components-updates,routing-policy,performance-policy,notifications,main}.jsx` — интеграция и пользовательские состояния.
- **FILE-009**: `docs/{ARCHITECTURE,CONTROL-PLANE,ROADMAP,OPERATIONS,FRESH-KEENETIC,XKEEN-COMPATIBILITY}.md`, `SECURITY.md`, `AGENTS.md` — согласованные контракты/инструкции; compatibility doc новый.

## 6. Testing

- **TEST-001**: Нативные artifacts обеих baseline версий, реальные file counts/sizes/config shapes, unknown version/capability, noninteractive EOF/extra prompt, self-detach, long operation и потерянный response.
- **TEST-002**: Сохранить offline journal counterexample как audit evidence; не требовать legacy recovery в новой clean-install реализации.
- **TEST-003**: Installer panel не пишет native executable/init/hooks/cron; штатный XKeen до/после удаления панели эквивалентен, за исключением явно применённых пользовательских config changes.
- **TEST-004**: Cron/manual-update/config-edit concurrency, неожиданный native exit, readback identity, unknown no-replay, сохранение unrelated cron и config.
- **TEST-005**: Подписки raw/base64 mixed, invalid supported profile, WL new/default/manual override/refresh, SSRF/redirect, batch atomicity и rollback.
- **TEST-006**: Deterministic quality replay: AUD-05, равные кандидаты, stale evidence, exploration fairness, dwell, liveness, endpoint outage, panel crash/hang, budget upper bound. Сравнение при равных ресурсах, не сравнение разных объёмов теста.
- **TEST-007**: Geodata corruption/oversize/unknown fields, suffix vs exact vs regex, IDNA, CIDR, dat generation change, pagination, first-match/DNS effects и target RSS.
- **TEST-008**: Restore A→B с другим LAN/device policy; current format, passphrase corruption, secrets exclusions, rollback и native runtime proof.
- **TEST-009**: Telegram unauthorized/replayed/stale messages, duplicate receiver prohibition, offline device, interrupted command, bounded alerts и отсутствие secret leaks.
- **TEST-010**: Полный local Linux gate и browser tests на финальном SHA каждого code PR. Hardware evidence отдельно от unit/source/reviewer evidence; «не запущено» никогда не превращать в PASS.

## 7. Risks & Assumptions

- **RISK-001**: У upstream нет стабильного machine API для всех команд. TASK-006 — технический gate; заранее обещать one-click полный install нельзя.
- **RISK-002**: Native updates могут иметь слабее rollback/trust guarantees, чем нынешние panel transactions. Принять native lifecycle означает явно документировать эту разницу и проверять фактический outcome, а не тайно поддерживать вторую реализацию.
- **RISK-003**: Сохранение всех неизвестных JSONC fields и native config variants сложно. Editor должен ограничивать область записи и проверять полный Xray candidate; при сомнении недоступна конкретная операция.
- **RISK-004**: Новая версия предназначена для чистой установки. Старые panel state/API не поддерживаются; это явно указано в инструкции, без скрытой миграции.
- **RISK-005**: Панель, держащая override, без независимого release механизма не обеспечивает автономный failover. До TEST-006 default — существующий native mode, не adaptive.
- **RISK-006**: Общий bot требует отдельного постоянно доступного владельца очереди, а offline router сам не сообщит о своей недоступности. Не обещать fleet monitoring без такого владельца.
- **ASSUMPTION-001**: Первый вертикальный slice ориентирован на Xray/ARM64. Mihomo/yq команды присутствуют в coverage matrix и подключаются только при соответствующем tested profile; их не удалять из требований незаметно.
- **ASSUMPTION-002**: Полное выполнение плана — несколько reviewable slices. Для полезной первой установки достаточно GOAL-001..003; геокаталог/fleet bot не должны её задерживать.

## 8. Related Specifications / Further Reading

- [Исходный аудит и доказательства](audit-xkeen-foundation-2026-10-02.md)
- [Полный инвентарь native dispatcher](xkeen-command-inventory-v1.md)
- [Текущая архитектура](../docs/ARCHITECTURE.md)
- [Roadmap](../docs/ROADMAP.md)
- [Upstream XKeen, точный snapshot](https://github.com/jameszeroX/XKeen/tree/5aaece27a70d5bd002c615248614914ebbc4569d)
- [Xray balancer override](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/router/balancing.go)
- [Telegram Bot API](https://core.telegram.org/bots/api#getupdates)
