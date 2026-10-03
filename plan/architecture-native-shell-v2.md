---
goal: Лёгкая графическая оболочка над штатным XKeen и его конфигурацией
version: 2
date_created: 2026-10-03
last_updated: 2026-10-03
owner: popiposter/xkeen-control
status: In progress
tags: [architecture, refactor, native-xkeen, simplification]
---

# Introduction

![Status: In progress](https://img.shields.io/badge/status-In%20progress-blue)

Заменяет `architecture-xkeen-foundation-v1.md` и native admission plan.
Основание — [source audit](audit-native-shell-2026-10-03.md) exact5fa / main8ad.
Цель: штатный XKeen работает самостоятельно; панель быстро вызывает его команды
и помогает редактировать его конфигурацию. Оператор разрешил реализацию v2 после
аудита. Phase1 удалена в исходниках; typed jobs/API и лениво загружаемая консоль
реализованы, но пока не подключены в main. Перед включением остаются interrupted-job
receipt, idle timeout, installed capabilities и independent readback. Live приёмка
команд и редакторов ещё не выполнена.

## 1. Requirements & Constraints

- **REQ-001**: XKeen владеет install/update/dependencies/service/hooks/native cron.
  Панель не декорирует, не пересобирает и не заменяет его internal modules.
  Код XKeen не меняется вообще: dispatcher/init/hooks/module scripts — штатные.
  Даже воспроизводимая ошибка требует upstream report/fix, не локального patch.
  Штатная команда может сама менять свои файлы; панель лишь вызывает её.
  Embedded settings в `S05xkeen`/других scripts — код, не config; менять только
  native командой, а без такой команды поле read-only. Editable paths — только
  data-only JSON/JSONC/native list files, не sourced shell files.
- **REQ-002**: Native command adapter использует fixed executable/argv, типизированные
  поля и [полную command matrix](xkeen-command-inventory-v2.md). Интерактивный native
  процесс получает command-bound PTY: пользователь отвечает самому XKeen. Вопросы
  не дублируются в React/expect; parameterized actions — формы/кнопки с output viewer.
  Нет shell string, произвольного executable/generic PTY/file API.
- **REQ-003**: Xray/XKeen active config — authority. Редактор сохраняет неизвестные
  поля и нетронутые области; `appliance.json` не обязательный двойник native config.
- **REQ-004**: `nodes.json` остаётся authority только panel-managed profiles/subscriptions.
  Нативные unmanaged outbounds сохраняются; opening editor не меняет конфигурацию.
- **REQ-005**: Native leastPing/SB/manual режимы видимы и сохраняются; один selection
  writer в выбранном режиме. Panel adaptive опционален и не блокирует остальную UI.
- **REQ-006**: DNS/geodata/routing/balancing — редакторы config, native setting/commands,
  а не собственные firewall/updater/cron engines. Telegram/portable transfer остаются
  функциональными этапами после работающей базовой оболочки.
- **SEC-001**: Auth/CSRF/private management, SSRF, signed panel updater и root-only
  secrets сохраняются. Status API и public evidence не выводят raw configs/native
  stdout/secrets. Явная authenticated private console может показывать native output
  и принимать ответы: bounded RAM, job/session ownership, no public log/export.
- **CON-001**: Один обычный checkout, текущий Draft PR122; no worktrees/self-merge.
  Старые installer/attachment/import/recovery/unknown Apply не повторять.
- **CON-002**: Нет compatibility с прежними панелями, общего native admission protocol,
  полной native profile/BOM verification, второго component updater/NDM daemon.
- **CON-003**: Panel lease сериализует только panel actions. External CLI/cron race
  exclusion не обещается. Save не совмещать с внешней записью тех же файлов; visible
  drift запрещает overwrite/автоповтор. Read-only UI не блокируется этим ограничением.
- **CON-004**: Jobs не зависят от открытой вкладки. Для mutation достаточно одного
  малого status receipt job/action/start/result; crash означает unknown/readback,
  не перезапуск команды. Нет journal по каждой внутренней фазе XKeen.
- **CON-005**: Karing остаётся на PC; LAN acceptance — другой клиент. No reboot,
  blanket opkg upgrade, credential rotation или sustained benchmark.
- **GUD-001**: Source hash фиксируется в evidence/tests, но неизменность 72-файловой
  native сборки не является prerequisite всех GUI actions. Неподдержанная команда
  отключает только свою кнопку с причиной, не всю панель.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Удалить недоставленную обвязку; native install не требует panel patches.
- Зависимости: нет. Приёмка: netfilter/init/cron не изменены, panel nodes/auth/update
  сохраняются; отсутствуют protocol dependencies и старые обязательные gates.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Сохранить локальный diff и удалить только замороженную 45-line runtime draft в `scripts/native-update-context.sh`. Удалить `scripts/native-operation-gate*`, `native-admission-*`, `native-event-*`, `native-update-*` и соответствующие fixture catalog entries `test-native-admission.sh`/`test-native-upstream.sh`. Удалить disabled-profile builder из packaging/check selectors; публичные ignored caches не являются поставкой. Перед удалением проверить imports/callers через rg. | Yes | 2026-10-03 |
| TASK-002 | Удалить `internal/authority/nativegate/`, native adapters/context modes и node-intent bindings/callers. Сохранить ordinary `authority.Lease`, текущий `.pending` и scoped node/config rollback. В `internal/xkeen/lifecycle.go`, `internal/nodes` убрать admission environment/75–77 semantics; оставить timeout/unknown/independent readback. | Yes | 2026-10-03 |
| TASK-003 | В `internal/xkeen/capabilities.go` убрать обязательный marker patched dispatcher (source done); capability определяется installed features. Обновить `docs/DEVELOPMENT.md` и selectors/tests для удалённых файлов, сохранив независимые Docker/embedded/audit и auth/node/security tests. Не устанавливать old native stop patch; воспроизводимую upstream ошибку оформить отдельно. Перед новым live milestone проверить, не осталось ли ранее установленных локальных native patches; если осталось — восстановить штатную сборку официальной native процедурой как новую отдельную операцию с сохранением конфигов, не replay прежнего installer. | No | — |

### Implementation Phase 2

- GOAL-002: Рабочая вертикаль штатных команд, включая native расписание geodata.
- Зависимость: Phase1. Приёмка: CLI и UI дают одинаковые native settings/cron;
  panel stop не меняет native работу. Не требуется сначала охватить все 69 команд.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | Добавить `internal/xkeen/commands.go`/`jobs.go`: allowlist operation→argv, один panel mutation job, bounded sanitized facts; query10s/service90s/update15min ceilings и interactive idle10min с measured refinement. Private RAM output256KiB/job, bounded input/resize, job/session ownership/auth/origin/CSRF; public output только facts. Interactive/conditional job запускает выбранный XKeen в PTY с начала; после exit shell не остаётся. Deadline/disconnect не запускает повтор; после crash/cancel unknown/readback. Проверить native foreground/TTY/service-children; native код не патчить. | No | — |
| TASK-005 | Первая вертикаль по полной matrix: lifecycle, `-uk`, `-ux auto`, `-ug`, `-ugc`, `-dgc`, `-dns`, `-pr`, `-pbr`, `-killswitch`, `-sb`, `-kb`, `-xb`. Проверить installed argv/capabilities в focused native fixtures. Для cron `-ugc/-dgc` показывать native вопросы в терминале, оператор отвечает; не писать собственную prompt state machine/expect. Проверить actual cron после выполнения. `-sb on` может спросить prerequisites и запускает первый замер; учитывать traffic budget. Internal callbacks/`-sbt` не имеют прямого API. | No | — |
| TASK-006 | Typed native actions/jobs в `internal/httpapi/server.go`; native cards + lazy `@xterm/xterm`/`@xterm/addon-fit` console, Go PTY adapter без Node на router. Noninteractive output read-only; interactive input связан с тем же job. Reconnect не respawn; закрытие вкладки detach, Ctrl-C explicit cancel. Disable OSC clipboard/external links/HTML, очистка UI при logout. Native download/backup принадлежит XKeen. Unknown result показывать понятно; exit0 не означает туннель PASS. Signed panel updater отдельно. | No | — |
| TASK-007 | Удалить из `cmd/xkeen-control/main.go` factories `newXrayService/newGeodataService/newXKeenService/newSetupService` и obsolete component/Setup APIs/UI/tests/callers, когда native cards подключены. В `internal/components` оставить только реально нужные signed panel/bootstrap/read-only dependencies; не удалить весь пакет вслепую. Убрать old migration/recovery/appliance activation CLI, которые не нужны new-generation config edits. | No | — |

### Implementation Phase 3

- GOAL-003: Единый простой механизм сохранения native configs и работающие редакторы.
- Зависимость: Phase2. Приёмка: native CLI config edit виден в GUI; неизвестные
  поля/неизменные файлы сохраняются; invalid candidate не активируется.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-008 | Добавить `internal/xkeen/config.go`: bounded JSONC read/projection и edits фиксированных native paths; snapshot hashes затронутых файлов, complete candidate Xray validation, final baseline check, atomic replacement. Multi-file change использует один существующий маленький current-operation transaction/backup, не native journal. Drift/unknown останавливает действия; rollback не перезаписывает чужие изменения. Не переписывать comments/opaque regions без нужды; семантически сохранить unknown fields. | No | — |
| TASK-009 | Перевести `internal/routingpolicy/service.go`, `internal/dnsobservatory/service.go`, `internal/restore` с managed appliance twin на native snapshot/edit adapter; подключить providers в main HTTP config. `internal/appliance` сохранить только используемые parsing/validation utilities, убрать ProductDefault equivalence/adoption gates. Типизированные панели: XKeen settings/ports/IP lists, DNS, routing, observatory/balancers. | No | — |
| TASK-010 | Сохранить `internal/nodes` import/batch/reconcile/native renderer, WL defaults и unmasked URL field. По source/runtime проверить 2subscriptions и profiles без повторного импорта. Явный onboarding managed pool/API только если оператор включает соответствующую функцию; просмотр редактора не создаёт routing/API/balancer. Auto refresh использует panel job owner, без global native gate prerequisites. | No | — |

### Implementation Phase 4

- GOAL-004: DNS и обычный интернет понятны и проверены при сбоях.
- Зависимость: Phase3. Приёмка: отдельно измерены DIRECT DNS/HTTPS при stopped
  XKeen и при proxy node failure; policy behavior подтверждён LAN-клиентом.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-011 | В native settings UI показать actual mode, client policy, DNS interception, PBR и killswitch через поддержанные команды/config. Политика с именем XKeen не обязательный install gate; для конкретного native режима объяснить выбор клиентов/всех клиентов. Панель сама не пишет Keenetic firewall или меняет WAN settings. | No | — |
| TASK-012 | DNS editor: direct resolver/default для обычных имён; proxy resolver по domain/geosite match, transport route/bootstrapping и fallback явно. IP-only routing нельзя автоматически превратить в DNS domain match. Сохранять существующий user config, не включать global DNS interception/killswitch молча. Resolver failure и circular bootstrap имеют fixtures. | No | — |
| TASK-013 | После ограниченного private snapshot выполнить native stop/start и node failure/recovery на отдельном LAN-клиенте; проверить DNS+HTTPS DIRECT/proxy, native cron/service и panel-off independence. PC Karing не источник LAN proof. Если клиент отсутствует, аппаратный результат NOT RUN; source/editor work не останавливать. Сохранить bounded observations, не unbounded logs. | No | — |

### Implementation Phase 5

- GOAL-005: Наглядный routing и установленная geodata.
- Зависимость: Phase3; Phase4 не блокирует source UI work.
- Приёмка: category membership соответствует installed bytes; правила дают валидный
  Xray config, сохраняют first-match order и пользовательские unknown rules.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-014 | Добавить `internal/geodatareader/`: installed file inventory, lazy bounded protobuf category/content/search, file snapshot по size/hash с invalidation после native update. Начальные limits64MiB/file,64MiB additional RSS,100entries/page,256KiB response; проверить target footprint. Не persistent whole-dat JSON index/DOM. | No | — |
| TASK-015 | `web/src/routing-policy.jsx`: categories, domain/IP/CIDR membership search с match type, свои lists, VPN/DIRECT/BLOCK, reorder и пример победившего правила. DNS bindings для domain lists по желанию, без full policy generator. Standard shadcn UI, lazy-load тяжёлых редакторов. | No | — |

### Implementation Phase 6

- GOAL-006: Нативная балансировка удобна; panel quality добавляется только за
  измеренную пользу. Зависимость: Phase3/4 для live comparison.
- Приёмка: конфиг balancing редактируется без panel daemon; второй writer не активен.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-016 | UI native leastPing/SB/ручной mode, pool/observatory/burst settings; команды `-sb` для supported native control. Native остаётся default. Сравнить существующие `internal/c1/adaptive.go` calibration/exploration fixes с native SB на одинаковом малом бюджете, не full sustained benchmark. | No | — |
| TASK-017 | Если quality advantage подтверждён, отдельный milestone panel-adaptive: native SB выключается его командой, override имеет независимое expiry/crash release. Без такой квалификации показывать bounded one-node quality diagnostics, adaptive не включать. Не строить gate/event infrastructure для этого; не вводить новый background supervisor без отдельного обоснования. | No | — |

### Implementation Phase 7

- GOAL-007: Быстрый backup/перенос и Telegram. Зависимость: Phase3; не ждать
  panel-adaptive. Приёмка portable A→B и Telegram command auth/dedupe отдельно.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-018 | `internal/backup`, `internal/restore`: reuse encrypted envelope; native configs/custom lists/registry/preferences и native schedule description. Local backup через `-kb/-xb`; portable restore после обычной native установки, mapping интерфейсов/политик и preview. Не переносить executable/kernel/PID/auth/listener. Schedule восстановить native command, не cron engine. Второй router live test — отдельная доступность. | No | — |
| TASK-019 | `internal/notifications`/новый `internal/bot`: reuse alerts, один long-poll receiver на router, allowlisted user/chat, короткие typed commands/status/restart/update/profile refresh и bounded dedupe/TTL. Native actions используют тот же panel jobs adapter. Не shell/raw configs/secrets. Optional fleet controller — последующий отдельный проект/milestone, не prerequisite одного router. До live test нужен private token. | No | — |

### Implementation Phase 8

- GOAL-008: Простота измеряется работающими сценариями. Каждый code milestone
  выпускается/проверяется отдельно; не ждать всего Phase1–7 ради первого native GUI.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-020 | Тесты удалённых протоколов удалить вместе с кодом. Focused parser/command/editor/auth/rollback cases и затронутые browser specs в итерации. Одна exact-clean-HEAD FULL по завершённому milestone перед поставкой, independent review отдельно. Docs-only audit — links/content/diff check, без149 browser. Build checks не зависят от native public caches. | No | — |
| TASK-021 | Измерить dashboard/action/config-save time и ARM64 RSS; initial targets warm dashboardAPI≤1s, first screen≤2s LAN, application-only RSS≤64MiB idle. Native downloads/test latency измерять отдельно, не маскировать. Записать baseline и исправлять только фактический bottleneck; UI standard shadcn без собственного theme system. | No | — |
| TASK-022 | Финальный checklist: native CLI↔GUI equivalence, native update сохраняет доступность панели, панель удалена/остановлена — XKeen работает, config invalid/drift отказ, DIRECT/proxy/DNS failure tests, подписки/WL, native selection. Signed panel release остаётся protected workflow; no merge/release/install без соответствующей задачи/авторизации. | No | — |

## 3. Alternatives

- **ALT-001**: Завершить 10-overlays/native admission. Отклонено: конфликт с
  пользовательским контрактом, стоимость сопровождения внутренних native writes.
- **ALT-002**: Панель заменяет installer/cron/firewall. Отклонено: дублирование XKeen.
- **ALT-003**: Удалить весь проект. Отклонено: полезные auth/nodes/UI/updater сохраняются.
- **ALT-004**: Произвольный shell/raw editor как быстрый выход. Отклонено: нет понятной
  модели, разрушает typed/security boundary. Command-bound native console достаточна.

## 4. Dependencies

- **DEP-001**: Установленный штатный XKeen и его version-specific command contract;
  текущий upstream main не считается автоматически установленным/проверенным.
- **DEP-002**: Docker/Linux toolchain, existing auth/lease/config transaction/shadcn.
- **DEP-003**: Отдельный LAN-клиент для failover/DNS proof, другой router для portable
  live restore, private bot token для Telegram; missing hardware не подменять fixtures.

## 5. Files

- **FILE-001**: `scripts/native-*`, `scripts/test-native-*`, native gate adapters/tests:
  конкретные deletion groups в TASK-001/002; unrelated tooling сохраняется.
- **FILE-002**: `internal/xkeen/{capabilities,lifecycle,commands,jobs,config}.go`,
  `internal/authority`, `cmd/xkeen-control/main.go`, `internal/httpapi/server.go`.
- **FILE-003**: `internal/nodes`, `internal/components`, `internal/appliance`,
  `internal/routingpolicy`, `internal/dnsobservatory`, `internal/c1`.
- **FILE-004**: `web/src/{native-xkeen,components-updates,routing-policy,dns-observatory,performance-policy}.jsx`,
  `internal/geodatareader`, `internal/backup`, `internal/restore`, `internal/notifications`.
- **FILE-005**: `docs/NATIVE-XKEEN.md`, `docs/ROADMAP.md`, `docs/DEVELOPMENT.md`,
  affected historical authorities и active Issue121. V1 plans только исторические.

## 6. Testing

- **TEST-001**: No native patch installation; native command exact argv/answers,
  output/input limits, expected response/state, timeout/crash unknown without replay;
  interactive input/reconnect/job ownership, read-only output and terminal controls.
- **TEST-002**: JSONC/unknown fields, unsupported field read-only, complete candidate
  Xray validation, external drift refusal и bounded own-write rollback.
- **TEST-003**: Реальные auth/SSRF/signatures/secretless projections, nodes/subscriptions
  unsupported protocols ignored, explicit user enable overrides retained, WL default.
- **TEST-004**: Page-focused shadcn interactions/accessibility и real native CLI↔GUI
  state comparison; byte-exact full vendor profile tests не нужны thin adapter.
- **TEST-005**: LAN DNS/direct/proxy and panel-off, scoped native update readback;
  native outcome не доказывается просто HTTP202/exit0/process present.

## 7. Risks & Assumptions

- **RISK-001**: Native interactive prompts могут измениться; native console показывает
  реальные вопросы вместо их копии. Неподдержанный argv отключает только операцию;
  upstream не патчится ради совместимости.
- **RISK-002**: External CLI/cron может менять те же файлы. Panel serialization не
  даёт global exclusion; отсутствие параллельных same-file writes — ограничение.
- **RISK-003**: Нативная команда имеет side effects/самовосстановление; dashboard
  не запускает её скрытно. Mutations выполняются как заявленные native actions.
- **RISK-004**: Удаление 52-файловой группы не гарантирует размер binary reduction:
  многие файлы никогда не поставлялись. Цель — убрать обязанности и tests/tooling.
- **ASSUMPTION-001**: Панель используется одним оператором в private LAN; полная
  транзакционность всех внешних CLI/cron и backwards compatibility не требуются.
- **ASSUMPTION-002**: В этом задании меняются план/контракт, не live configuration.

## 8. Related Specifications / Further Reading

- [Контракт](../docs/NATIVE-XKEEN.md), [аудит](audit-native-shell-2026-10-03.md),
  [sequencing](../docs/ROADMAP.md), [Issue121](https://github.com/popiposter/xkeen-control/issues/121).
- [Pinned upstream command source](https://github.com/jameszeroX/XKeen/blob/68eca60fedf03957952d1f8b44bfdd98f8a282e5/scripts/xkeen),
  [native configuration guide](https://github.com/jameszeroX/XKeen/wiki/Configuration),
  [Xray DNS](https://xtls.github.io/en/config/dns.html), [routing](https://xtls.github.io/en/config/routing.html).
