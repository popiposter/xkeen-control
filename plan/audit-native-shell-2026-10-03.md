> Historical implementation/audit record. Current behavior and remaining work are in [plan index](README.md) and [ROADMAP](../docs/ROADMAP.md). Chronological checkpoints below are not current runtime state or permission to replay operations.

# Аудит: возврат к лёгкой оболочке XKeen

Дата: 2026-10-03. Авторитетное решение оператора: штатный XKeen — основа;
панель вызывает его команды и визуально редактирует читаемые им конфиги.
Повторное уточнение оператора: код самого XKeen не меняем вообще; исключения
для локальных исправлений init/hooks/dispatcher также не допускаются.
Переменные внутри `S05xkeen` и других scripts не становятся редактируемыми
конфигами: используем native команду либо показываем только для чтения.
Аудит исходников и документов; аппаратных операций и новых тестов не выполнялось.

## Проверенные основания

- Панель: HEAD `5fa05ad2a3eca08f66d4a39f32b9ef1dd8ddf47e`, Draft PR122,
  remote main `8adff1e00e89515b37aac1d7d6e7e1df924a9143`; 90 коммитов между ними.
- Незакоммиченный эксперимент: 45 строк в `scripts/native-update-context.sh`.
  Он заморожен и не является частью нового решения.
- Общий diff ветки: 250 файлов, 22403 добавления / 2869 удаления. Это не оценка
  всего diff как ненужного: auth, nodes, shadcn и оптимизация тестов сохраняются.
- Группа `scripts/native-{operation,admission,event,update}*`: 52 файла,
  19 не-тестовых файлов (включая builders/metadata), около 4068 строк и
  33 тестовых файла / около 4767 строк в измеренном рабочем дереве.
- Новый native протокол и десять overlays не установлены. Runtime создаёт
  обычный `authority.NewLease()` в `cmd/xkeen-control/main.go`, а не nativegate.
  Историческая установка development c805 не является свежей аппаратной проверкой.
- Последний FULL5fa прошёл; он подтверждает проверки исходников, не оправдывает
  архитектуру и не подтверждает работу нового обновлятора на роутере.
- Upstream API на дату аудита: единственная ветка `main`, HEAD
  `68eca60fedf03957952d1f8b44bfdd98f8a282e5`. Название Dev относится к каналу;
  не требовать несуществующую Git-ветку. Старые 69-командный inventory и fixtures
  привязаны к `5aaece27a70d5bd002c615248614914ebbc4569d`, не к текущей установке.

## Findings и решения

| ID | Наблюдение в коде | Последствие | Решение |
|---|---|---|---|
| AUD-S01 | `native-update-context.sh`, `native-admission-patch.mjs`, `native-update-profile.mjs` связывают ancestry/phase/exec/receipts, полную 72-файловую генерацию и десять patches | Панель сопровождает внутренний updater XKeen; каждая штатная правка upstream требует пересборки доказательств | Удалить всю недоставленную обвязку, не завершать terminal/runtime/cleanup стадии |
| AUD-S02 | `internal/authority/nativegate/` и native adapters/context/node-intent добавляют второй режим lease; обычный main его не включает | Большой объём тестирования ещё не приносит пользовательской функции | Оставить один внутренний panel lease/job owner; удалить неподключённый native режим |
| AUD-S03 | `internal/xkeen/lifecycle.go` трактует 75–77 как наш протокол и injects admission environment; `capabilities.go` требует marker в dispatcher | Штатный XKeen становится зависим от наших patches для управления/распознавания | Фиксированные native команды, документированный foreground только там, где он поддержан, наблюдение результата |
| AUD-S04 | Main не передаёт HTTP `Policy`, `DNSObservatory`, `Selection`; существующие DNS/routing services требуют `ActiveSnapshot`/managed appliance | UI недоступен; простое подключение старых services вновь навязывает appliance twin | Native file editors вместо полного ProductDefault; новые адаптеры поверх сохранённых UI/parsers |
| AUD-S05 | `newXrayService`, `newGeodataService`, `newXKeenService`, `newSetupService` и legacy component APIs остаются в source | Две реализации install/update/cron сохраняют сложность | Удалить старых component writers и Setup callers после перевода действий на native; panel signed updater сохранить |
| AUD-S06 | `attachment.go` добавляет API, routing и leastPing; adaptive использует runtime override | Автоподключение редактора способно менять native policy; override при crash не доказанно исчезает | Явная опциональная настройка managed pool; native selection по умолчанию, adaptive отдельный поздний этап |
| AUD-S07 | Issue121 Decision4 / v1 plan требуют общий admission для CLI/cron | Требование стало основанием для invasive patches | Снять обещание сериализации внешнего CLI/cron. Внутренний lease управляет только панелью; конфликт видимых baseline запрещает overwrite |
| AUD-S08 | Количество fixtures растёт вместе с удаляемым протоколом; FULL выполнялся и для промежуточных checkpoint | Много времени уходит на доказательство частей без пользовательского результата | Удалить тесты удалённых контрактов; focused change lanes, одна FULL для законченного milestone |

