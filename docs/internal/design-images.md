# Дизайн: ВМ из образов и продуктов маркетплейса в CLI

Статус: проект, 16.10.2026. Код не менялся, облако не трогалось. Факты
отмечены ссылкой на файл:строку или документ; что не проверено вживую —
помечено «непроверено», мнения и гипотезы — явно.

Вопрос владельца: «Не понимаю, как через CLI человек развернёт, например,
Astra Linux, а не голую ВМ. Ни один нормальный человек UUID не знает».

## 1. Резюме

**Рекомендация — вариант B «каталог как ядро» (стоимость M):**

1. `evo image list [подстрока] --type public|marketplace|private --zone … --json`
   и `evo image get <имя|UUID>` — поиск по каталогу с фильтрами, которые
   compute API **уже даёт на сервере**: `name`, `display_name`, `type`,
   `enabled`, `availability_zone_id`, `need_gpu`, `free_tier_enabled`,
   `project_id` (спецификация `compute/openapi-v3.yaml`, `GET /api/v1/images`;
   сейчас мы не передаём ни одного — `internal/provision/adapter.go:197-198`).
2. `evo vm create <name> --image <имя|семейство|UUID>` вместо `--image-id <UUID>`.
   Ссылка на образ разрешается одним общим резолвером: UUID → точное имя →
   `display_name` → семейство (`ubuntu`, `debian-12` = «самая свежая версия
   семейства», как `--image-family` в GCP и `get-latest-from-family` в YC).
   Неоднозначность — ошибка со списком кандидатов (без терминала) или меню
   (на терминале). Подстрока в `vm create` **не** угадывается: платное
   действие не должно зависеть от удачного совпадения.
3. Поля продукта (`image_metadata`): типовые заполняет evo (как сейчас в трёх
   местах), остальные обязательные — `--meta k=v`, `--meta-file`, на
   терминале — вопрос; секретные поля не печатаются ни в превью, ни в логе,
   ни в state.
4. Один пакет `internal/images` (каталог, резолвер, заполнение метаданных)
   вместо трёх расходящихся копий (`engine/deploy.go:372-430`,
   `tools/tools.go:1554-1613`, `deployplan/resolve.go:307-355`). Четвёртый путь
   создания ВМ не появляется: `engine.Deploy`, `provision`, `deploy_plan/apply`
   зовут одно и то же.
5. Превью: тип образа, заполненные поля, цена. Цену пробуем брать из
   `POST /api/v1/projects/{project_id}/price-calculation` — он есть в
   спецификации compute и возвращает отдельную строку `image` («Плата за
   образы»); evo его не использует, а BACKLOG утверждает, что публичного API
   цен нет (`docs/internal/standalone/BACKLOG.md:21`) — это надо пересмотреть.

**Почему не минимальный вариант A.** Фильтры в `list-images` без общего
резолвера оставляют UUID в `vm create`, `deploy`, `init` и трёх копиях логики
метаданных; каждая следующая правка (Astra, GPU-образы, поля продукта)
делается трижды. **Почему не L.** Локальный реестр алиасов, кэш, fuzzy-поиск
и «принятие условий» упираются в отсутствие API каталога маркетплейса и в
неизвестные правила лицензий — строить это сейчас значит угадывать.

