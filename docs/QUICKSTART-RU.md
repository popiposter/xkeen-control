# Быстрый запуск

Текущая production-версия `xkeen-control` уже работает поверх Keenetic + XKeen + Xray, но **публичный one-command installer ещё не выпущен**. Его реализация — активный Slice D / Issue #2.

Ниже отдельно показан текущий операторский путь и будущий пользовательский путь, чтобы не смешивать их.

## Что уже работает

Текущая C.1-версия умеет:

- управлять VPN-нодами и именованными подписками через preview/apply;
- хранить секретные VPN-данные только локально в `nodes.json`;
- стабильно удерживать рабочую ноду и быстро переключаться при подтверждённой недоступности;
- показывать native / override / effective selection;
- запускать bounded sustained benchmark без flash-churn;
- валидировать Xray candidate и откатываться при ошибке;
- работать как один заранее собранный Go-бинарник с web UI.

## Текущая установка / восстановление

Текущий путь предназначен для разработчика/оператора и требует заранее подготовленных Entware, XKeen + Xray и off-router build.

Не выполняйте blanket `opkg upgrade` как обычный шаг установки продукта.

### 1. Подготовить роутер

Нужны рабочий `/opt` / Entware/Open Package, SSH, XKeen + Xray, необходимые runtime/geodata зависимости и отдельный backup VPN secret state при восстановлении существующей конфигурации.

### 2. Восстановить secret registry

Текущий источник VPN/subscription secrets:

```text
/opt/etc/xkeen-control/secrets/nodes.json
```

Права:

```text
каталог 0700
nodes.json 0600
```

Содержимое `nodes.json` нельзя помещать в GitHub issues/PR/logs. Для миграции старой рабочей установки существует одноразовый `scripts/migrate-secrets.sh`.

### 3. Развернуть текущую версию

С рабочего компьютера используется конкретная проверенная revision `popiposter/xkeen-control` и заранее собранный ARM64 artifact. На роутере:

```sh
cd /opt/etc/xkeen/repo
chmod +x scripts/*.sh
./scripts/deploy.sh
./scripts/verify.sh
```

Deploy собирает полный candidate в `/tmp`, рендерит `04_outbounds.json` из локального `nodes.json`, валидирует Xray до активной mutation, создаёт bounded backup, переключает generation через foreground lifecycle, ждёт RoutingService/inventory и откатывает generation при ошибке.

Тяжёлый benchmark после deploy/restart **не нужен**.

### 4. Открыть панель

По умолчанию:

```text
127.0.0.1:8787
```

Безопасный локальный доступ через SSH tunnel:

```sh
ssh -L 8787:127.0.0.1:8787 <router>
```

Также поддерживается один явно заданный private LAN address. `0.0.0.0`, public IP/hostname и прямое WAN exposure не поддерживаются.

## Будущий one-command installer — #2

После первого подписанного GitHub Release целевой UX будет таким:

В Draft-реализации команда установки не рекламируется: опубликованный
installer должен быть asset конкретного квалифицированного semver-релиза
GitHub Release. Mutable `latest/download`, branch/archive и непроверенный
pre-release URL запрещены до прохождения release operator gate.

**Пока первого квалифицированного Release нет, эту команду использовать нельзя.**

Installer #2 должен проверить root, `/opt`, CPU/ABI и свободное место; выполнить только `opkg update` + конкретно недостающие prerequisites; не выполнять blanket `opkg upgrade`; проверить signed/checksummed release; установить и запустить панель; выдать одноразовый setup credential без логирования; выбрать только loopback/private management address; определить состояние XKeen/Xray; сохранить существующие настройки/secrets при повторном запуске.

## Будущий Backup & Restore — #3

D.1 добавит переносимый typed backup: обычный export без VPN/subscription secrets по умолчанию, отдельный encrypted export с секретами и preview/import/restore через транзакцию. До #3 backup с `nodes.json` остаётся секретным операторским материалом.

## Будущие обновления компонентов — #4

Панель будет показывать версии XKeen/Xray/geodata и управлять check/update/rollback через capability-aware операции. Raw shell passthrough и generic package manager не планируются.

## Проверка текущей установки

```sh
./scripts/verify.sh
```

С клиента проверьте ожидаемую политику: обычный трафик идёт DIRECT, а явно настроенные proxy-required сервисы — через единый `bal-proxy` pool.

Подробности текущего production path: [`OPERATIONS.md`](OPERATIONS.md) и [`FRESH-KEENETIC.md`](FRESH-KEENETIC.md).
