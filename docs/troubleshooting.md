# Типовые проблемы

Первое, что стоит запустить при любой проблеме:

```sh
cloudru-vm doctor      # конфиг, права файлов, откуда взята каждая настройка, IAM/Compute API, ключ, kubectl/helm
cloudru-vm whoami      # принимает ли IAM ключ, какой проект выбран
```

Коды выхода — в [exit-codes.md](exit-codes.md).

## Ключ и права

### `AUTH_REQUIRED` (код 2): no access key

Ключ не найден ни в переменных, ни в профиле. `cloudru-vm configure` или
`CLOUDRU_KEY_ID` + `CLOUDRU_SECRET`. Если вы задали `--profile` /
`CLOUDRU_PROFILE` — проверьте `cloudru-vm configure list`: профиль из другого
`CLOUDRU_CONFIG_DIR` не виден.

### `AUTH_FAILED` (код 2): IAM rejected the credentials (status 401/403)

IAM не принял пару Key ID / Key Secret:

- опечатка или пробел в секрете — введите заново через `cloudru-vm configure`;
- ключ удалён или истёк его срок жизни (задаётся при создании) — создайте
  новый ([auth.md → ротация](auth.md#ротация-ключа));
- сервисный аккаунт выключен;
- одна из `CLOUDRU_KEY_ID` / `CLOUDRU_SECRET` задана в окружении — тогда ключ
  профиля не используется вовсе: `cloudru-vm doctor` покажет `key_id from env`.

### `403 Forbidden` от сервиса (код 5)

Ключ принят, но у сервисного аккаунта нет прав на операцию в этом проекте
(или `project_id` — чужой проект). Проверьте роли аккаунта в проекте
(«Пользователи» → «Сервисные аккаунты»). Точный минимальный набор ролей не
опубликован и не проверен — см. [auth.md → роли](auth.md#3-роли); на
аккаунте с правами на уровне проекта весь сценарий `deploy` работает.

У compute API (ВМ, диски, `deploy`, `destroy`) тот же отказ приходит как
`AUTH_FAILED: access denied (403)` с кодом `2`.

### `configure` не показывает проекты: «Could not list projects»

Ожидаемо для сервисного аккаунта: Organization API не отдаёт ему список
проектов (ответ `400` «user not found», проверено 14.10.2026). Введите UUID
проекта вручную или передайте `--project-id`. На ключ это не влияет — он уже
проверен до запроса списка.

### `CLOUDRU_API_KEY … is deprecated`

Предупреждение, не ошибка. Замените на `CLOUDRU_KEY_ID` и `CLOUDRU_SECRET`
или на профиль.

### `legacy config` в `doctor`

Найден `~/.cloudru-vm.yaml` от старых версий — он больше не читается.
Перенесите значения: ключ и проект — `cloudru-vm configure`, настройки проекта —
`./.cloudru-vm.yaml` (`cloudru-vm init`). Потом удалите старый файл.

## Создание и удаление ВМ

### ВМ уходит в `error` / `error_creating`

14.10.2026 в зоне **ru.AZ-1** новые ВМ уходили в `error_creating`. Возьмите
другую зону, например **ru.AZ-2**:

```sh
cloudru-vm list-zones                       # ID и имена зон
cloudru-vm deploy --region <UUID ru.AZ-2>   # или region: в .cloudru-vm.yaml
```

ВМ в ошибке удалите (`cloudru-vm destroy` или `cloudru-vm vm delete <id>`),
чтобы она не занимала квоту.

### Удаление отвечает `422`, ВМ в состоянии `creating`

ВМ, которая ещё создаётся, удалить нельзя — API отвечает `422`. Подождите,
пока она перейдёт в `running` или `error` (`cloudru-vm vm get <id>`), и
повторите `destroy`. В скриптах — повтор с паузой:

```sh
for i in 1 2 3; do cloudru-vm destroy --yes && break; sleep 30; done
```

### После `destroy` в группе безопасности остались открытые порты

`deploy` открывает порты из `ports:` compose-файла правилом в **общей группе
безопасности зоны** (входящий TCP с `0.0.0.0/0`, описание
`cloudru-vm: port N`). `destroy` удаляет ВМ и освобождает floating IP, но
правила **не удаляет** — они остаются и действуют для других ВМ в этой
группе. Удалите их вручную в консоли (группы безопасности VPC); посмотреть
группы — `cloudru-vm vpc security-groups`.

### `VM has no public IP address`

Floating IP не назначился (квота на публичные IP, временная ошибка API).
Проверьте квоты (ниже), назначьте IP в консоли или выполните `destroy` и
`deploy` заново.

### Прерванный `deploy` (Ctrl-C, код 130)

Если ВМ уже создана, команда спросит, удалить ли её (в терминале), или
напишет её ID и команду для удаления. ВМ продолжает тарифицироваться, пока
вы её не удалите.

## Квоты и лимиты

### Не хватает квоты

Ошибка API с текстом про квоту/лимит (код 5). Текущие лимиты и использование:

```sh
cloudru-vm tools call org_quotas
```

Увеличение квоты — через поддержку Cloud.ru.

### `API_RATE_LIMITED` (код 5), HTTP 429

Cloud.ru ограничил частоту запросов. Запросы к compute повторяются с
нарастающей паузой сами; если ошибка дошла до вас, подождите минуту и
повторите. В скриптах не запускайте много команд параллельно.

### `API_UNREACHABLE` (код 1)

Нет связи с `*.api.cloud.ru`: сеть, DNS, прокси. `cloudru-vm doctor`
покажет, какие API недоступны. Обрывы бывают и разовыми (при облачной
приёмке `list-zones` из контейнера падал так примерно в одном запуске из
трёх) — повторите команду.

## SSH и `deploy`

### `SSH_TIMEOUT`: не удаётся подключиться к ВМ

`deploy` ставит Docker и загружает compose-файлы по SSH **с вашей машины**.
Нужен **исходящий доступ к порту 22** публичного IP ВМ. Корпоративные сети и
VPN часто его закрывают — запустите с другой сети или с ВМ/раннера, где
порт 22 открыт наружу. Проверка: `nc -vz <IP> 22` (IP — в `cloudru-vm status`).
Сразу после создания ВМ SSH поднимается не мгновенно; `deploy` ждёт и
повторяет подключение сам.

### `SSH_AUTH_FAILED`

SSH-ключ к ВМ лежит в `./.cloudru-vm/id_ed25519` **каталога, из которого
делался `deploy`**. Запуск `status`, `logs`, `deploy` из другого каталога или
после удаления `.cloudru-vm/` — это другой ключ, и ВМ его не примет.
Вернитесь в исходный каталог (или восстановите `.cloudru-vm/`). Если ключ
потерян — `destroy` и новый `deploy`.

### `SSH_HOST_KEY_CHANGED`

Ключ хоста ВМ не совпал с сохранённым при первом подключении. Обычно IP
достался другой ВМ (ВМ пересоздали вне cloudru-vm). Если вы уверены, что это
ваша ВМ, — `destroy` и `deploy` заново; иначе не подключайтесь.

### `deploy` отвечает «No changes», хотя приложение не запущено

Если `docker compose up` упал, запись остаётся в состоянии `running`, а хэш
compose-файла не изменился. Повторите с `cloudru-vm deploy --force`.

### `verify`: `UNHEALTHY` (код 1)

Порт приложения не ответил снаружи. Проверьте `cloudru-vm logs`, что
контейнер слушает `0.0.0.0`, и что порт открыт в группе безопасности
(`deploy` открывает только порты из `ports:`).

## Kubernetes, S3

- `kubectl not found on the host` / `not available in this mode` — установите
  `kubectl`/`helm` ([install.md](install.md#kubectl-и-helm)); в Docker-образе
  их нет.
- `S3 credentials not configured` (код 5) — задайте ключи Object Storage
  (`CLOUDRU_S3_ACCESS_KEY`, `CLOUDRU_S3_SECRET_KEY` или секция `s3` профиля,
  [auth.md](auth.md#ключи-object-storage-s3)).
- `agreement_id not configured` — для инструментов биллинга задайте
  `CLOUDRU_AGREEMENT_ID`.

## Обращение в поддержку

Ошибки cloudru-vm — в [Issues](https://github.com/shatim-ops/cloudru-vm-cli/issues)
репозитория; проблемы самого облака (квоты, ресурсы в ошибке, биллинг) — в
[поддержку Cloud.ru](https://cloud.ru/docs/evolution/overview/contacts/technical-support).

Приложите:

1. `cloudru-vm version --json`;
2. `cloudru-vm doctor --json` — Key ID в нём маскирован, секретов нет;
3. точную команду, её вывод с `--json` и код выхода (`echo $?`);
4. время (с часовым поясом), зону, ID ВМ или ресурса (`cloudru-vm status --json`).

**Не прикладывайте**: Key Secret, `credentials.yaml`, `CLOUDRU_SECRET`,
`MCP_BEARER_TOKEN`, ключи S3, содержимое `.cloudru-vm/` (там приватный
SSH-ключ к вашим ВМ), kubeconfig со встроенным токеном. Если секрет всё же
попал в сообщение — удалите ключ в консоли и создайте новый.
