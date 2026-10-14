# Доступ к Cloud.ru: ключ, проект, профили, переменные окружения

cloudru-vm ходит в API Cloud.ru от имени **сервисного аккаунта**: пара
Key ID + Key Secret его ключа доступа обменивается в IAM
(`iam.api.cloud.ru`) на токен на час, токен кэшируется на диске. Кроме ключа
нужен **UUID проекта**, в котором создаются ресурсы.

Подойдёт и персональный ключ доступа пользователя (та же пара Key ID / Key
Secret, тот же обмен), но для скриптов, CI и MCP-сервера лучше отдельный
сервисный аккаунт с правами только на нужный проект.

> Термины. У Cloud.ru есть ещё «API-ключ» — отдельная сущность
> (`Authorization: Api-Key …`), cloudru-vm её **не использует**. Переменная
> `CLOUDRU_API_KEY` исторически означает пару `keyId:secret` и устарела.

## 1. Сервисный аккаунт

Пути по меню личного кабинета [console.cloud.ru](https://console.cloud.ru) —
по документации Cloud.ru на 14.10.2026; подписи кнопок могут меняться.

1. «Пользователи» → «Сервисные аккаунты» → «Создать аккаунт» (справа вверху).
2. Название и описание (например, `cloudru-vm-ci`).
3. Уровень аккаунта — **«Проект»**, если аккаунт нужен для одного проекта.
4. «Продолжить» → назначьте роли (см. [раздел 3](#3-роли)) → «Создать».

Аккаунт должен перейти в статус «Активен».
Документация: [создание сервисного аккаунта](https://cloud.ru/docs/console_api/ug/topics/guides__service_accounts_create?source-platform=Evolution).

## 2. Ключ доступа

1. «Пользователи» → «Сервисные аккаунты» → название аккаунта.
2. Блок «Учетные данные доступа», вкладка «Ключи доступа» → «Создать ключ».
3. Описание ключа (по нему вы потом отличите ключи при ротации) и **срок
   жизни**: по его истечении ключ перестаёт работать. Бессрочный ключ Cloud.ru
   не рекомендует; ограничьте срок или меняйте ключ сами не реже раза в год.
4. «Создать». **Key Secret показывается один раз** — сохраните его сразу
   (в менеджер паролей или сразу в `cloudru-vm configure`).

На один сервисный аккаунт — до 20 ключей.
Документация: [создание ключа доступа](https://cloud.ru/docs/console_api/ug/topics/guides__service_accounts_key-create?source-platform=Evolution).

## 3. Роли

Честно о том, что известно:

- Cloud.ru публикует роли уровня платформы — `platform.project.admin`,
  `platform.project.user`, `platform.project.viewer` (в части документации —
  `iam.project.admin`) и `iam.serviceaccount.*`. Сервисные роли вроде
  `compute.*` и `vpc.*` в документации не опубликованы.
- **Проверено 14.10.2026** (облачная приёмка этапов 2, 5 и 8): полный сценарий
  `configure → whoami → init → deploy → verify → logs → destroy` и чтение
  зон/образов/ВМ работают на сервисном аккаунте **с правами на уровне
  проекта**.
- **Точный минимальный набор ролей — открытый вопрос**: роли на запись по
  отдельности не проверялись. Если нужна минимальная выдача, начните с роли
  пользователя проекта и расширяйте по ошибкам `403`.

Что видно в ошибках: `403` при обмене ключа — `AUTH_FAILED`, код выхода `2`;
`403` от compute API (ВМ, диски, `deploy`) — `AUTH_FAILED`, код `2`; от других
сервисов (vpc, dns, …) — код `5` с текстом ответа Cloud.ru.
Подробнее — [troubleshooting.md](troubleshooting.md#403-forbidden-от-сервиса-код-5).

Organization API (`organization.api.cloud.ru/v3/projects`) **не отдаёт
сервисному аккаунту список проектов** (проверено 14.10.2026: ответ `400`
«user not found»). Поэтому `configure` для сервисного аккаунта просит
`project_id` вручную — это ожидаемо, а не ошибка ключа.

## 4. UUID проекта

UUID проекта (вида `3f2a8c1e-…`) возьмите в консоли Cloud.ru, в сведениях о
проекте, в котором сервисный аккаунт получил роли. Персональный ключ
пользователя видит свои проекты сам — `configure` покажет список.

## 5. `cloudru-vm configure`

Интерактивно (в терминале):

```console
$ cloudru-vm configure
Configuring profile "default" in /home/me/.config/cloudru-vm
Access key ID: 7b1e…
Secret:                                   ← не отображается
Could not list projects (…).              ← для сервисного аккаунта это нормально
Project ID: 3f2a8c1e-…
Profile "default" saved to /home/me/.config/cloudru-vm
```

`configure` сначала обменивает ключ на токен — неверный ключ не сохранится.
Повторный запуск показывает текущие значения в скобках; Enter их оставляет.

Без терминала (CI, скрипты). Секрет — только через stdin, не флагом: флаг
виден в `ps` и истории оболочки.

```sh
printf '%s' "$CLOUDRU_SECRET" | cloudru-vm configure \
  --key-id "$CLOUDRU_KEY_ID" --secret-stdin --project-id "$CLOUDRU_PROJECT_ID"
```

Флаги: `--key-id`, `--secret-stdin`, `--project-id` (пропускает список
проектов), `--region` (зона по умолчанию для профиля).

Проверка:

```sh
cloudru-vm whoami        # профиль, Key ID (маскирован), проект, срок токена; код 2 при отказе IAM
cloudru-vm doctor        # файлы и права, откуда взята каждая настройка, доступность API, kubectl/helm
```

### Профили

Профиль — именованный набор «ключ + проект + умолчания». По умолчанию —
`default`.

```sh
cloudru-vm configure --profile staging        # создать/изменить профиль
cloudru-vm --profile staging vm list          # использовать разово
export CLOUDRU_PROFILE=staging                # использовать в этой оболочке
cloudru-vm configure list                     # профили; активный помечен *
cloudru-vm configure delete staging           # удалить профиль и его кэш токена
```

Имя профиля: латиница, цифры, `-`, `_`, `.`, до 64 символов. Профиль, названный
явно (`--profile`, `CLOUDRU_PROFILE`), обязан существовать — иначе код `2`.

### Ключи Object Storage (S3)

Команды `s3` и инструменты `s3_*` ходят в Object Storage по протоколу S3 и
используют **отдельные** ключи Object Storage (см. документацию Object Storage
Cloud.ru), а не ключ IAM. `configure` их не спрашивает: задайте переменными
`CLOUDRU_S3_*` (таблица ниже) или секцией `s3` профиля в `credentials.yaml`:

```yaml
profiles:
  default:
    key_id: 7b1e…
    secret: …
    s3:
      endpoint: https://s3.cloud.ru     # по умолчанию
      access_key: …
      secret_key: …
      region: ru-central-1              # по умолчанию
```

## 6. Откуда берутся настройки

От высшего приоритета к низшему:

1. **флаги** (`--project-id`, `--region`, `--profile`, флаги `deploy`…);
2. **переменные окружения** (таблица ниже); пустая переменная = не задана;
3. **файл проекта** `./.cloudru-vm.yaml` в текущем каталоге (его создаёт
   `init`; его можно коммитить — секреты из него **не читаются**, `doctor`
   предупредит, если они там есть);
4. **профиль** — `config.yaml` и `credentials.yaml` (см. [раздел 8](#что-хранится-на-машине));
5. значения по умолчанию.

Ключ берётся парой целиком: если задана хотя бы одна из `CLOUDRU_KEY_ID` /
`CLOUDRU_SECRET`, ключ профиля не используется. `cloudru-vm doctor` показывает,
откуда взята каждая настройка (`flag`, `env`, `project`, `profile`,
`default`).

## 7. Переменные окружения

Полный список переменных, которые читает cloudru-vm. Тест в CI
(`TestAuthDoc_ListsEveryEnvVar`) проверяет, что каждая переменная из кода
упомянута в этом файле.

### Ключ и профиль

| Переменная | Что задаёт | Флаг / файл |
|---|---|---|
| `CLOUDRU_KEY_ID` | Key ID ключа доступа | `credentials.yaml: key_id` |
| `CLOUDRU_SECRET` | Key Secret ключа доступа | `credentials.yaml: secret` |
| `CLOUDRU_API_KEY` | **устарела**: пара `keyId:secret` одной строкой; читается, только если не заданы две переменные выше, и печатает предупреждение | — |
| `CLOUDRU_PROFILE` | профиль вместо `default` | `--profile` |
| `CLOUDRU_CONFIG_DIR` | каталог конфигурации вместо стандартного; кэш токена — в его подкаталоге `cache/` (одна переменная изолирует всё: CI, тесты, несколько окружений) | — |

### Проект и параметры развёртывания

| Переменная | Что задаёт | Флаг / ключ в `.cloudru-vm.yaml` и профиле |
|---|---|---|
| `CLOUDRU_PROJECT_ID` | UUID проекта (обязателен для команд, создающих ресурсы) | `--project-id` / `project_id` |
| `CLOUDRU_REGION` | UUID зоны доступности (`cloudru-vm list-zones`) | `--region` / `region` |
| `CLOUDRU_IMAGE_ID` | UUID загрузочного образа для `deploy` (`cloudru-vm list-images`) | `deploy --image-id` / `image_id` |
| `CLOUDRU_FLAVOR` | конфигурация ВМ, например `lowcost10-2-4`; `auto` — подобрать (по умолчанию) | `deploy --flavor` / `flavor` |
| `CLOUDRU_DISK_SIZE` | размер загрузочного диска, ГБ (по умолчанию 10) | `deploy --disk-size` / `disk_size` |
| `CLOUDRU_COMPOSE_FILE` | путь к compose-файлу (по умолчанию `docker-compose.yml`) | `deploy -f` / `compose_file` |
| `CLOUDRU_PROJECT_NAME` | имя развёртывания (по умолчанию — имя текущего каталога) | `project_name` |
| `CLOUDRU_TIMEOUT` | таймаут в секундах (по умолчанию 300); в v1.0 читается в конфигурацию, но команды его пока не используют | `timeout` |
| `CLOUDRU_AGREEMENT_ID` | номер договора для инструментов биллинга (`billing_*`), если договоров несколько | — |

### Object Storage (S3)

| Переменная | Что задаёт | В `credentials.yaml` |
|---|---|---|
| `CLOUDRU_S3_ENDPOINT` | адрес S3 (по умолчанию `https://s3.cloud.ru`) | `s3.endpoint` |
| `CLOUDRU_S3_ACCESS_KEY` | ключ доступа Object Storage | `s3.access_key` |
| `CLOUDRU_S3_SECRET_KEY` | секрет Object Storage | `s3.secret_key` |
| `CLOUDRU_S3_REGION` | регион S3 (по умолчанию `ru-central-1`) | `s3.region` |

### MCP-сервер

| Переменная | Что задаёт |
|---|---|
| `MCP_BEARER_TOKEN` | токен, который клиенты `mcp --http` присылают в `Authorization: Bearer …`; без него HTTP-сервер не стартует (кроме `--auth none` на loopback). Только переменной — флага нет, чтобы токен не светился в `ps` |
| `MCP_HEALTHCHECK_URL` | адрес для `cloudru-vm mcp healthcheck` (HEALTHCHECK Docker-образа); по умолчанию `http://127.0.0.1:8080/healthz` |

### Вывод

| Переменная | Что задаёт |
|---|---|
| `NO_COLOR` | если задана и не пуста — без цвета, как `--no-color` ([no-color.org](https://no-color.org)) |
| `TERM` | `TERM=dumb` — без цвета |

Переменные `CLOUDRU_*` и `MCP_BEARER_TOKEN` **не передаются** в дочерние
`kubectl` и `helm`.

Переменные установщика (`CLOUDRU_VM_VERSION`, `INSTALL_DIR`,
`CLOUDRU_VM_BASE_URL`) — в [install.md](install.md#linux-и-macos-installsh).

## Что хранится на машине

| Что | Где | Права |
|---|---|---|
| настройки профилей (проект, зона, умолчания) | `config.yaml` в каталоге конфигурации | `0600`, каталог `0700` |
| ключи профилей (Key ID, Key Secret, S3) | `credentials.yaml` там же | `0600` |
| кэш IAM-токена (на час, привязан к Key ID) | `token-<профиль>.json` в каталоге кэша | `0600` |
| настройки проекта (без секретов) | `./.cloudru-vm.yaml` | `0644`, можно коммитить |
| состояние развёртываний (ID ВМ, IP, floating IP, порты) | `./.cloudru-vm/state.json` | |
| **приватный SSH-ключ к созданным ВМ** | `./.cloudru-vm/id_ed25519` (+ `.pub`) | `0600`, каталог `0700` |
| планы MCP `deploy_plan` | `./.cloudru-vm/plans/` | |
| задания `stack_*` (MCP) | `~/.cloudru-vm/stack-jobs/` | |
| своя таблица цен (необязательно, создаёте вы) | `~/.cloudru-vm/pricing.json` | |

Каталоги конфигурации и кэша:

| ОС | Конфигурация | Кэш токена |
|---|---|---|
| Linux | `~/.config/cloudru-vm` (или `$XDG_CONFIG_HOME/cloudru-vm`) | `~/.cache/cloudru-vm` (`$XDG_CACHE_HOME`) |
| macOS | `~/Library/Application Support/cloudru-vm` | `~/Library/Caches/cloudru-vm` |
| Windows | `%AppData%\cloudru-vm` | `%LocalAppData%\cloudru-vm` |
| с `CLOUDRU_CONFIG_DIR=D` | `D` | `D/cache` |

`./` — каталог, из которого запущена команда (для MCP по stdio — рабочий
каталог, в котором клиент запускает сервер). SSH-ключ создаётся по одному на
каталог проекта при первом `deploy` (сейчас — уже на `deploy --dry-run`). **`.cloudru-vm/` не коммитьте**
(добавьте в `.gitignore`): утёкший ключ — это доступ к вашим ВМ.

На Windows права `0600` не выставляются; `doctor` там права не проверяет.

### Удалить всё

```sh
cloudru-vm destroy                          # в каждом каталоге проекта: ВМ и floating IP
cloudru-vm configure delete default         # профиль и его кэш токена (и другие профили)
rm -rf ./.cloudru-vm ./.cloudru-vm.yaml     # состояние и SSH-ключ проекта
rm -rf ~/.config/cloudru-vm ~/.cache/cloudru-vm ~/.cloudru-vm   # Linux; пути других ОС — в таблице выше
```

Затем удалите ключ доступа в консоли (раздел «Ключи доступа» сервисного
аккаунта), если он больше не нужен. Старый файл `~/.cloudru-vm.yaml` (версии
до 1.0) больше не читается — удалите его, если остался.

## Ротация ключа

1. Создайте новый ключ у того же сервисного аккаунта ([раздел 2](#2-ключ-доступа)).
2. `cloudru-vm configure` (или `--profile …`): введите новый Key ID и секрет;
   проект сохранится. Без терминала — та же команда с `--key-id` и
   `--secret-stdin`. Кэш старого токена не мешает: он привязан к Key ID.
3. `cloudru-vm whoami` — новый ключ принят.
4. Обновите ключ везде, где он задан переменными: секреты CI, окружение
   MCP-сервера / контейнера.
5. Удалите старый ключ в консоли. Токены, выданные по нему, перестают
   обновляться, а живут не дольше часа.

Если ключ утёк — сначала удалите его в консоли, потом выполняйте шаги 1–4.
