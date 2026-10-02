# Матрица команд XKeen: покрытие будущей оболочки

Snapshot: `jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d`, `scripts/xkeen`. Это инвентарь dispatcher и план UI-покрытия, не утверждение, что все действия уже безопасно автоматизируются. Вложенные параметры проверяются в TASK-006. Mihomo/Yq строки доступны только при отдельном tested capability profile.

Найдено 69 верхнеуровневых ветвей команды (алиасы в одной строке).

| Флаг/алиасы | Строка upstream | Группа | Плановое представление |
|---|---:|---|---|
| `-i\|-install` | 147 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-io` | 403 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-ug` | 542 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-uk` | 588 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-uk_post_update` | 628 | Служебный | Внутренний native flow; без самостоятельной кнопки произвольного вызова |
| `-ux` | 671 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-um` | 749 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-uy` | 843 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-ugc` | 877 | Расписание | Редактор native geodata cron; без собственного scheduler |
| `-ri` | 892 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-dgc` | 921 | Расписание | Редактор native geodata cron; без собственного scheduler |
| `-dx` | 937 | Удаление | Advanced native operation с конкретным impact/confirmation, без Telegram shortcut |
| `-dm` | 961 | Удаление | Advanced native operation с конкретным impact/confirmation, без Telegram shortcut |
| `-dk` | 981 | Удаление | Advanced native operation с конкретным impact/confirmation, без Telegram shortcut |
| `-dgi` | 1010 | Удаление | Advanced native operation с конкретным impact/confirmation, без Telegram shortcut |
| `-dgs` | 1027 | Удаление | Advanced native operation с конкретным impact/confirmation, без Telegram shortcut |
| `-remove` | 1044 | Удаление | Advanced native operation с конкретным impact/confirmation, без Telegram shortcut |
| `-k` | 1154 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-k_post_install` | 1188 | Служебный | Внутренний native flow; без самостоятельной кнопки произвольного вызова |
| `-g` | 1230 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-gips` | 1258 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-dgips` | 1268 | Удаление | Advanced native operation с конкретным impact/confirmation, без Telegram shortcut |
| `-kb` | 1274 | Backup | Native local backup/restore; portable export отдельный panel flow |
| `-kbr` | 1281 | Backup | Native local backup/restore; portable export отдельный panel flow |
| `-xb` | 1288 | Backup | Native local backup/restore; portable export отдельный panel flow |
| `-xbr` | 1295 | Backup | Native local backup/restore; portable export отдельный panel flow |
| `-mb` | 1303 | Backup | Native local backup/restore; portable export отдельный panel flow |
| `-mbr` | 1310 | Backup | Native local backup/restore; portable export отдельный panel flow |
| `-tp` | 1318 | Диагностика | Typed safe projection; raw diagnostic output приватный |
| `-v\|-version` | 1324 | Информация | Информация/ссылка; версии через bounded read-only projection |
| `-about` | 1329 | Информация | Информация/ссылка; версии через bounded read-only projection |
| `-ad\|-donate` | 1335 | Информация | Информация/ссылка; версии через bounded read-only projection |
| `-af\|-feedback` | 1341 | Информация | Информация/ссылка; версии через bounded read-only projection |
| `-h\|-help` | 1347 | Информация | Информация/ссылка; версии через bounded read-only projection |
| `-start` | 1353 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-stop` | 1359 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-restart` | 1365 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-status` | 1371 | Диагностика | Typed safe projection; raw diagnostic output приватный |
| `-dscp` | 1376 | Диагностика | Typed safe projection; raw diagnostic output приватный |
| `-auto` | 1386 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-startvb` | 1395 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-fd` | 1404 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-cfd` | 1412 | Диагностика | Typed safe projection; raw diagnostic output приватный |
| `-ap` | 1420 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-dp` | 1441 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-cp` | 1462 | Диагностика | Typed safe projection; raw diagnostic output приватный |
| `-ape` | 1467 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-dpe` | 1488 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-cpe` | 1509 | Диагностика | Typed safe projection; raw diagnostic output приватный |
| `-di` | 1514 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-d` | 1521 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-diag` | 1528 | Диагностика | Typed safe projection; raw diagnostic output приватный |
| `-channel` | 1535 | Установка/обновления | Native job; версия/выбор требуют доказанного dialogue или machine API |
| `-xray` | 1542 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-mihomo` | 1547 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-ipv6` | 1552 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-dns` | 1560 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-pr` | 1569 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-pbr` | 1577 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-killswitch` | 1601 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-extmsg` | 1621 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-cbk` | 1629 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-aghfix` | 1637 | Настройки/lifecycle | Typed field/action; preserve unrelated native config |
| `-xtest` | 1645 | Диагностика | Typed safe projection; raw diagnostic output приватный |
| `-mtest` | 1650 | Диагностика | Typed safe projection; raw diagnostic output приватный |
| `-health` | 1655 | Диагностика | Typed safe projection; raw diagnostic output приватный |
| `-toff` | 1662 | Модификатор | Не давать отключать bounded timeout panel job |
| `-sb` | 1667 | Выбор узла | Один из взаимоисключающих owners; native menu mapping |
| `-sbt` | 1674 | Служебный | Внутренний native flow; без самостоятельной кнопки произвольного вызова |

[Точный dispatcher](https://github.com/jameszeroX/XKeen/blob/5aaece27a70d5bd002c615248614914ebbc4569d/scripts/xkeen). Реализация не должна создавать endpoint произвольных argv или shell.