**Что изменилось в постановке после проверки.** По документации Cloud.ru
образы маркетплейса — Linux с предустановленным ПО (Grafana, Kibana, LAMP,
LEMP, MEAN, Nginx, Prometheus, strongSwan, «Квантовый симулятор»),
распространяются **бесплатно**, платится только инфраструктура
([cloud.ru/docs/images … marketplace-images](https://cloud.ru/docs/images/ug/topics/concepts__marketplace-images)).
Публичные образы — Ubuntu 24.04/22.04/20.04, Debian 12/11, CentOS 9,
Fedora 41 ([concepts__images](https://cloud.ru/docs/virtual-machines/ug/topics/concepts__images)).
**Astra Linux в документации нет.** Гипотеза: либо он есть в живом каталоге
и документация отстаёт, либо его нет вовсе. Это первое, что должна показать
read-only проверка (§7). Платные образы при этом возможны: партнёрская
программа допускает модели BYOL, pay-as-you-go и pay-as-you-allocate
([marketplace-partner quickstart](https://cloud.ru/docs/marketplace-partner/ug/topics/quickstart)),
а `price-calculation` считает плату за образ отдельно — значит, механизм
есть, но как он срабатывает через API, не описано (§8).

## 2. Как это сделано у других

Проверено сегодня по документации: Azure (`terms`), Yandex Cloud
(`get-latest-from-family`), DigitalOcean (`doctl image …`), Linode
(Akamai API). Остальное — по памяти, помечено.

| Эталон | Найти | Запустить по имени | Маркетплейс / платные образы | Что берём |
|---|---|---|---|---|
| **Azure** | `az vm image list --publisher Canonical --offer ubuntu-24_04-lts --all -o table` (без `--all` — кэшированный короткий список) | URN `Canonical:ubuntu-24_04-lts:server:latest` или алиас `--image Ubuntu2204` | `az vm image terms show/accept --urn …` — отдельный шаг принятия условий до создания ([learn.microsoft.com](https://learn.microsoft.com/en-us/cli/azure/vm/image/terms)); цена — в карточке портала | `latest` как часть ссылки; короткие алиасы для базовых ОС; **отдельная команда принятия условий** — только если Cloud.ru даст такой API |
| **AWS** (по памяти) | `aws ec2 describe-images --owners amazon --filters "Name=name,Values=ubuntu/images/*24.04*"` | AMI ID; алиасы через SSM: `resolve:ssm:/aws/service/canonical/ubuntu/server/24.04/stable/current/amd64/hvm/ebs-gp3/ami-id` | подписка на продукт AWS Marketplace в консоли; без неё `RunInstances` отвечает `OptInRequired` | «алиас → свежий ID» как сервис, не как локальный файл; честная ошибка API, если подписки нет |
| **GCP** (по памяти) | `gcloud compute images list --filter="family~ubuntu"` | `--image-family ubuntu-2404-lts-amd64 --image-project ubuntu-os-cloud` — семейство = самый свежий не-deprecated образ | платные образы помечены лицензией в метаданных образа; плата идёт автоматически с ВМ | **семейство** как первичная ссылка; лицензия — свойство образа |
| **Yandex Cloud** | `yc compute image list --folder-id standard-images` | `yc compute image get-latest-from-family ubuntu-2404-lts --folder-id standard-images` ([docs](https://yandex.cloud/ru/docs/compute/cli-ref/image/get-latest-from-family)); `yc compute instance create --create-boot-disk image-family=…,image-folder-id=standard-images` | образы маркетплейса — публичные образы с лицензией; лицензия тарифицируется с ВМ, для некоторых нужна роль `license-manager.viewer` (по документации YC, непроверено в деталях) | ближайший аналог по рынку: семейства, публичный каталог отдельно от своего |
| **Hetzner** (по памяти) | `hcloud image list --type system` | `hcloud server create --name web --type cx22 --image ubuntu-24.04` — имя вместо ID | платных образов ОС нет; приложения — `--image` из каталога apps | **имя образа — первичный идентификатор**, ID не показывается без нужды |
| **DigitalOcean** | `doctl compute image list-distribution`, `list-application` (1-Click Apps маркетплейса), `list --public` ([docs](https://docs.digitalocean.com/reference/doctl/reference/compute/image/list-application/)) | `doctl compute droplet create web --image ubuntu-24-04-x64 --size s-1vcpu-1gb` — слаг | 1-Click Apps — тоже слаги; плата за ПО, если есть, идёт с дроплетом | **разделение списков**: дистрибутивы / приложения / свои |
| **Linode** | `linode-cli images list`; Marketplace Apps = StackScripts: `linode-cli stackscripts list --label …` ([Akamai](https://techdocs.akamai.com/quick-deploy-apps/docs/get-started)) | `linode-cli linodes create --image linode/ubuntu24.04 --stackscript_id … --stackscript_data '{…}'` | поля приложения передаются JSON-объектом `stackscript_data`; UDF описывают обязательность | **поля продукта как объект** + описание полей в `get`; аналог наших `image_metadata` |
| **Multipass** (по памяти) | `multipass find` — таблица Image / Aliases / Version / Description | `multipass launch jammy`, `multipass launch 24.04` | — | эталон «найти и запустить по имени»: алиасы в выводе списка, не в документации |
| **LXD / Incus** (по памяти) | `incus image list images:debian` | `incus launch images:debian/12 c1` — `remote:family/version` | — | иерархическая ссылка `семейство/версия`; «последняя» по умолчанию |

Общее у всех: (1) имя или семейство — первичная ссылка, UUID — для скриптов;
(2) «последняя версия семейства» — отдельное понятие; (3) каталог и свои
образы различаются явно; (4) платный маркетплейс — либо отдельный шаг
принятия условий (Azure, AWS), либо лицензия как свойство образа, которое
платится вместе с ВМ (GCP, YC). У Cloud.ru по спецификации нет ни первого
(эндпоинта принятия условий в compute нет), ни явного признака лицензии в
схеме `PublicImageResponse` — есть только `type` и цена через
`price-calculation`.

## 3. Что есть сейчас (проверено по коду)

| Факт | Где |
|---|---|
| `evo list-images` печатает весь каталог, колонки ID / Имя / ОС / Состояние; ОС и Состояние пустые — таких полей в API нет | `internal/engine/list_images.go:31-42`; `docs/internal/api-contract.md:115, :377-390` |
| Образы проекта в CLI-списке не показываются (только в MCP `list_images`) | `engine/list_images.go:17` против `tools/tools.go:2175-2191` |
| `evo vm create <name>` — инструмент `provision`, принимает только `image_id` (UUID); без него `CLOUDRU_IMAGE_ID is required` | `internal/cli/trees.go:52`; `tools/tools.go:47, :1466-1469` |
| `deploy --image-id <UUID>`; то же требование | `cli/main.go:535`; `engine/deploy.go:348-353` |
| `deploy_plan` уже принимает образ **по имени или UUID** (точное совпадение; по умолчанию Ubuntu 24.04 без GPU) — единственный путь без UUID | `deployplan/resolve.go:263-298` |
| `init` предлагает только Ubuntu/Debian по regex имени, лучшая версия первой | `engine/init.go:54-60, :213-216, :220-251` |
| `deploy` ставит Docker скриптом на ВМ, если его нет | `engine/deploy.go:224-225`; `internal/bootstrap/install.sh` (get.docker.com, BACKLOG `[sec-supply]` :91) |
| Заполнение `image_metadata` — три копии одной логики (`public_key`, `hostname`, `linux_password`, `name`; `mandatory_group`; пропуск `vmm_enabled`/`guest_agent_ready`) | `engine/deploy.go:372-430`; `tools/tools.go:1554-1613`; `deployplan/resolve.go:307-355` |
| Передать произвольное поле продукта нельзя ни одним путём | те же три места |
| `Image` не разбирает `type`, `display_name`, `description`, `min_*`, `created_time` | `provision/types.go:159-165` |
| `ImageMetadataField.Required` читает `is_required`, которого в спецификации нет; нет `display_name`, `display_position`, `display_group`, `mandatory_group_display_name` | `provision/types.go:263-271`; спецификация `MiniImageMetadataResponse` |
| Пагинация `limit=100` по `offset` до `total` — есть | `provision/discovery.go:17-20, :38-90` |
| Создание ВМ идёт по `image_name`, не по `image_id` (UUID давали 500) | `provision/types.go:54-67`; `engine/deploy.go:471`; `tools/tools.go:1691` |
| `provision` пишет ВМ в state, `destroy <vm_id|name>` её находит | `tools/tools.go:1740-1760`; `cli/trees.go:53` |
| Цена — зашитая карта, лицензию не учитывает | `internal/pricing/pricing.go:1-16` |
| Коды выхода: не найдено — 4, аргументы отвергнуты до облака — 1 `USAGE`, API — 5 | `docs/exit-codes.md`; DECISIONS D11 |

### 3.1 Что даёт API (спецификация compute, скачана `scripts/cloudru-specs.py` 16.10.2026)

`GET /api/v1/images` — параметры: `order_by` (только `created_time`),
`order_desc`, `offset`, `limit` (max 100, default 50), `availability_zone_id`,
`name` (1–64 символа, «системное название»), `display_name`, `type`
(`public|private|marketplace`), `enabled`, `need_gpu`, `project_id`,
`free_tier_enabled`. **Непроверено:** `name`/`display_name` — точное
совпадение или подстрока; регистр. Это решает, где делать поиск — на сервере
или у себя после полной выгрузки (86 образов = одна страница,
`api-contract.md:692`).

`PublicImageResponse`: `id, name, display_name, description, project_id,
icon (SVG!), min_cpu, min_ram, min_disk, min_gpu, public, type,
user_data_template, image_metadata[], created_time, modified_time,
availability_zones[] {availability_zone_id, availability_zone_name, default,
enabled, size, state}`. Семейства ОС как поля нет — только `name`.

`image_metadata[]` (`MiniImageMetadataResponse`): `id, name, display_name,
display_position, field_type, display_group, default_value, sensitive,
mandatory_group, mandatory_group_display_name`. `sensitive: true` — «значение
используется однократно при создании ВМ и не сохраняется в её свойствах».
`field_type` при создании образа — enum `info, text, linux_login,
plain_password, linux_password, email, public_key, hostname`
(`api-contract.md:117`).

`POST /api/v1.1/vms`: `image_id` **или** `image_name` (не оба);
`image_metadata` — объект «имя поля → строка» (до 3072 символов); 422 с
текстами «Обязательные для выбранного образа метаданные отсутствуют», «Не
заполнены обязательные поля с метаданными для образа», «Ошибка в метаданных
виртуальной машины». Также документировано поле `cloud_init` (строка-шаблон) —
BACKLOG `[05]` (:35) считал его неподтверждённым; спецификация его описывает,
вживую не проверено.

`POST /api/v1/projects/{project_id}/price-calculation`: тело
`{total_count, flavor_id, image_id, disks[{disk_type_id,size}],
floating_ips[{floating_ip_action,count}], nat_gateways[]}`, ответ `{flavor,
image, disks, floating_ips, nat_gateways, total_price_month,
total_price_hour}`. Эндпоинта принятия условий или сведений о лицензии в
compute нет.

## 4. Варианты

| | A. Фильтры и имя в `provision` | **B. Каталог как ядро (рекомендуется)** | C. Слой продуктов |
|---|---|---|---|
| Суть | `list-images --type/--name`, `vm create --image <имя|UUID>` точным совпадением (как `deploy_plan`) | §1: `image list/get`, резолвер семейств, `--meta`, общий пакет `internal/images`, превью с полями и ценой | B + локальный реестр алиасов (`astra` → UUID), кэш каталога на диске, fuzzy-поиск с меню, `image terms accept`, карточка продукта |
| Плюсы | день работы, без новых понятий | закрывает вопрос владельца; убирает три копии; готов к появлению Astra/GPU/платных образов без переделки | максимально «как у Azure» |
| Минусы | UUID остаётся в `deploy`/`init`/конфиге; метаданные продукта всё ещё не передать; четыре места с разной логикой | новый пакет и миграция трёх путей — риск регрессии в `deploy` (есть e2e) | строит механизмы под API, которого нет (каталог, условия, цена лицензии); алиасы в файле устаревают и расходятся с каталогом |
| Стоимость | S (1–2 дня) | M (4–6 дней с тестами и docs) | L (2+ недели, часть блокирована §8) |
| Риск | низкий, но долг растёт | средний | высокий |

Оценки — мнение исполнителя, не обязательство.

### 4.1 Вариант B: макеты сессии (вывод на русском, по D6)

Поиск по каталогу:

```text
$ evo image list ubuntu
ИМЯ            ОТОБРАЖАЕМОЕ ИМЯ   ТИП      ЗОНЫ          МИН. ДИСК  СЕМЕЙСТВО
Ubuntu-24.04   Ubuntu 24.04 LTS   public   AZ-1, AZ-2    10 ГБ      ubuntu (последний)
ubuntu-22.04   Ubuntu 22.04 LTS   public   AZ-1, AZ-2    10 ГБ      ubuntu
ubuntu-20.04   Ubuntu 20.04 LTS   public   AZ-2          10 ГБ      ubuntu
Показаны 3 из 86. UUID — в --json или 'evo image get <имя>'.

$ evo image list --type marketplace
ИМЯ               ОТОБРАЖАЕМОЕ ИМЯ        ТИП          ЗОНЫ        МИН. ДИСК  ПОЛЯ
grafana-11        Grafana                 marketplace  AZ-1, AZ-2  20 ГБ      3 (1 обязательное)
strongswan-5.9    strongSwan VPN          marketplace  AZ-2        10 ГБ      5 (2 обязательных)
…

$ evo image list astra
Образы не найдены: «astra» не встречается в каталоге (86 образов).
Проверьте написание или посмотрите каталог целиком: evo image list
$ echo $?
4
```

Имена образов в макетах условные: реальные — из каталога, read-only
проверка §7 покажет их. Столбец «ЗОНЫ» — зоны, где
`availability_zones[].state == created && enabled`; «ПОЛЯ» — число полей
`image_metadata` минус те, что evo заполнит сам.

Карточка образа — то, что человеку нужно до создания:

```text
$ evo image get grafana-11
Образ:        grafana-11 (Grafana), тип marketplace
Описание:     Grafana 11 на Ubuntu 22.04: дашборды, алерты …
Минимум:      2 vCPU, 4 ГБ RAM, диск 20 ГБ
Зоны:         ru.AZ-1 (готов), ru.AZ-2 (готов)
Поля при создании:
  name            логин        evo заполнит сам (по умолчанию user1)
  public_key      ключ SSH     evo заполнит сам (~/.evo/…)
  linux_password  пароль       evo создаст случайный (сохраняется только если нет ключа)
  admin_password  секрет       ОБЯЗАТЕЛЬНО, --meta admin_password=… или вопрос на терминале
  domain          текст        необязательно, по умолчанию пусто
Лицензия/цена:  по документации Cloud.ru образы маркетплейса бесплатны; evo
                считает только ВМ, диск и IP. Проверьте карточку продукта.
Создать ВМ:     evo vm create grafana-1 --image grafana-11 --meta admin_password=…
UUID:           6a1f…  (нужен только скриптам)
```

Создание по имени и семейству:

```text
$ evo vm create web-1 --image ubuntu
План: создать ВМ web-1 из образа Ubuntu-24.04 (ubuntu, последняя версия) в ru.AZ-2
  Проект:   my-project (…a1b2)
  Flavor:   lowcost10-2-4 (2 vCPU, 4 ГБ)
  Диск:     SSD 10 ГБ
  Сеть:     подсеть default-AZ-2, floating IP — новый
  Поля:     name=user1, hostname=web-1, public_key=~/.evo/id_ed25519.pub, linux_password=•••• (случайный)
  Цена:     ≈ 2 044 ₽/мес (оценка evo; API расчёта цены: ВМ 1 971, диск 73, образ 0)
Создать? [y/N]
```

Неоднозначность — без терминала или с `--json`:

```text
$ evo vm create web-1 --image debian --non-interactive --json
{"error":{"code":1,"reason":"USAGE","message":"образ «debian» неоднозначен: debian-12, debian-11","hint":"Укажите точное имя (--image debian-12) или семейство с версией. Список: evo image list debian"}}
```

Мнение: семейство без версии (`ubuntu`, `debian`) **разрешать** в «последнюю
версию», как GCP и YC, но только для образов `type=public`, у которых версия
читается из имени. Для `marketplace` ссылки разрешаются только точным
`name`/`display_name`: продукты одного семейства — разные вещи, и
«последний Grafana» угадывать нельзя. Поэтому `debian` в примере выше —
это не ошибка семейства, а случай, когда версию из имён вытащить нельзя
(например, `debian-12` и `debian-12-gpu`); тогда честный отказ.

Неоднозначность на терминале — меню, как в `init` (`engine/init.go:183-208`):

```text
$ evo vm create vpn-1 --image strongswan
Образов с именем «strongswan» несколько:
  1) strongswan-5.9      strongSwan VPN             marketplace  AZ-2
  2) strongswan-5.9-gpu  strongSwan VPN (GPU)       marketplace  AZ-1
Выберите [1]:
```

Не найдено — код 4, как у любого объекта (`NOT_FOUND`):

```text
$ evo vm create db-1 --image astra-1.8
Ошибка: образ «astra-1.8» не найден ни в каталоге, ни в проекте my-project
Похожие: (нет). Список: evo image list; свои образы: evo image list --type private
$ echo $?
4
```

Обязательное поле не заполнено (до обращения к облаку — `USAGE`, код 1,
по D11):

```text
$ evo vm create mon-1 --image grafana-11 --yes
Ошибка: у образа grafana-11 не заполнено обязательное поле admin_password (секрет)
Передайте --meta admin_password=… , --meta-file поля.yaml или запустите без --yes на терминале — evo спросит
```

На терминале без `--yes` — вопрос, ввод секрета скрыт:

```text
$ evo vm create mon-1 --image grafana-11
Поле admin_password (секрет, обязательное): ********
Поле domain (текст, необязательно) [пусто]:
План: создать ВМ mon-1 из образа grafana-11 (Grafana, marketplace) в ru.AZ-2
  …
  Поля:   name=user1, hostname=mon-1, public_key=~/.evo/id_ed25519.pub, admin_password=•••• (введён), domain=(пусто)
  Лицензия: по документации Cloud.ru образы маркетплейса бесплатны; плата за образ по API расчёта цены: 0 ₽
Создать? [y/N] y
✔ ВМ mon-1 создана: 1.2.3.4 (ru.AZ-2)
  ssh user1@1.2.3.4 -i ~/.evo/id_ed25519
  Пароль не сохранён: вход по ключу. Удалить: evo vm delete mon-1
```

Если у образа нет поля для ключа и вход только по паролю, сгенерированный
пароль печатается **один раз** в результате (как `deploy_apply` сейчас,
`tools/tools.go:179`) и не пишется в state. Отпечаток хоста при первом
`ssh` — как в `deploy` (`SSH_HOST_KEY_CHANGED` уже есть в кодах,
`internal/errors/codes.go:15`); в `vm create` отдельной проверки нет и в v1.0
не добавляется — ВМ создаётся без входа на неё.

`--meta` в командной строке попадает в историю шелла. Правило: для полей
`sensitive` или `field_type ∈ {linux_password, plain_password}` печатать
предупреждение и предлагать `--meta-file`, переменную `EVO_META_<ИМЯ>` или
вопрос на терминале. В `--json`-превью значение секрета — `"••••"`, в state
и логе `--verbose` — не пишется.

### 4.2 `deploy` и `init`

`deploy` обещает Docker через `get.docker.com`, который поддерживает Ubuntu,
Debian, CentOS, Fedora, RHEL и ещё несколько (по памяти; Astra Linux и
ALT там нет). Честное правило:

1. Если на ВМ уже есть `docker compose` — ставить не надо (сейчас так и есть,
   `deploy.go:224`). Это покрывает образы маркетплейса с предустановленным
   Docker.
2. Иначе семейство образа должно быть в списке поддерживаемых скриптом;
   v1.0 обещает **только Ubuntu и Debian** (проверены e2e), CentOS/Fedora —
   с предупреждением «не проверялось».
3. Иначе отказ **до создания ВМ**: `CONFIG_INVALID`, код 2, «образ X не
   подходит для deploy: установка Docker проверена только на Ubuntu и Debian.
   Создайте ВМ `evo vm create --image X` и поставьте Docker сами, или возьмите
   образ маркетплейса с Docker». Сейчас отказ случается уже после создания и
   оплаты ВМ (`docker_failed`, `deploy.go:227`).

`init --image <имя|семейство>` — флаг вместо меню; меню остаётся и предлагает
семейства с версиями из каталога (не только Ubuntu/Debian — все `public`,
но помечает, какие годятся для `deploy`). В `.evo.yaml` пишется `image:
Ubuntu-24.04` (имя), `image_id` остаётся читаемым для старых файлов
(`internal/config/config.go:36, :144`).

### 4.3 Кэш каталога

Не нужен в v1.0: каталог — одна страница (86 образов), один запрос, а
флейворы уже кэшируются час в адаптере (`api-contract.md §4.7`). Кэш на
диске появится, только если `vm create` станет заметно медленнее из-за
`image list` + `image get` + `price-calculation` (три запроса) — тогда
`~/.evo/cache/images.json` на 10 минут и `--refresh`. Мнение: не делать
заранее.

## 5. Изменения по слоям

**`internal/provision`.**
- `Image`: добавить `DisplayName`, `Description`, `Type`, `MinCPU/MinRAM/MinDisk/MinGPU`,
  `CreatedTime`, `Public`; убрать мёртвые `OS`/`Status` (api-contract §4.6).
  `ReadyIn(zoneID)` рядом с `Usable()` (`types.go:171-185`).
- `ImageMetadataField`: `DisplayName`, `DisplayPosition`, `DisplayGroup`,
  `MandatoryGroupDisplayName`; `Required` из `is_required` убрать — поля нет
  в спецификации. Обязательность = `mandatory_group != ""` **или**
  (`default_value == ""` и поле не статусное) — это гипотеза, уже заложенная
  в `deployplan/resolve.go:329`; подтвердить вживую (§7).
- `ListImages(ctx, ImageFilter{Type, ZoneID, Name, DisplayName, Enabled, GPU, FreeTier, ProjectID})`
  — серверные параметры из спецификации поверх существующего `listAll`.
- `PriceCalculation(ctx, projectID, PriceReq) (PriceResp, error)` — новый метод.
  Падение → предупреждение и оценка по карте, не ошибка.

**Новый `internal/images`** (ядро, без вывода и без cobra):
- `Catalog{Shared, Project []Image}`; `Load(ctx, cloud, projectID, filter)`.
- `Resolve(ref string) (Image, []Image /*кандидаты*/, error)`: UUID → точное
  `name` (без учёта регистра) → `display_name` → семейство (`family[-version]`
  по regex имён `public`-образов, версия — максимум) → ошибка `NOT_FOUND`
  (0 кандидатов) или `USAGE` «неоднозначен» (>1). Подстрока — только в
  `Search`, не в `Resolve`.
- `Family(img) (name, version, ok)` — переносится `deployableImages`
  (`init.go:213-251`) и расширяется CentOS/Fedora.
- `FillMetadata(detail, inputs map[string]string, auto AutoValues) (meta, missing []Field, filled []Filled, err)`
  — одна реализация вместо трёх. `auto` — ключ, имя хоста, логин, генератор
  пароля. `missing` — обязательные без значения (для вопроса или ошибки).
  `Filled` помнит источник (`auto`, `flag`, `file`, `prompt`, `default`) и
  `Sensitive` — для превью с маскированием.
- `DockerInstallable(img) (ok bool, tested bool)` — для `deploy`/`init`.

**`internal/engine`.** `Deploy` и `init` зовут `images.Resolve` и
`FillMetadata`; `Config` получает `Image string` (`image:` в YAML,
`CLOUDRU_IMAGE`), `ImageID` остаётся как синоним на один минорный релиз.
`ListImages` → таблица из §4.1, объединённая с образами проекта (как в MCP).

**`internal/tools`.**
- `image_list` (новый; `list_images` — алиас на релиз): аргументы `query`,
  `type`, `zone`, `gpu`, `limit` (по умолчанию 50); ответ — компактные строки
  `{id, name, display_name, type, zones_ready, min_disk_gb, family, fields_required}`
  **без** `availability_zones[]`, `icon` и `user_data_template` — сейчас
  `list_images` отдаёт `Image` целиком с зонами (`tools.go:2192`), а `image_get`
  — SVG-иконку и весь Jinja-шаблон, это сотни строк контекста агента.
- `image_get`: аргумент `image` (имя|семейство|UUID), флаг `full=true` для
  шаблона и иконки; по умолчанию — карточка из §4.1 в JSON (поля с
  `required`, `sensitive`, `filled_by_evo`).
- `provision`, `deploy`, `deploy_plan`: аргумент `image` (имя|семейство|UUID),
  `image_id` — синоним; `image_metadata: {k: v}`. Превью
  (`confirmation_required`) включает `image: {name, type}`, `metadata_fields:
  [{name, source, sensitive}]` без значений секретов, `price: {source: "api"|"estimate", image: …}`.
  Неоднозначность → `isError: true`, `reason: USAGE`, `candidates: [...]`;
  не найдено → `NOT_FOUND`. Пропущенное обязательное поле → `USAGE` с
  `missing_fields` — агент добирает у человека; в v1.1 — через MCP
  elicitation (D10).

**`internal/cli`.** Дерево `image` в `trees.go`: `list` (`image_list`, колонки
из §4.1), `get` (`image_get`, Args `image`). `vm create`: `--image`
(из схемы инструмента автоматически), `--meta k=v` (повторяемый),
`--meta-file`. `list-images` и `--image-id` — скрытые синонимы с
предупреждением, удаление — v1.1 (как `cloudru-vm`, D2). Вопросы на терминале —
через `confirm.ReadLine` и скрытый ввод для секретов (`golang.org/x/term`
уже в зависимостях — `confirm.go:28`).

**Общее с ядром создания ВМ.** Все три пути сходятся на `images.Resolve` →
`images.FillMetadata` → `provision.CreateVMRequest{ImageName, ImageMetadata}`
→ `confirm.Preview`. Это не четвёртый путь, а вынос общего из трёх.
Полное слияние трёх путей в один `engine.CreateVM` — отдельная работа
(BACKLOG `[cli-practice]`), здесь не делается.

## 6. План и риски

**v1.0 этой функции (M):**
1. `provision`: поля образа и метаданных по спецификации, `ImageFilter`,
   `PriceCalculation` (с живой проверкой §7 до включения в превью).
2. `internal/images`: каталог, резолвер, `FillMetadata`; тесты на
   неоднозначность, семейства, `mandatory_group`, маскирование.
3. `evo image list/get`, `vm create --image/--meta`, `deploy --image`,
   `init --image`; синонимы старых флагов.
4. Превью с полями и ценой; отказ `deploy` до создания ВМ на неподходящем
   образе.
5. `docs/commands.md`, `docs/mcp.md`, `docs/exit-codes.md` (новых кодов нет —
   используются 1/4/5), BACKLOG: снять `[audit]` «публичного API цен нет»
   после проверки §7.

**v1.1:**
- `--meta-file`, `EVO_META_<ИМЯ>`, вопросы на терминале (в v1.0 — только
  `--meta` и ошибка `USAGE`, если на терминале спрашивать не удалось).
- MCP elicitation для пропущенных полей (D10).
- Кэш каталога — по измерениям.
- Формулировка про лицензию в превью — после ответов Cloud.ru (§8).
- Удаление `list-images`, `--image-id`, `image_id` в конфиге.

**Риски.**
- Регрессия `deploy` при выносе `FillMetadata`: три копии различаются
  деталями (логин по умолчанию `ubuntu` в `deploy.go:405` против `user1`
  в `tools.go:1583` и `resolve.go:308`). Снимается e2e и тестом на
  идентичность запроса для Ubuntu-24.04.
- Семейство «последняя версия» выберет образ, который `enabled: false` в
  зоне деплоя — фильтровать по `ReadyIn(zone)` до выбора версии.
- Серверный `name=` может оказаться точным совпадением — тогда поиск по
  подстроке делаем у себя после полной выгрузки (одна страница); на
  поведение CLI не влияет.
- `price-calculation` может требовать прав, которых нет у сервисного
  аккаунта с ролями на чтение — превью должно деградировать до оценки, не
  падать.
- Астры может не быть вовсе: тогда ответ владельцу — «Astra через CLI
  развернуть нельзя, потому что её нет в каталоге; как только появится,
  команда `evo vm create --image astra-…` заработает без изменений кода».

## 7. Read-only проверка на живом каталоге

`.github/scripts/cloud-readonly.sh` в `main` нет; есть `api-probe.yml`
(только GET, в лог — коды, имена полей и количества). Предлагаемые пробы
туда же (`probe NAME URL JQ`, `.github/workflows/api-probe.yml:55-61`); в лог
попадают счётчики и **публичные имена каталога** (образы `type=public|marketplace`
без `project_id` — публичны по определению), никаких ID и данных проекта:

```sh
echo "== images: типы и фильтры"
probe images_total           "$C/images?limit=100" '{total, n: (.items|length), types: ([.items[].type]|group_by(.)|map({(.[0]): length})|add)}'
probe images_marketplace     "$C/images?type=marketplace&limit=100" '{total, names: [.items[].name]}'
probe images_public_names    "$C/images?type=public&limit=100" '{total, names: [.items[].name]}'
probe images_astra           "$C/images?limit=100" '[.items[] | select((.name + " " + .display_name) | test("astra|alt|redos|red os"; "i")) | .name]'
probe images_name_exact      "$C/images?name=Ubuntu-24.04&limit=100" '{total, names: [.items[].name]}'
probe images_name_substr     "$C/images?name=ubuntu&limit=100" '{total}'          # 0 → точное совпадение; >1 → подстрока
probe images_name_case       "$C/images?name=ubuntu-24.04&limit=100" '{total}'    # регистр
probe images_display_name    "$C/images?display_name=Ubuntu&limit=100" '{total}'
probe images_enabled         "$C/images?enabled=true&limit=100" '{total}'
probe images_gpu             "$C/images?need_gpu=true&limit=100" '{total}'
probe images_zone_filter     "$C/images?availability_zone_id=$ZONE_ID&limit=100" '{total}'   # ZONE_ID — из /availability-zones в этом же job, маскировать
probe images_order_asc       "$C/images?order_desc=false&limit=1" '[.items[].created_time | type]'
probe images_project_only    "$C/images?$P&limit=100" '{total, types: [.items[].type]|unique}'

echo "== image_metadata образов маркетплейса (имена и типы полей, без id)"
# id образа — только в URL запроса; в лог идёт .name и список полей
for id in $(curl -sS -H "Authorization: Bearer $TOKEN" "$C/images?type=marketplace&limit=100" | jq -r '.items[].id'); do
  probe meta "$C/images/$id" '{name, fields: [.image_metadata[] | {name, field_type, sensitive, mandatory_group, has_default: (.default_value != "")}], template_len: (.user_data_template|length), icon_len: (.icon|length)}'
done

echo "== price-calculation: форма ответа"
# POST с flavor_id и image_id из тех же списков (по одному public и одному
# marketplace); в лог — http, keys и флаг (.image > 0), сами суммы не печатать.
```

Пробы на `cloud_init` в `POST /vms` и на 422-тексты по метаданным — только в
`cloud-e2e` (создают ВМ), не здесь.

## 8. Вопросы

**Владельцу:**
1. Astra Linux — реальный пример или собирательный? Если реальный: он в
   каталоге вашего тенанта (тип `marketplace`?) или это образ, который ещё
   предстоит загрузить как `private`? Проба §7 ответит про каталог.
2. Согласны ли на правило «семейство без версии → последняя, но только для
   `public`»? Альтернатива — всегда требовать точное имя (строже, скучнее).
3. `deploy` на не-Ubuntu/Debian: отказывать до создания ВМ (предложено) или
   пробовать с предупреждением?
4. Удаление `list-images`/`--image-id`/`image_id` в v1.1 — приемлемо?
5. Нужен ли `--meta` в v1.0 или достаточно `image get` + ошибка «заполните в
   консоли» до v1.1? (Мнение: `--meta` нужен, иначе образы маркетплейса с
   обязательными полями остаются недоступны из CLI.)

**Командам Cloud.ru (compute, маркетплейс, биллинг):**
1. `GET /api/v1/images?name=…` и `display_name=…` — точное совпадение или
   подстрока? Чувствительность к регистру? Планируется ли `order_by=name`?
2. Есть ли или планируется признак семейства/версии ОС у образа (поле вроде
   `os_family`, `os_version`) — сейчас семейство читается только из `name`?
3. Платные образы маркетплейса (модели PAYG/PAYA по партнёрской программе):
   как плата включается при `POST /api/v1.1/vms` — автоматически, без
   отдельного принятия условий? Есть ли эндпоинт принятия оферты/EULA, и
   отвечает ли API 4xx, если условия не приняты? Как это отличить от других
   422?
4. `POST /api/v1/projects/{project_id}/price-calculation`: поле `image` — это
   плата за лицензию образа? Какие роли нужны сервисному аккаунту? Можно ли
   считать его публичным источником цены для превью (BACKLOG :21 считает,
   что API цен нет)?
5. Как в ответе образа понять, что он платный, не вызывая
   `price-calculation` для каждого (признак в `PublicImageResponse`)?
6. `image_metadata`: правило обязательности — `mandatory_group` плюс пустой
   `default_value`? Есть ли валидация по `field_type` на сервере (формат
   `public_key`, сложность `linux_password`) и какие тексты 422 ей
   соответствуют?
7. `cloud_init` в `POST /api/v1.1/vms` описан в спецификации — работает ли он
   поверх `user_data_template` образа или вместо него? (BACKLOG `[05]`.)
8. Будет ли публичный API каталога маркетплейса (категории, карточка, способ
   поставки, цена, ссылка на документацию продукта)? Без него `image get`
   показывает только то, что есть в compute.
9. Появится ли Astra Linux / ALT / РЕД ОС в публичном каталоге или
   маркетплейсе Evolution, и по какой модели лицензирования?

## Источники

- Спецификация compute `openapi-v3.yaml` (скачана `scripts/cloudru-specs.py`,
  реестр: https://cloud.ru/docs/console_api/ug/topics/overview__reestr_api).
- https://cloud.ru/docs/images/ug/topics/concepts__marketplace-images
- https://cloud.ru/docs/virtual-machines/ug/topics/concepts__images
- https://cloud.ru/docs/marketplace-partner/ug/topics/quickstart
- https://cloud.ru/docs/virtual-machines/ug/topics/pricing
- https://learn.microsoft.com/en-us/cli/azure/vm/image/terms
- https://yandex.cloud/ru/docs/compute/cli-ref/image/get-latest-from-family
- https://docs.digitalocean.com/reference/doctl/reference/compute/image/list-application/
- https://techdocs.akamai.com/quick-deploy-apps/docs/get-started
- `docs/internal/api-contract.md`, `docs/internal/standalone/BACKLOG.md`,
  `docs/internal/standalone/DECISIONS.md`, `docs/exit-codes.md`.
