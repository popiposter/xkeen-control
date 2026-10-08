# Все команды XKeen: матрица использования в панели

Дата: 2026-10-07. Проверенный upstream: **2.1 Stable**,
[`c4b0fb5b956b4f70bf1b8e4a2e0eec452d61ebd1`](https://github.com/jameszeroX/XKeen/blob/c4b0fb5b956b4f70bf1b8e4a2e0eec452d61ebd1/scripts/xkeen).
Dispatcher SHA256 `66a00579938cbb9ff9fa4f595636481f3aa528380af8e79f6861b811998c434c`;
bytes release archive сопоставлены с pinned source (детали в
[переходе на 2.1](../docs/XKEEN-2.1-TRANSITION.md)).
Источники: dispatcher, [справочник](https://github.com/jameszeroX/XKeen/blob/c4b0fb5b956b4f70bf1b8e4a2e0eec452d61ebd1/docs/commands.md),
native choice/cron/ports/delay/backup/diagnostic и SB control modules того же commit.

**69 верхнеуровневых ветвей основного dispatcher, 74 написания флагов с aliases.**
Предварительные 70 совпадений включали startup classifier `-restart|-start|-stop`
до основного цикла; эта строка не является дополнительной командой.
Матрица — source-backed план, не проверка installed router и не запуск команд.

## Дельта 2.1

От предыдущего pin `68eca60fedf03957952d1f8b44bfdd98f8a282e5` — девять commits;
набор флагов не изменился. Обычный `-uk` теперь запрашивает подтверждение;
отказ и отсутствие обновления завершаются с кодом 0. Панель оставляет PTY,
не передаёт новый `-uk auto` и отдельно читает release identity до/после.
`-uk auto` существует только в новом upstream; это не поддержанный параметр
panel API и не способ перехода со старого dispatcher.

Beta/Dev `-uk` выбирает dev archive: для Stable нужен отдельный штатный
`-channel` и readback, затем `-uk`. Geodata menus используют `choice_menu`;
EOF означает отказ. Embedded `udp_flush` удалён: native TProxy/Hybrid hook
очищает соответствующий UDP conntrack без переключателя. Патчи native scripts
для возврата прежнего поведения запрещены. RCI token, killswitch, DSCP61,
`xkeen_full`, provider routing и download verification уже были в предыдущем pin.

## Представление

- **Кнопка**: native action не требует ответов при указанных параметрах. По умолчанию
  показываем progress/result, «Консольный вывод» раскрывает настоящий output.
- **Форма**: пользователь вводит конкретный параметр; панель валидирует и передаёт
  отдельным argv. Если предусмотрен explicit параметр, не открываем native menu.
- **Терминал**: команда требует выбора/подтверждения/ответов. Показываем live native
  output и отправляем клавиши/ответы тому же процессу XKeen через ограниченный PTY.
  Не воспроизводим вопросы upstream в React и не пишем универсальный expect engine.
- **Условно**: native действие обычно без вопросов, но prerequisite/выбор может
  вызвать dialogue. Для такого job PTY доступен сразу; user может открыть консоль.
- **Штатно**: внутренний native callback/modifier/cron tick; самостоятельной кнопки нет.

Нужность: **основная** — первая рабочая оболочка/редакторы; **расширенная** — native
инструменты обслуживания; **Mihomo** — только при соответствующем installed core,
не prerequisite Xray; **служебная** — только native implementation; **информация**.
Native output доступен для всех запущенных из UI jobs; поле ввода включается только
для интерактивного job. Closing page не отменяет job; reconnect присоединяется к нему,
не запускает команду снова. После завершения процесс не превращается в shell.

## 1. Установка и обслуживание

| Команда / параметры | Нужность | UI и ввод | Процесс использования / итог |
|---|---|---|---|
| `-i`, `-install`; `-i auto` | Расширенная | Терминал для обычной установки; форма + кнопка для `auto` | Только fresh bootstrap отдельного router. В auto доступны `cores=xray/mihomo/all/none`, `geo=on/off`, `geoipset=on/off`, `cron=on/off`, `autostart=on/off`, `xray=vX.Y.Z`, `mihomo=vX.Y.Z`. XKeen сам устанавливает; прежнюю установку не replay |
| `-io` | Расширенная | Терминал: offline prerequisites/настройка | Offline native install. Нужны заранее доставленные official files; панель не становится installer/file manager |
| `-uk`; upstream `-uk auto` | Основная | Терминал: подтверждение обновления, затем native dialogue; панель запускает обычный `-uk` | Native script update в текущем канале; для перехода Beta/Dev → Stable сначала `-channel`. `auto` не передаётся панелью. После — independent version/build/process/config observations; exit0 допускает отказ/no-op |
| `-ux`, `-ux auto`, `-ux vX.Y.Z` | Основная | Кнопка «Последняя» с `auto`; форма версии; терминал для `-ux` без параметров | Установка/обновление Xray; native выбор версии и сохранение configs. Проверить version/config/service после native операции |
| `-um`, `-um auto`, `-um vX.Y.Z` | Mihomo | Аналогично Xray | Native Mihomo update/install; не нужен для текущего Xray-only режима |
| `-uy` | Mihomo | Кнопка + output | Native Yq install/update для Mihomo, без собственного downloader |
| `-k` | Расширенная | Терминал: native вопрос о повторной загрузке | Переустановка XKeen, не обычное обновление; явно выбранное обслуживание |
| `-ri` | Расширенная | Кнопка + output | Штатное пересоздание init с native переносом настроек. Панель сама init не редактирует |
| `-channel` | Основная | Терминал: выбор Stable/Dev(Beta) | Native канал будущих обновлений; не переключение Git branch. Не приписывать несуществующий `-channel dev` API |
| `-health` | Расширенная | Кнопка + output | Native диагностика Entware по запросу, не dashboard timer. Часть native install/update preflight тоже вызывает её internally |
| `-toff` | Служебная | Advanced опция длительного download job, не отдельная action | Modifier, например `-i -toff`: снимает native curl timeout. По умолчанию выключен; panel job всё равно bounded. Не фоновая «ускоряющая настройка» |

## 2. Geodata и native расписание

| Команда / параметры | Нужность | UI и ввод | Процесс использования / итог |
|---|---|---|---|
| `-ug` | Основная | Кнопка «Обновить установленные базы» + output | GeoFile/GeoIPSET и native user geofiles. Может штатно restart active core. Затем invalidate GUI category/search cache |
| `-g` | Основная | Терминал: выбрать GeoSite/GeoIP источники | Штатная переустановка/изменение состава баз; не обычный refresh. После проверить используемые routing references |
| `-gips` | Расширенная | Условно: кнопка с доступной консолью | Native GeoIPSET install; зависит от выбранного native interception режима. Не prerequisite всех routing editors |
| `-dgips` | Расширенная | Кнопка с явным impact, output | Native удаление GeoIPSET, только advanced обслуживание; не автоматическая очистка |
| `-ugc` | Основная | Терминал: действие → день → час → минута | Создание/изменение native geodata cron. Показываем вопросы XKeen; после читаем фактический schedule и отображаем человечески. Свой cron writer/scheduler не нужен |
| `-dgc` | Основная | Терминал: native подтверждение удаления | Отключение native geodata schedule. После проверяем absence только соответствующего native job, остальные cron jobs не трогаем |
| `-dgs` | Расширенная | Терминал: подтверждение | Native GeoSite removal; GUI показывает, что routing categories могут стать недоступны |
| `-dgi` | Расширенная | Терминал: подтверждение | Native GeoIP removal; аналогично проверяем relevant references |

Для script/core нет обещания встроенного cron, если installed XKeen его не предлагает.
Панель не добавляет component-update scheduler «для полноты»; доступный native geodata
cron и SB cron показываем отдельно от panel subscription refresh/panel self-update.

## 3. Сервис, core и время запуска

| Команда / параметры | Нужность | UI и ввод | Процесс использования / итог |
|---|---|---|---|
| `-start` | Основная | Кнопка + output | Ручной native запуск и запуск после проверенного изменения configs |
| `-stop` | Основная | Кнопка + output | Native остановка по запросу/обслуживание, не daemon панели |
| `-restart` | Основная | Кнопка + output | Native restart после scoped config Save/import, если native action уже не сделал его сама |
| `-status` | Основная | Кнопка «Проверить статус» + output | Native статус по запросу. Главная использует bounded read-only observations вместо постоянного выполнения dispatcher |
| `-auto on/off`; без параметра | Основная | Переключатель → argv; без параметра native диалог | Автозапуск XKeen. Не ручная запись variable в init |
| `-di <seconds>`; без параметра | Расширенная | Числовая форма; без параметра показать текущее | Задержка инициализации router. Native команда меняет embedded init setting сама |
| `-d <seconds>`; без параметра | Расширенная | Числовая форма; без параметра показать текущее | Задержка/ожидание запуска, native interpretation; не patch init из панели |
| `-xray` | Основная | Условно: switch core action, консоль при native вопросе | Переключить installed native core на Xray; не panel rewrite `name_client` |
| `-mihomo` | Mihomo | Условно: switch core action | Переключить native core на Mihomo; скрыто/disabled при неподдержанном core |
| `-startvb on/off`; без параметра | Расширенная | Переключатель; без параметра терминал | Native startup information; не показатель реальной health |
| `-extmsg on/off`; без параметра | Расширенная | Переключатель; без параметра терминал | Native extended startup messages, advanced diagnostics |
| `-fd on/off`; без параметра | Расширенная | Переключатель; без параметра терминал | Native file-descriptor control. Не обязательное предварительное условие panel operation |
| `-cfd` | Расширенная | Кнопка + output | Разовая проверка числа file descriptors, troubleshooting |

Explicit `on/off` проверен в dispatcher. Если installed feature недоступна,
disable только её action. Нельзя превращать missing parameter в неявный toggle.

## 4. Порты и исключения

| Команда / параметры | Нужность | UI и ввод | Процесс использования / итог |
|---|---|---|---|
| `-ap <ports/ranges...>` | Основная | Форма с validated отдельными argv | Добавить проксируемые ports/ranges, native helper валидирует и сохраняет/restarts при необходимости |
| `-dp <ports/ranges...>` | Основная | Выбор строк/форма → argv | Удалить выбранные native проксируемые порты; GUI не sed-редактирует init |
| `-cp` | Основная | Read-only список + output | Получить native proxy ports при открытии настроек по запросу/refresh |
| `-ape <ports/ranges...>` | Основная | Форма → argv | Добавить ports исключения из native interception |
| `-dpe <ports/ranges...>` | Основная | Форма/выбор → argv | Удалить port exceptions |
| `-cpe` | Основная | Read-only список + output | Считать effective native исключения |

Формы используют подтверждённую native grammar портов/диапазонов, не raw command
line. Native list-file editors допустимы для отдельных data-only IP/domain lists,
но embedded script assignments всегда меняются только native командой.

## 5. DNS, policies, маршрутизация router

| Команда / параметры | Нужность | UI и ввод | Процесс использования / итог |
|---|---|---|---|
| `-dns on/off`; без параметра | Основная | Переключатель; bare native terminal с предупреждением | Включение/отключение native DNS interception. Это не editor Xray DNS servers/domain matching |
| `-pr on/off`; без параметра | Основная | Переключатель; bare terminal | Проксировать трафик самого Entware. Не включать автоматически при запуске панели |
| `-pbr on/off` | Основная | Переключатель + output | Native strict PBR validation; объяснить влияние на выбранную policy |
| `-pbr status`, `-pbr codes` | Основная | Статус/«Показать коды политик» + output | Native effective mode и marks; связать с конфигами/policy UI. Не обещать, что панель создаёт Keenetic policies |
| `-killswitch on/off`, `-killswitch status` | Основная | Явный switch/status + output | Блокировка policy при core failure. Объяснить ожидаемую потерю доступа при `on`, не включать молча |
| `-ipv6 on/off`; без параметра | Расширенная | Switch с native impact; bare terminal | Native управление IPv6 KeeneticOS, не firewall writer панели и не обязательное выключение |
| `-dscp on/off`; без параметра | Расширенная | Switch; bare terminal | Dispatcher сначала показывает native DSCP state и затем вызывает `change_dscp_proxy`. Комментарий «Состояние» не означает чистый read-only: bare может переключить. Не poll на главной |
| `-aghfix on/off`; без параметра | Расширенная | Switch при AdGuard capability; bare terminal | Native исправление отображения клиентов AdGuard Home; не отдельная реализация панели |

Domain/IP/category rules, direct/proxy DNS resolvers, Xray balancers/observatory и
custom lists — data-config editors, отдельных команд для каждого routing rule нет.
После Save: full candidate validation → scoped save → native restart если нужен →
readback. Панель не добавляет firewall/IPSET rule engine и не пишет shell snippets.

## 6. Балансировка и качество

| Команда / параметры | Нужность | UI и ввод | Процесс использования / итог |
|---|---|---|---|
| `-sb` | Основная | Терминал native menu | Статус, enable/disable, «замер сейчас» в родном меню, без копирования меню |
| `-sb on` | Основная | Условно: switch с live console | Включение native speed balancer. При missing API/pool native может спросить о настройке; enable также запускает первый speed measurement. Показать это до запуска, учитывать budget |
| `-sb off` | Основная | Switch + output | Выключить native SB, снять его override штатно; требуется при выборе отдельного panel selector |
| `-sb status` | Основная | Status button + output | Native current target/events/schedule, без запуска speed benchmark |
| `-sbt` | Служебная | Нет самостоятельной кнопки | Native cron tick. Не вызывать из панели дополнительно или при dashboard refresh; не второй scheduler |

Форма observatory/balancer/weights/intervals редактирует supported data fields,
которым нет native parameter API. Native leastPing/SB остаётся default owner.
Panel bounded one-node quality test — наша диагностика, не alias `-sbt`.
Наш quality selector включается отдельно лишь после measured comparison/expiry
acceptance; scheduler не является prerequisite native terminal или editors.

## 7. Backups и перенос

| Команда / параметры | Нужность | UI и ввод | Процесс использования / итог |
|---|---|---|---|
| `-kb` | Основная | Кнопка + output | Штатный локальный backup XKeen; проверить факт созданной native копии |
| `-kbr` | Расширенная | Терминал native confirmation/selection | Native restore XKeen, может перезаписать native installation, advanced обслуживание |
| `-xb` | Основная | Кнопка + output | Штатный локальный backup Xray configs |
| `-xbr` | Основная | Терминал confirmation/selection | Native Xray config restore, после full config/service readback |
| `-mb` | Mihomo | Кнопка + output | Native Mihomo local config backup |
| `-mbr` | Mihomo | Терминал confirmation/selection | Native Mihomo restore |
| `-cbk on/off`; без параметра | Основная | Switch → argv; bare terminal | Включить native automatic backup XKeen при его обновлении |

Portable export/import на другой router — отдельная функция панели: native data
configs + managed subscriptions/nodes + preferences в existing encrypted envelope.
Не выдавать native executable backup `-kb` за portable config export. Schedule на
новом router восстанавливается native командой, не копированием entire crontab.

## 8. Диагностика и информация

| Команда / aliases | Нужность | UI и ввод | Процесс использования / итог |
|---|---|---|---|
| `-tp` | Основная | Кнопка + output | Native listen ports/gateway/protocol inspection, troubleshooting |
| `-diag` | Расширенная | Кнопка, явный private console output | Native диагностический отчёт `/opt/diagnostic.txt`; только по запросу. Нативная маскировка не гарантирует public-safe результат; файл не загружается автоматически в issue/Telegram |
| `-xtest` | Основная | Кнопка + output | Native Xray config test по запросу. Candidate config Save валидируется до замены реальных файлов отдельно |
| `-mtest` | Mihomo | Кнопка + output | Native Mihomo config test |
| `-v`, `-version` | Информация | About/status + output | Native version. Routine dashboard version желательно из bounded file read, не hidden command execution |
| `-about` | Информация | About button + output | Информация native проекта |
| `-h`, `-help`; без флагов | Информация | «Команды XKeen» / native help console | Нативная справка установленной версии. Не unrestricted prompt/command line |
| `-ad`, `-donate` | Информация | About/help или native output | Native donation info; не нужна отдельная операционная кнопка |
| `-af`, `-feedback` | Информация | About/help или native output | Native feedback info; панель не отправляет сообщение автоматически |

`-status/-diag/-health/-cp/-cpe/...` — не гарантированно side-effect-free весь
dispatcher: до выбранной ветви импортируются modules, для многих flags есть
native package self-heal. Поэтому нет периодического hidden CLI polling панели.

## 9. Удаление и internal callbacks

| Команда | Нужность | UI и ввод | Процесс использования / итог |
|---|---|---|---|
| `-dx` | Расширенная | Терминал native confirmation | Удалить Xray; отдельная destructive action, не routine cleanup |
| `-dm` | Mihomo | Терминал native confirmation | Удалить Mihomo/Yq; учитывать выбранный core |
| `-dk` | Расширенная | Терминал native confirmation | Удалить XKeen; GUI не чистит его directories сама |
| `-remove` | Расширенная | Терминал native confirmation | Полная штатная деинсталляция XKeen/components. Не функция удаления только нашей панели |
| `-uk_post_update` | Служебная | Нет прямого endpoint/кнопки | Внутреннее продолжение native `-uk`, вызывается им самим |
| `-k_post_install` | Служебная | Нет прямого endpoint/кнопки | Внутреннее продолжение native переустановки |

## Встраивание консоли и порядок реализации

1. Один existing native job adapter: fixed action → executable/argv; interactive
   action запускает только XKeen в PTY. Shell после native exit не остаётся.
2. UI `@xterm/xterm` + `@xterm/addon-fit` загружаются лениво при открытии консоли;
   Go PTY adapter, например `github.com/creack/pty`, не требует Node на роутере.
   [xterm](https://github.com/xtermjs/xterm.js), [Go PTY](https://github.com/creack/pty).
3. Authenticated output stream и input/resize routes привязаны к одному job/owner,
   с existing auth/origin/CSRF. Это не SSH session, shell command endpoint или file browser.
   Normal noninteractive job показывает консоль read-only; conditional job получает
   PTY с начала, даже если output initially collapsed. Не сменять/replay процесс
   ради открытия терминала. Ctrl-C — явная cancellation с последующим unknown/readback.
4. Raw native console — приватная поверхность оператора, не обычная sanitized status
   projection. Ring output ≤256KiB/job в RAM, ограниченные chunks/input; logout
   закрывает доступ и очищает UI buffer. Ничего из stdin/stdout автоматически не
   уходит в Git/issue/Telegram. Отключить OSC clipboard/external links/HTML rendering.
5. Closing browser отсоединяет viewer, не job. Interactive ожидание пользователя
   имеет отдельный deadline (initial10min inactivity); downloads initial15min ceiling,
   service90s. По deadline не заявлять отсутствие native side effects. Проверить
   foreground/TTY/service-children поведение native на focused qualification.
6. Сначала lifecycle/update/geodata schedule/settings/subscriptions; затем advanced
   backup/core-switch/destructive/native help. Mihomo показывается conditional.
   Все internal callbacks и `-sbt` остаются без прямого API. Не ждать реализации
   каждого advanced flag ради первой usable версии панели.

Полный список выше сопоставляется с основным dispatcher: каждый top-level branch
покрыт, aliases объединены. Для installed version показать только фактически
доступные commands, без обязательного matching всего native source tree.