## Команды вместо своей реализации

Источник: [актуальный закреплённый dispatcher](https://github.com/jameszeroX/XKeen/blob/68eca60fedf03957952d1f8b44bfdd98f8a282e5/scripts/xkeen)
и предыдущая полная публичная сборка. Не обещать API по наличию флага:
конкретные аргументы/диалог проверить на выбранном installed version.

| Функция UI | Native действие | Граница панели |
|---|---|---|
| Start / Stop / Restart | `-start`, `-stop`, `-restart` | Один bounded job; затем service/process readback |
| Обновить XKeen / Xray / geodata | `-uk`, `-ux`, `-ug` | Штатная загрузка, backup и cron; панель не извлекает/устанавливает компоненты |
| Расписание geodata | `-ugc`; `-dgc` | Терминал выбранной native команды; чтение итогового native cron, не свой scheduler |
| DNS interception / Entware proxy / PBR / killswitch | `-dns on/off`, `-pr on/off`, `-pbr on/off/status`, `-killswitch on/off/status` | Явные режимы и изменение native настройки, без собственного netfilter writer |
| Native speed balancer | `-sb` и native settings | Проверить конкретные действия; не вызывать `-sbt` как обычный тест скорости |
| Локальные backups | `-kb`, `-xb`, соответствующие restore | Штатный локальный механизм; portable export — отдельные конфиги/секреты |
| Поля Xray / XKeen / списки | Редактор реально используемых конфигов | Сохранять неизвестное; validate complete candidate, scoped save |

`-ugc` в проверенном старом profile интерактивен: действие, день, час, минута.
Следовательно, передать только флаг недостаточно. По дополнительному указанию
оператора показываем терминал выбранной native команды: пользователь отвечает
самому XKeen. Это заменяет первоначальное предложение фиксированного dialogue
adapter, не создаёт generic shell/expect engine или свой cron writer. Полная
[матрица команд](xkeen-command-inventory-v2.md) определяет console/button/form
и проверку результата, включая фактический cron.
Upstream сейчас также предоставляет `-i auto` и `-ux auto`; свежую установку
проводить ими по отдельной задаче, не повторять уже выполненный installer.

## Конфигурация и конкуренция

Панель сериализует собственные jobs/config saves/subscription refresh. Внешний
native CLI и cron не пользуются её lock. Hash до/после обнаруживает часть конфликтов,
но не доказывает отсутствия гонки. Нормальный сценарий — последовательное управление
через панель; не редактировать те же файлы вручную одновременно с Save.
Это эксплуатационное ограничение, не новый блокирующий Setup/admission ritual.

При видимом external drift: перечитать и показать diff; не перетирать чужие файлы,
не выполнять автоматический повтор. Собственный rollback допустим только если
затронутые файлы всё ещё принадлежат нашей неудачной записи. После unknown — readback.
Если понадобится гарантия одновременных внешних writers, использовать только
поддержанный upstream lock, отдельным решением, без patch framework панели.

## DNS, политика Keenetic и доступность

Upstream [README](https://github.com/jameszeroX/XKeen/blob/68eca60fedf03957952d1f8b44bfdd98f8a282e5/README.md)
описывает и выбранных клиентов в политике, и режим без политики для всех клиентов.
Следовательно, обязательное создание политики именно с именем XKeen не является
универсальным требованием панели. Нужны настройки текущего native режима и отдельный
LAN-тест; текущую проблему с DNS нельзя считать диагностированной по source audit.

[Xray DNS](https://xtls.github.io/en/config/dns.html) поддерживает выбор resolver
по `domains`/geosite и маршрутизацию DNS-запросов. Это позволяет сделать domain-based
split DNS. IP/CIDR правила после резолвинга не задают однозначно DNS-маршрут до
резолвинга: не обещать автоматическую точную копию всех routing rules в DNS.
Перехват клиентского DNS на Keenetic и обработка DNS внутри Xray — разные настройки.
DIRECT интернет/DNS при остановленном XKeen и работа proxy при отказе узла требуют
проверки на отдельном LAN-клиенте без Karing. Автоматически менять router firewall
или включать killswitch ради «защиты» панель не должна.

## Что сохраняем

Auth/CSRF/private listener, signed updater панели, SSRF/bounded subscription parser,
registry/reconciliation/WL defaults, native outbound preservation, Xray validation,
небольшой rollback собственных config changes, crypto backup, Xray observations,
shadcn standard UI, Docker и ускоренные test lanes. Полезные tests этих функций не
удаляются вместе с native protocol. Сравнение native SB и нашей quality model остаётся
в плане, но не блокирует native controls и редакторы.

## Итог ревью

Независимый architecture reviewer проверил exact5fa/main8ad и подтвердил группы
удаления, отсутствие включённого nativegate, missing HTTP editor wiring и конфликт
старого контракта с новым решением. Его audit не является аппаратной проверкой.
Исполнимый порядок: [v2 plan](architecture-native-shell-v2.md).
