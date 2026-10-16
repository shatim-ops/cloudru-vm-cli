# Cloud.ru Evolution: документация и спецификации против живого API

Реестр расхождений, найденных при разработке evo-cli (Go CLI и MCP-сервер для
Cloud.ru Evolution). Адресаты — продуктовые команды Cloud.ru. Документ ведётся
и дальше: новое расхождение — новый пункт с доказательством, снятое — в раздел
«Снятые вопросы».

- **Период наблюдений:** 14–16.10.2026.
- **Как получены факты:** GitHub Actions репозитория `shatim-ops/evo-cli`:
  `api-probe` (только GET и расчёт цены, в лог — коды HTTP, имена полей,
  счётчики) и `cloud-e2e` (создание и удаление ВМ и кластера Kubernetes,
  задания `readonly`, `e2e`, `os-smoke`, `k8s-smoke`). Ключ — сервисный
  аккаунт уровня проекта, один тестовый проект.
- **Спецификации:** скачаны со страниц справочников API 15 и 16.10.2026 (реестр
  — [overview__reestr_api](https://cloud.ru/docs/console_api/ug/topics/overview__reestr_api));
  sha256 спецификаций compute и mk8s в эти дни не менялся
  (`45cf7cbcdde2`, `e8186cd966a5`).
- **Что не публикуется:** ID проектов, ВМ, кластеров, IP, групп безопасности,
  ключей и имена ресурсов тестового проекта. Имена образов общего каталога и
  плагинов mk8s публичны и приводятся.

**Классы:** «расхождение» — документация или спецификация говорит одно, API
делает другое; «нет в документации» — поведение есть, описания нет; «ошибка
API» — ответ неверен независимо от документации; эксплуатационные наблюдения —
отдельным разделом.
**Статусы:** «подтверждено вживую» — со ссылкой на прогон, дату и код HTTP;
«гипотеза» — с пробой из раздела [«Что проверить пробой»](#что-проверить-пробой).

Ссылки на спецификации ниже сокращены: `VM-spec` —
`https://cloud.ru/docs/api/specs/virtual-machines/ug/_specs/openapi-v3.yaml`,
`mk8s-spec` — `…/kubernetes-evolution/ug/_specs/openapi-v3.yaml`, остальные
— `https://cloud.ru/docs/api/specs/<сервис>/ug/_specs/<файл>`.

## Сводка

| Команда | Пунктов | Подтверждено | Гипотез | Главная проблема |
|---|---|---|---|---|
| [Образы и Маркетплейс](#образы-и-маркетплейс) | 5 | 5 | 0 | 23 из 86 образов общего каталога, включая все отечественные ОС, проекту недоступны; создание ВМ — 422 «Image … not found» |
| [Биллинг, расчёт цены](#биллинг-и-расчёт-цены) | 4 | 3 | 1 | `price-calculation` даёт 0 за лицензию у всех образов маркетплейса, хотя у Windows Server на карточке 1 006 ₽/мес |
| [Compute (ВМ)](#compute-вм) | 8 | 8 | 0 | обязательное поле интерфейса `interface_security_enabled` отсутствует в спецификации с `additionalProperties: false` |
| [Managed Kubernetes](#managed-kubernetes) | 5 | 5 | 0 | `addonNamespace` релиза плагина повторяет `addonVersion`; инструкция «Установить плагин → API» описывает устаревший v2 |
| [IAM и Organization](#iam-и-organization) | 3 | 3 | 0 | сервисный аккаунт проекта не может узнать свой проект: `GET /v3/projects` → 400 «user not found» |
| [Аудит-логирование](#аудит-логирование) | 2 | 2 | 0 | 401 вместо 403 при отсутствии доступа и 415 на лишний параметр |
| [VPC, DNS, группы безопасности, SNAT](#vpc-dns-группы-безопасности-snat) | 3 | 2 | 1 | snake_case принимается при camelCase в спецификации — не ясно, гарантия ли это |
| [DBaaS: Kafka, Redis, PostgreSQL](#dbaas-kafka-redis-postgresql) | 4 | 1 | 3 | списки кластеров и спецификаций Kafka/Redis отвечают ошибкой, хотя запросы совпадают со спецификацией |
| [Мониторинг](#мониторинг) | 1 | 0 | 1 | единицы `start`/`end` и формат ответа `export_metrics` не описаны |
| [Foundation Models](#foundation-models) | 1 | 1 | 0 | токен IAM отклоняется сообщением про API-ключ |
| [Портал документации](#портал-документации) | 1 | 1 | 0 | ответ cloud.ru/docs зависит от User-Agent: без него — 403 |
| Container Apps, Artifact Registry, NLB, Secret Manager, Certificate Manager | 0 | — | — | расхождений не найдено; вживую проверены только списки (cloud-e2e #48, `readonly`) |
| **Итого** | **37** | **31** | **6** | |

Пять пунктов с наибольшим влиянием: [IMG-1](#img-1-часть-общего-каталога-недоступна-проекту-ошибка-выдаётся-как-image-not-found),
[BIL-1](#bil-1-лицензия-образа-маркетплейса-в-расчёте-цены-всегда-0),
[CMP-1](#cmp-1-interface_security_enabled-обязательно-но-его-нет-в-спецификации),
[K8S-1](#k8s-1-addonnamespace-повторяет-addonversion),
[IAM-3](#iam-3-сервисный-аккаунт-проекта-не-получает-список-проектов).

## Образы и Маркетплейс

#### IMG-1. Часть общего каталога недоступна проекту, ошибка выдаётся как «Image not found»
Нет в документации + ошибка API · подтверждено вживую
- **Док:** [Образы Маркетплейса](https://cloud.ru/docs/images/ug/topics/concepts__marketplace-images): «Нужный образ можно выбрать при создании виртуальной машины». Об условиях доступности образа проекту — ни слова; в `VM-spec` у `GET /api/v1/images` параметр `project_id` описан только как фильтр.
- **Живое:** api-probe #8 (16.10, 17:25 UTC): `GET /api/v1/images` без `project_id` — 86 общих образов; с `project_id` — 63 общих + собственные. Не видны 23, в том числе все Astra Linux (`AstraLinuxSmolensk1.8.3KES`, `astra_1.8.1`, `astra-voronej` …), Альт (`alt_8_sp_9`, `alt_10.2.1` …), РЕД ОС (`redos-8.0`, `redos_7.3_image`, `REDOS7.3-sert`), `SberlinuxOSServe9.1`, а также образы типа `public`: `Windows_11_vdi`, `Ubuntu_24_04_Desktop`, `vyos`, `pt-ngfw-vm-1.11.1`. `GET /images?project_id=…&name=AstraLinuxSmolensk1.8.3KES` → 200, `total=0`.
- **Живое (создание):** cloud-e2e #31, job `os-smoke` (16.10, 11:52 UTC): `POST /api/v1.1/vms` с `AstraLinuxSmolensk1.8.3KES` и `redos_7.3_image` → 422 `[{"message":"Image with this id is not found","code":"wrong_image"}]`, хотя образ есть в каталоге.
- **Влияние:** ВМ на отечественных ОС через API не создаётся, а текст ошибки указывает не на причину (доступ), а на опечатку. Автоматизация не может отличить «нет образа» от «образ не подключён».
- **Просим:** описать, что делает образ общего каталога доступным проекту (подключение продукта, лицензия, роль, регион); отдельный код ошибки (например, `image_not_available_for_project`); признак доступности в ответе `GET /images`.

#### IMG-2. «Маркетплейс — только Linux и бесплатно», а в каталоге Windows Server с платной лицензией
Расхождение · подтверждено вживую
- **Док:** [Образы Маркетплейса](https://cloud.ru/docs/images/ug/topics/concepts__marketplace-images): «Маркетплейс включает образы на основе ОС Linux…», «Образы из Маркетплейса распространяются бесплатно. Оплачивается только используемая инфраструктура». [Тарификация образов](https://cloud.ru/docs/images/ug/topics/pricing): «Сервис предоставляется бесплатно».
- **Живое:** api-probe #8: в `type=marketplace` 60 образов, среди них `windows2019std-evo-kmsB`, `windows2019dc-evo-kmsB`, `windows2022std-evo-kmsB`, `windows2022dc-evo-kmsB`, лицензируемые Astra Linux KES и РЕД ОС KES. Карточка [Windows Server Standard 2022](https://cloud.ru/marketplace/products/ms-std-22) (16.10): «1 006 ₽ в месяц», «Независимо от даты запуска продукта оплачивается месяц его использования».
- **Влияние:** пользователь, прочитавший документацию образов, не ожидает платы за лицензию.
- **Просим:** привести страницу «Образы Маркетплейса» и «Тарификация» в соответствие каталогу; указать, какие образы платные и как начисляется лицензия.

#### IMG-3. Публичный каталог содержит тестовые и сломанные образы
Расхождение · подтверждено вживую
- **Док:** [Публичные образы](https://cloud.ru/docs/images/ug/topics/concepts__public-images): Ubuntu 24.04/22.04/20.04, Debian 12/11, CentOS 9, Fedora 41.
- **Живое:** api-probe #8: `type=public` — 26 образов, в том числе `ubuntu-test2204`, `test-fix-ostrov`, `ostrov-fix`, `img-nester-3110-test-1`, `nester0807`, `debian-11_error`, `osbc_111p66_b3`, `Win25Std-INC-16033`; в маркетплейсе — образ с именем `null` и `alt-test`. `enabled=true` оставляет 78 из 86.
- **Влияние:** образы видны всем клиентам; поиск по подстроке (`name=ubuntu` → 17) выдаёт тестовые образы рядом с рабочими.
- **Просим:** убрать тестовые образы из общего каталога или скрыть их флагом; обновить список на странице публичных образов (GPU-, NVLINK-, desktop-варианты в документации не упомянуты).

#### IMG-4. Метаданные образов: тип `flag` вне enum, разнобой `mandatory_group`, пароль без `sensitive`
Расхождение · подтверждено вживую
- **Спека:** `VM-spec`, `MetaType`: `info, text, linux_login, plain_password, linux_password, email, public_key, hostname`. `sensitive`: «Если указано `true`, значение поля используется однократно при создании ВМ и не сохраняется в ее свойствах».
- **Живое:** api-probe #8, `GET /api/v1/images/{id}` для всех 60 образов маркетплейса: `field_type: "flag"` у `guest_agent_ready`/`vmm_enabled` (в enum нет); `mandatory_group` бывает `"authentication"`, `" authentication"`, `"Метод аутентификации"`, `" Да"`, `""`, `null`; у Windows-образов поля `linux_login`/`linux_password`, у `windows2019std-evo-kmsB` — `plain_password`; у `ngfw` и `platform-v-pangolin-se-demo` `linux_password` с `sensitive: false`.
- **Влияние:** клиент не может надёжно определить обязательные поля и сгруппировать их; пароль ВМ из двух образов по спецификации сохраняется в её свойствах.
- **Просим:** добавить `flag` в `MetaType` (или привести данные к enum); нормализовать `mandatory_group`; проверить `sensitive` у паролей в каталоге.

#### IMG-5. Нет признака семейства и версии ОС
Нет в документации · подтверждено вживую
- **Спека:** у образа нет полей ОС; верхнеуровневых `os`/`status` нет и в ответе (api-probe #1, 15.10).
- **Живое:** api-probe #2–#4, #8: имена отечественных ОС разнобойные (`AstraLinuxSmolensk1.8.3KES`, `astra_1.8.1`, `alse-base-1-7-7`); `display_name` по-русски и по-английски.
- **Влияние:** «последняя Astra Linux» или «последняя Ubuntu LTS» автоматически не выбрать; выбор по подстроке имени ломается при каждом новом образе.
- **Просим:** поля `os_family`, `os_version` (или метки) в `GET /images`.

## Биллинг и расчёт цены

#### BIL-1. Лицензия образа маркетплейса в расчёте цены всегда 0
Расхождение · подтверждено вживую
- **Спека:** `VM-spec`, `POST /api/v1/projects/{project_id}/price-calculation`, ответ: `image` — «Плата за образы»; полей `mkp_image`, `custom_image_size` в схеме нет.
- **Живое:** api-probe #8 (16.10, ~17:29 UTC): для 57 из 60 образов маркетплейса — 200, `image = 0`, `mkp_image = 0`, `total_price_month > 0` (только инфраструктура); в том числе все четыре Windows Server, Astra Linux KES, РЕД ОС KES. Карточка Windows Server Standard 2022 — 1 006 ₽/мес (см. IMG-2).
- **Влияние:** предварительная цена занижена ровно на лицензию; FinOps-оценки и превью в инструментах (в том числе evo) вводят в заблуждение.
- **Просим:** возвращать цену лицензии в `mkp_image` (или отдельном поле) и описать поле в спецификации; если лицензия считается иначе (подключение продукта, отдельная строка счёта) — описать, как получить её цену до создания ВМ.

#### BIL-2. 422 «sku_identifier cannot be blank» у двух образов Astra Linux
Ошибка API · подтверждено вживую
- **Спека:** 422 у `price-calculation` — «Не удалось найти тип диска / флейвор», тело — массив `{message, code}`.
- **Живое:** api-probe #4 и #8: `alse-smolensk-1-8-3-mkp`, `alse-adv-1-7-7` → 422, тело-строка `sku_list[1]: (sku_identifier: cannot be blank.).` — внутренняя ошибка биллинга в недокументированном формате.
- **Влияние:** цену ВМ с этими образами получить нельзя; вероятно, у продуктов нет SKU в биллинге — вопрос, создаётся ли ВМ вообще.
- **Просим:** завести SKU или скрыть образы; ошибки — в формате спецификации.

#### BIL-3. 500 internal_error в расчёте цены для образа `lamp`
Ошибка API · подтверждено вживую
- **Живое:** api-probe #8: `lamp` → 500 `[{"message":"Internal Server Error","code":"internal_error"}]`; соседний `lemp` с тем же набором полей — 200.
- **Влияние:** превью цены для популярного образа (LAMP есть в документации маркетплейса) падает.
- **Просим:** исправить; при ошибке данных каталога — 422 с причиной.

#### BIL-4. Баланс договора — с недокументированного хоста
Нет в документации · гипотеза
- **Док/спеки:** хост `billing.api.cloud.ru` не упомянут ни в одной спецификации реестра и ни на одной странице документации (поиск по 1600 страницам, 16.10); `GET /v1/agreements` и `/v3/agreements` (`organization.api.cloud.ru`) баланса не отдают. В `/v2/consumption` есть и `amount`, и `cost` — что из них к оплате и с НДС ли, не сказано.
- **Живое:** `billing_balance`/`billing_finops` в cloud-e2e `readonly` (#23–#48) — ошибка; причина (нет `agreement_id` в CI или ответ API) не установлена.
- **Влияние:** расчёт «на сколько хватит баланса» держится на неопубликованном контракте.
- **Просим:** опубликовать эндпоинт баланса или назвать официальный; описать `amount`/`cost`/`amount_nds`. Проба — P13.

## Compute (ВМ)

#### CMP-1. `interface_security_enabled` обязательно, но его нет в спецификации
Расхождение · подтверждено вживую
- **Спека:** `VM-spec`, `VmCreateInterfaceSection` (`POST /api/v1.1/vms`): `type, subnet_id, security_groups, security_group_names, new_external_ip, attach_external_ip_id/name, ip_address`; `additionalProperties: false`.
- **Живое:** без `interface_security_enabled: true` создание ВМ — 500 (разбор отказов cloud-e2e 14–15.10, `.github/review-context/cloudru-api.md`); с полем все e2e 15–16.10 создают ВМ. Поле приходит в `GET /api/v1/vms` у интерфейса (api-probe #8).
- **Влияние:** клиент, сгенерированный по спецификации, ВМ не создаст; строгий валидатор отбросит рабочий запрос.
- **Просим:** добавить поле в схему (тип, умолчание) или не требовать его; отсутствие поля — 422 с именем поля, а не 500.

#### CMP-2. `new_external_ip: true` → 500 `ExternalNetNotFound`
Ошибка API · подтверждено вживую
- **Спека:** `new_external_ip` — «Если выбрано `true`, интерфейсу будет назначен новый публичный IP-адрес»; `ExternalNetNotFoundOnServerExceptionSchema` описан как ответ 500 без условий.
- **Живое:** 500 `ExternalNetNotFound` (cloud-e2e 14–15.10, review-context); рабочий путь — заранее созданный FIP и `attach_external_ip_id`.
- **Влияние:** документированный способ получить ВМ с публичным IP не работает; 500 провоцирует повторы.
- **Просим:** назвать предусловие (сеть с внешним доступом?) и отвечать 422; либо починить.

#### CMP-3. `limit` больше 100 принимается вопреки спецификации
Расхождение · подтверждено вживую (усечение — гипотеза)
- **Спека:** у списков compute `limit`: `maximum: 100`, `default: 50`.
- **Живое:** api-probe #1 (15.10) и #8 (16.10): `GET /vms?limit=200`, `GET /images?limit=200` → 200, `images` — 86 из 86. Отдаёт ли сервер больше 100 или молча усекает — не проверено (объектов меньше 100).
- **Влияние:** клиенты, полагающиеся на 422, его не получат; при ужесточении валидации сломаются все, кто шлёт 200.
- **Просим:** либо валидировать (422), либо поднять `maximum` в спецификации; зафиксировать, что сервер делает с `limit > 100`. Проба — P1.

#### CMP-4. `GET /disk-types` с `limit` отвечает 200 и не-JSON
Ошибка API · подтверждено вживую
- **Спека:** у `/api/v1/disk-types` параметров `limit/offset` нет; ответ — JSON.
- **Живое:** api-probe #8: `GET /disk-types?project_id=…&limit=200` → 200, тело не разбирается как JSON; без `limit` — JSON-массив (не `{items}`, в отличие от остальных списков).
- **Влияние:** неизвестный параметр ломает ответ вместо того, чтобы быть проигнорированным или отвергнутым 422.
- **Просим:** 422 на неизвестный параметр или игнорировать его; единый формат списка. Проба — P2.

#### CMP-5. Поля ответов, которых нет в спецификации
Нет в документации · подтверждено вживую
- **Живое:** api-probe #8: `GET /vms` — `image_id`, `metadata_fields`, `additional_properties`; `GET /security-groups` — `rules`, `has_remote`; `GET /subnets` — `dhcp_default_gateway`; `price-calculation` — `mkp_image`, `custom_image_size`.
- **Влияние:** низкое; клиенты по спецификации эти данные не видят (например, образ ВМ — только через отдельный запрос).
- **Просим:** описать поля или убрать их из ответа.

#### CMP-6. `image_metadata`: флаги образа нельзя отправлять, спецификация молчит
Нет в документации · подтверждено вживую
- **Спека:** `image_metadata` в `POST /api/v1.1/vms` — объект без перечня ключей.
- **Живое:** отправка `vmm_enabled`, `guest_agent_ready` или любого поля с пустым значением — 500; без обязательных полей — `image_metadata_fields_missing`; без `linux_password` ВМ не выходит из `creating` (review-context, cloud-e2e 14–15.10).
- **Влияние:** клиент, честно заполняющий все поля образа, получает 500.
- **Просим:** описать, какие поля метаданных вводит пользователь, какие только читаются; на лишние — 422.

#### CMP-7. `DELETE /vms/{id}` отвечает 409 для ВМ в `creating`
Расхождение · подтверждено вживую
- **Спека:** `DELETE /api/v1/vms/{id}`: 204, 404, 405 «есть связанные сущности», 422 «нельзя удалить в текущем статусе»; 409 нет.
- **Живое:** cloud-e2e 15.10: удаление ВМ в `creating` — 409 и 422 в разных попытках.
- **Влияние:** клиент, обрабатывающий только описанные коды, считает 409 фатальной ошибкой и оставляет платную ВМ.
- **Просим:** один код для «нельзя удалить сейчас» и описание в спецификации; уточнить, удаляет ли `delete_attachments.external_ips` FIP вместе с ВМ.

#### CMP-8. Фильтр `name` у образов ищет подстроку без учёта регистра
Нет в документации · подтверждено вживую
- **Спека:** `GET /api/v1/images`, `name` — «Системное название образа», `display_name` — «Название образа в интерфейсе» (без слова «поиск»).
- **Живое:** api-probe #8: `name=Ubuntu-24.04` и `name=ubuntu-24.04` → 3 (`NVLINK-Ubuntu-24.04-gpu`, `Ubuntu-24.04`, `ubuntu-24.04-gpu`); `name=ubuntu` → 17; `display_name=Astra` → 7.
- **Влияние:** запрос «образ с именем X» возвращает несколько; точного совпадения на сервере нет.
- **Просим:** описать семантику фильтра; при желании — отдельный параметр точного совпадения.

## Managed Kubernetes

#### K8S-1. `addonNamespace` повторяет `addonVersion`
Ошибка API · подтверждено вживую
- **Спека:** `mk8s-spec`, `AddonRelease.addonNamespace` — «Пространство имен, в которое должен устанавливаться плагин», пример `kube-system`.
- **Живое:** api-probe #5 и #8: `GET /v3/addons/cert-manager` — `addonNamespace: "1.21.1-sbc.0"` при `addonVersion: "1.21.1-sbc.0"` (у всех трёх релизов); `traefik` — `"41.1.1-sbc.0"`, `"41.2.0-sbc.3"`. Фактически cert-manager ставится в `cert-manager` (k8s-smoke в cloud-e2e #46 и #48, 16.10).
- **Влияние:** по API нельзя узнать, где искать поды плагина; автоматизация проверки готовности вынуждена искать по всем namespace.
- **Просим:** отдавать настоящий namespace.

#### K8S-2. «Установить плагин → API» описывает устаревший v2
Расхождение док/спеки · подтверждено вживую
- **Док:** [Установить плагин](https://cloud.ru/docs/kubernetes-evolution/ug/topics/guides__addons__add), вкладка API: `POST https://mk8s.api.cloud.ru/v2/clusters/{clusterId}/addons`, тело `addonName`, `addonVersion`, `addonConfiguration`, `resolveMode: RESOLVE_CONFLICTS_MODE_OVERWRITE`; NONE — «конфликты не разрешаются автоматически».
- **Спека:** v2 помечена «Deprecated: используйте API v3»; v3 — `POST /v3/clusterAddons`, обязательные `clusterId, name, appVersion, resolveMode`; NONE — «существующие манифесты не перезапишутся», есть PRESERVE.
- **Живое:** v3-запрос с `RESOLVE_CONFLICTS_MODE_NONE` устанавливает cert-manager 1.21.1, поды Ready менее чем за минуту (k8s-smoke #46, #48); без `resolveMode` — 400 `resolveMode: must be specified` (k8s-smoke 16.10).
- **Влияние:** по инструкции пишут код на устаревший API, с другими именами полей и с OVERWRITE (откатывает пользовательские изменения) в примере.
- **Просим:** перевести инструкцию на v3; согласовать описание режимов; в примере — NONE.

#### K8S-3. Плагины ставятся без SNAT-шлюза — это не описано
Нет в документации · подтверждено вживую (отсутствие SNAT — подтвердить P10)
- **Док:** [Ограничения](https://cloud.ru/docs/kubernetes-evolution/ug/topics/overview__limitations): «Для доступа кластера и его подов в интернет необходимо создать SNAT-шлюз в той же зоне доступности, что и узлы».
- **Живое:** k8s-smoke #46 (17:23–17:44 UTC) и #48 (21:20–21:39 UTC), ru.AZ-2: в проекте без SNAT-шлюза поды cert-manager 1.21.1 `Running/Ready` через ~35 с после установки, `ImagePullBackOff` нет. Версии плагинов с суффиксом `-sbc.N` — вероятно, образы из внутреннего реестра.
- **Влияние:** пользователь создаёт платный SNAT-шлюз «для плагинов», хотя он не нужен; или не понимает, почему его собственные образы не тянутся, а плагины — да.
- **Просим:** написать, откуда кластер берёт образы плагинов и когда SNAT нужен.

#### K8S-4. Фильтр `isBillable=true` возвращает плагин с `isBillable=false`
Ошибка API · подтверждено вживую
- **Спека:** `GET /v3/addons`, `filter` в виде «field condition value», пример `isBillable = true`.
- **Живое:** api-probe #5 и #8: у всех 42 плагинов `isBillable=false`; `filter=isBillable=true` (без пробелов) → 1 плагин.
- **Влияние:** выборка «платных плагинов» недостоверна.
- **Просим:** проверить фильтр; описать допустимый синтаксис (с пробелами и без). Проба — P11.

#### K8S-5. Формат `requirements` и порядок релизов не определены
Нет в документации · подтверждено вживую
- **Спека:** `kubeVersions` — «Диапазон поддерживаемых версий Kubernetes в формате semver»; порядок релизов по умолчанию не указан.
- **Живое:** api-probe #8: `keda` — `">=v1.23.0-0"`, остальные — `">=1.26.0-0"` (без `v`); `evocs` — `readyWorkers: 0`; релизы `traefik` идут по возрастанию, остальных — по убыванию.
- **Влияние:** проверка совместимости и выбор «последней версии» требуют нормализации на клиенте.
- **Просим:** один формат диапазона; порядок по умолчанию в спецификации.

## IAM и Organization

#### IAM-1. Ответ токена IAM — snake_case, в спецификации camelCase
Расхождение · подтверждено вживую
- **Спека:** `administration/ug/_specs/iam-gw-public.yaml`, `POST /api/v1/auth/token`: `accessToken, expiresIn, idToken, notBefore, refreshExpiresIn, refreshToken, scopes, tokenType`.
- **Живое:** api-probe #1 (15.10) и #8 (16.10): 200, ключи `access_token, expires_in (число), id_token, not_before, refresh_expires_in, refresh_token, scopes, token_type`.
- **Влияние:** клиент по спецификации не находит токен — самая первая операция любой интеграции.
- **Просим:** исправить спецификацию (или ответ) и зафиксировать написание.

#### IAM-2. Список сервисных аккаунтов — snake_case, в спецификации camelCase
Расхождение · подтверждено вживую
- **Спека:** `GET /api/v2/serviceAccounts`: `serviceAccounts`, `nextPageToken`.
- **Живое:** api-probe #1 и #8: 200, `service_accounts` (18 записей), `next_page_token`.
- **Влияние:** клиент по спецификации получает пустой список и не находит SA, например, для `cluster_sa_id` кластера Kubernetes.
- **Просим:** то же, что IAM-1.

#### IAM-3. Сервисный аккаунт проекта не получает список проектов
Нет в документации + ошибка API · подтверждено вживую
- **Спека:** `administration/ug/_specs/api.op.v3.yaml`, `GET /v3/projects` — фильтры `project_ids`, `customer_ids`, …; ответ 400 не описан.
- **Живое:** 14.10 и api-probe #1, #8: 400, тело `{code, details, message}`, «(user_id) is invalid argument: user not found». cloud-e2e #43/#44 (`whoami`): список проектов недоступен.
- **Влияние:** CLI и агенты не могут предложить проект ключа — UUID вводится вручную; 400 похож на ошибку клиента.
- **Просим:** назвать официальный способ узнать проект(ы) SA (например, `GET /api/v2/serviceAccounts/{id}`); для SA — 200 со своим проектом или 403 с понятной причиной; описать в спецификации. Пробы — P7, P8.

## Аудит-логирование

#### AUD-1. 401 «Необходима авторизация» при действующем токене
Расхождение · подтверждено вживую
- **Док:** [Получить аудит-логи](https://cloud.ru/docs/audit-logging/ug/topics/guides__logs__get-logs): «Доступ к функциональности ограничен. Чтобы получать аудит-логи, обратитесь в техническую поддержку». Спека `audit-logging/ug/_specs/openapi-v3.yaml`: 401 «Необходима авторизация», 403 «Доступ запрещен».
- **Живое:** api-probe #1 и #8: `GET /events/list/{projectId}?start&end&limit=5` → 401, тело `{code, details, message}`; тот же токен в ту же минуту принимается compute, IAM, mk8s.
- **Влияние:** клиент считает токен недействительным и уходит в повторную аутентификацию вместо сообщения «функция не включена».
- **Просим:** 403 с причиной («аудит не подключён для организации»); описать в спецификации.

#### AUD-2. Неизвестный query-параметр → 415, раньше проверки доступа
Ошибка API · подтверждено вживую
- **Спека:** параметры `eventType, eventSource, start, end, sort.*, nextPageToken, limit`; 415 не описан.
- **Живое:** api-probe #1 и #8: тот же запрос с `pageSize=5` вместо `limit` → 415 (Unsupported Media Type), пустое тело; с `limit` — 401.
- **Влияние:** код 415 указывает на заголовок Content-Type, а не на параметр; отладка ведёт по ложному следу.
- **Просим:** 400 с именем параметра (или игнорировать неизвестные).

## VPC, DNS, группы безопасности, SNAT

#### NET-1. grpc-gateway принимает snake_case при camelCase в спецификации
Нет в документации · подтверждено вживую
- **Спека:** VPC `GET /v1/vpcs` — `projectId` (required); DNS `publicZones` — `projectId`; mk8s — `projectId`. NLB и PostgreSQL на том же стеке опубликованы в snake_case.
- **Живое:** api-probe #1 и #8: `GET vpc.api.cloud.ru/v1/vpcs?project_id=…` и `?projectId=…` — одинаковый 200; DNS — то же (#1).
- **Влияние:** интеграции, написанные по примерам snake_case, работают за счёт свойства реализации; при отключении лениентности сломаются без предупреждения.
- **Просим:** сказать, гарантирован ли приём proto-имён, и единую политику именования.

#### NET-2. Группа безопасности по умолчанию: «при создании организации», а живёт в проекте
Нет в документации · гипотеза
- **Док:** [Группы безопасности](https://cloud.ru/docs/security-groups/ug/topics/concepts__security-group): «При создании организации в каждой зоне доступности автоматически создается группа безопасности по умолчанию с названием SSH-access_<az>»; «удаление недоступно».
- **Живое:** api-probe #8: в тестовом проекте группа с этим шаблоном имени есть во всех трёх зонах; в ответе есть поле `default`. Создаётся ли она в каждом новом проекте и всегда ли `default=true` — не проверено.
- **Влияние:** низкое; автоматизация, ищущая «группу по умолчанию» по имени, ломается после переименования (документация переименование разрешает).
- **Просим:** уточнить в документации «проект/организация»; сослаться на поле `default` как на признак. Проба — P3.

#### NET-3. SNAT: «ровно одно из `availability_zone_id` / `availability_zone_name`»
Расхождение · подтверждено вживую
- **Спека:** `VM-spec`, `POST /api/v1/nat-gateways`: оба поля необязательны, `oneOf` нет.
- **Живое:** валидатор отвергает запрос без обоих и с обоими полями (review-context, 14–15.10).
- **Влияние:** низкое; ошибка только на этапе отладки.
- **Просим:** отразить в спецификации (`oneOf` или текст).

## DBaaS: Kafka, Redis, PostgreSQL

#### DB-1. Списки кластеров и спецификаций Kafka/Redis отвечают ошибкой
Ошибка API или нет в документации · гипотеза
- **Спека:** `paas-kafka/…/openapi-kafka.yaml`, `redis/…/openapi-redis.yaml`: `GET /v1/clusters` — `project_id`, `page_size` (строка int64), `page_token`, у Kafka `product_type`; все необязательные.
- **Живое:** cloud-e2e `readonly` #23 … #48 (16.10, последний 21:20 UTC): `kafka_list`, `redis_list`, `kafka_specs`, `redis_specs` — ошибка API, `kafka_versions`/`redis_versions` того же хоста — 200. Запрос: `?product_type=kafka&project_id=…&page_size=1000`. Код HTTP в лог не попал.
- **Подозрения:** недокументированный максимум `page_size`; обязательный `availability_zone_id` у specifications (DB-2).
- **Просим:** описать диапазон `page_size` и обязательные параметры. Пробы — P4, P5.

#### DB-2. `GET /v1/specifications` требует `availability_zone_id`, помеченный необязательным
Расхождение · гипотеза
- **Спека:** `availability_zone_id` — необязательный.
- **Живое:** наблюдение из кода клиента (без даты и прогона): без зоны — ошибка.
- **Просим:** подтвердить и отразить в спецификации. Проба — P5.

#### DB-3. Redis: `STANDALONE` и `subnet_ids[]` не работают
Расхождение · подтверждено вживую (до 15.10)
- **Спека:** `type` enum `STANDALONE, MASTER_REPLICA`; `subnet_id` и `storage_gb` — deprecated, актуальные `subnet_ids[]`, `storage_gib`.
- **Живое:** review-context и комментарии клиента (создание кластеров до 15.10, номер прогона не сохранён): `STANDALONE` отвергается; `MASTER_REPLICA` — ровно 3 реплики на шард; `subnet_ids[]` роняет бэкенд (деление на ноль), работает только deprecated `subnet_id`.
- **Влияние:** по спецификации Redis не создать; deprecated-поле — единственный рабочий путь.
- **Просим:** починить `subnet_ids[]` до удаления `subnet_id`; описать ограничения `type`. Повтор требует создания кластера — пробой не проверяется.

#### DB-4. Ошибки 4xx с пустым телом и причиной в заголовке `grpc-message`
Расхождение · гипотеза
- **Спека:** тело ошибки — `rpcStatus` (`code, message, details`).
- **Живое:** клиенты PostgreSQL и Kafka/Redis разбирают `grpc-message`, потому что тело приходило пустым (например, «backup disabled»); дата не сохранена.
- **Просим:** причину в теле по спецификации. Проба — P4 (печатает заголовок).

## Мониторинг

#### MON-1. `export_metrics`: единицы времени, предел интервала и формат ответа
Нет в документации · гипотеза
- **Спека:** `service-monitoring/…/openapi-v3.yaml`, `GET /v1/{project_id}/export_metrics`: `match`, `start`, `end` обязательны; единицы не указаны; ответ — `string`.
- **Живое:** клиент шлёт миллисекунды и держит окно ≤ 7 дней «по опыту»; ответ разбирается как JSON-массив, объект или JSON lines.
- **Просим:** единицы, максимальный интервал, схему ответа. Проба — P12.

## Foundation Models

#### FM-1. Токен IAM отклоняется сообщением «invalid api key secret»
Нет в документации · подтверждено вживую (до 15.10)
- **Док:** [Аутентификация](https://cloud.ru/docs/foundation-models/ug/topics/api-ref__authentication) — только API-ключ.
- **Живое:** `GET /v1/models` принимает и токен IAM, и API-ключ; `POST /v1/chat/completions` с токеном IAM — 400 `invalid api key secret` (review-context).
- **Влияние:** неочевидно, что токен IAM не годится, — сообщение говорит о неверном ключе.
- **Просим:** 401 с текстом «нужен API-ключ»; одинаковое поведение у всех методов.

## Портал документации

#### DOC-1. cloud.ru/docs отвечает по-разному в зависимости от User-Agent
Ошибка (сайт) · подтверждено вживую
- **Живое:** 16.10, 21:48 UTC, `https://cloud.ru/docs/kubernetes-evolution/ug/topics/overview__limitations`: без заголовка User-Agent — 403; `python-requests/…` и `Python-urllib` — 403; `curl/8.x`, `Go-http-client`, `Mozilla/5.0` — 200. Ранее 16.10 из окружения агентов (WebFetch и curl по умолчанию) получали 400 «Not found»; с `-A 'Mozilla/5.0'` — 200. Спецификации `/docs/api/specs/…` в 21:48 UTC отдавались и без браузерного UA.
- **Влияние:** документацию не читают агенты и скрипты (в том числе скачивание спецификаций для проверки контракта); 400 «Not found» маскирует блокировку под отсутствие страницы.
- **Просим:** 200 для любых UA или явный 403/429 с причиной; не возвращать «Not found» для существующих страниц.

## Снятые вопросы

Проверены и совпадают с документацией; ошибка была на стороне evo или в
наших ожиданиях.

| Вопрос | Что оказалось | Доказательство |
|---|---|---|
| mk8s: `resolveMode` обязателен, значения `RESOLVE_CONFLICTS_MODE_*` | так и в `mk8s-spec` (`CreateClusterAddonRequest.required`); evo слал `RESOLVE_MODE_*` | k8s-smoke 16.10: 400 → исправлено, #46/#48 зелёные |
| mk8s: `GET /v3/nodePools` и `/v3/clusterAddons` без `clusterId` → 400 | в спецификации `clusterId` required | api-probe #8 (`cluster_addons` 400), cloud-e2e `readonly` |
| mk8s: `GET /v3/addons` без `projectId` → 400 | `projectId` required | api-probe #5, #8 |
| mk8s: `mode = SUPPORT_MODE_ACTIVE` | enum спецификации такой же | api-probe #8 |
| Группа безопасности «Default» отсутствует в зонах | по документации группа по умолчанию называется `SSH-access_<az>`; она есть во всех трёх зонах; evo искал имя «Default» | api-probe #8; см. NET-2 |
| Зона подсети и группы безопасности «пустая» | в спецификации и в ответе — вложенный `availability_zone{id,name,enabled}` | api-probe #1 |
| Публичный IP ВМ | `interfaces[].floating_ip{id,ip_address,name,state}` — как в спецификации | api-probe #1, #8 |
| Флейворы «теряются» | без `limit` — 50 из 78 по `default: 50` спецификации | api-probe #1, #8 |
| Состояние FIP `active` | встречаются `in_use`, `attaching` — в enum | api-probe #1, #8 |
| `GET /vms` без `project_id` пуст | `project_id` required | QA 15.10 |
| NLB `GET /v2/operations/{id}` требует `?id=` | задокументировано | спецификация NLB |
| Тела создания Kafka, PostgreSQL, NLB, Secret Manager, AR | совпадают со спецификациями | сверка 15.10 (`api-contract.md`) |

## Эксплуатационные наблюдения

Не расхождения документации; адресаты — эксплуатация Compute и сети.

- **OPS-1. ru.AZ-2, 16.10: ВМ активна, SSH не отвечает.** cloud-e2e #43 (16:42 UTC) и #44 (17:05 UTC): ВМ `ubuntu-22.04` активна за 2 мин 25 с / 2 мин 30 с, 60 попыток SSH за 14 мин — таймаут. Группа безопасности ВМ разрешала tcp/22 с 0.0.0.0/0 и весь исходящий трафик (api-probe #8, 17:25 UTC). Тот же план зелёный в #42 (16:08), #46 (17:23), #47 (17:44), #48 (21:19). Вероятно, сеть первой загрузки.
- **OPS-2. ru.AZ-2, 16.10 днём: apt-зеркало образа недоступно из ВМ.** Установка Docker не завершилась за 10 мин в 4 из 6 прогонов (по BACKLOG [e2e]: «прогоны 30, 33, 33-повтор, 34»; в журнале #35 — тот же исход, 12:53–13:10 UTC); диагностика: `W: Failed to fetch https://repo.mng.sbercloud…`. Код и образ одни и те же в зелёных и красных прогонах.
- **OPS-3. ru.AZ-2, 15.10 утром: нет DNS внутри ВМ.** `Temporary failure in name resolution` для `ubuntu-mirror-1-deb.ar.cloud.ru`; SSH поднялся через 5,5 мин, во втором прогоне — не поднялся за 22 мин.
- **OPS-4. ru.AZ-1, 15.10 вечером: ВМ не создаются.** #17 — 10 мин в `creating`; #18 — 19,5 мин в `creating`, затем `error_creating`; #21 — активна за 5 мин 20 с, SSH не поднялся за 15 мин. В ru.AZ-2 в то же время — зелёные #19, #20.
- **OPS-5. Сроки mk8s (ru.AZ-2, 16.10).** Кластер с одним мастером `OBJECT_STATUS_PENDING → PROVISIONING → RUNNING` — 9 мин (#46) и 7 мин (#48); группа из одного узла — 6 мин; удаление кластера — 4–5 мин (`OBJECT_STATUS_DELETING`, затем исчезает из списка). Полезно для документации ожидаемых сроков.
- **OPS-6. Разовые сетевые сбои.** api-probe #8: `dns.api.cloud.ru` — `Connection reset by peer` (один запрос из пяти); 15.10: `list-zones` из контейнера — обрыв примерно в одном запуске из трёх; cloud.ru/docs 16.10 — `SSL_ERROR_SYSCALL` и 55 таймаутов соединения при обходе 1600 страниц.

## Что проверить пробой

Все пробы — только чтение (кроме P9: `POST price-calculation`, который
ничего не создаёт). В лог — только коды HTTP, имена полей, числа и
сообщения об ошибках, обрезанные до 160 символов; ID и IP — маскировать.
`P` — ID проекта, `T` — токен IAM.

| № | Запрос | Что печатать | Закрывает |
|---|---|---|---|
| P1 | `GET compute/api/v1/images?limit=200`, `…/flavors?limit=200`, `…/images?limit=100&offset=50` | `http`, `.limit` (эхо сервера), `.items\|length`, `.total` | CMP-3: усекает ли сервер `limit` |
| P2 | `GET compute/api/v1/disk-types?project_id=P&limit=200` и без `limit` | `http`, `Content-Type`, первые 120 байт тела (UUID → `<id>`) | CMP-4 |
| P3 | `GET compute/api/v1/security-groups?project_id=P&limit=100` | `[{zone: .availability_zone.name, default, ssh_access_name: (.name\|startswith("SSH-access_"))}]` | NET-2 |
| P4 | `GET kafka.api.cloud.ru/v1/clusters?product_type=kafka&project_id=P` c `page_size=1000`, `=100`, без `page_size`; то же `redis.api.cloud.ru/v1/clusters?project_id=P` | `http`, ключи верхнего уровня, `.message[0:160]`, заголовок `grpc-message` | DB-1, DB-4 |
| P5 | `GET kafka…/v1/specifications`: (а) без параметров, (б) `version_id=<первый из /v1/versions?product_type=kafka>`, (в) + `availability_zone_id=<UUID ru.AZ-2>`; то же для Redis | `http`, число спецификаций, `.message[0:160]` | DB-1, DB-2 |
| P6 | `GET audit.api.cloud.ru/events/list/P?start&end&limit=5`; без параметров; с `pageSize=5` | `http`, `.code`, `.message[0:160]`, `Content-Type` | AUD-1, AUD-2 (текст причины) |
| P7 | `GET organization.api.cloud.ru/v3/projects?project_ids=P`; без параметров | `http`, число проектов, `.message[0:160]` | IAM-3 |
| P8 | `sub` из токена (не печатать) → `GET iam.api.cloud.ru/api/v2/serviceAccounts/{sub}` | `http`, ключи, есть ли `project_id`/`target` | IAM-3: официальный путь к проекту SA |
| P9 | `price-calculation` с `image_id` образа `windows2022std-evo-kmsB` и без флейвора/дисков | `http`, значения `image`, `mkp_image`, `total_price_month` | BIL-1 (цены публичны) |
| P10 | `GET compute/api/v1/nat-gateways?project_id=P` | `total`, `[.items[].availability_zone.name]` | K8S-3: SNAT в проекте отсутствует |
| P11 | `GET mk8s/v3/addons?projectId=P&filter=` `isBillable%20%3D%20true`, `isBillable%3Dtrue`, `isBillable%3Dfalse` | `http`, число, имена плагинов | K8S-4 |
| P12 | `GET monitoring.api.cloud.ru/v1/P/export_metrics?match=up&start&end` — секунды и миллисекунды, окно 1 ч и 8 дней | `http`, длина тела, первый символ тела, `.message[0:160]` | MON-1 |
| P13 | `GET organization.api.cloud.ru/v3/agreements`; `GET https://billing.api.cloud.ru/` | `http`, ключи (без значений) | BIL-4 |

## Источники

- Репозиторий: `docs/internal/standalone/BACKLOG.md` (разделы [images],
  [k8s-addons], [cloud-readonly], [e2e], [audit], [02], [api-contract]),
  `docs/internal/api-contract.md` (сверка 15.10 и §7 «Живая проверка»),
  `.github/review-context/cloudru-api.md`, `docs/troubleshooting.md`,
  `docs/guide.md` §11, `scripts/api-contract.json`,
  `.github/workflows/api-probe.yml`, `.github/workflows/cloud-e2e.yml`.
- api-probe: #1 (15.10, 20:23 UTC), #2–#7 (16.10), #8 (16.10, 17:25 UTC, job
  117103387281).
- cloud-e2e: #17–#21 (15.10), #23 (16.10, первый `readonly`), #31 (`os-smoke`,
  jobs 116958214214, 116958213835), #35 (job 116981821182), #43 (job
  117084713592), #44 (job 117094443135), #46 (`k8s-smoke`, job 117102589469),
  #48 (`k8s-smoke` job 117201199081, `readonly` job 117201199107).
- Спецификации (реестр API, скачаны 15–16.10.2026): virtual-machines
  `openapi-v3.yaml`, kubernetes-evolution `openapi-v3.yaml` и `openapi.yaml`
  (v2), administration `iam-gw-public.yaml`, `api.op.v3.yaml`, audit-logging
  `openapi-v3.yaml`, paas-kafka `openapi-kafka.yaml`, redis
  `openapi-redis.yaml`, evolution-vpc `openapi.yaml`, service-monitoring
  `openapi-v3.yaml`.
- Страницы документации (16.10.2026):
  [Группы безопасности](https://cloud.ru/docs/security-groups/ug/topics/concepts__security-group),
  [Ограничения mk8s](https://cloud.ru/docs/kubernetes-evolution/ug/topics/overview__limitations),
  [Установить плагин](https://cloud.ru/docs/kubernetes-evolution/ug/topics/guides__addons__add),
  [Образы Маркетплейса](https://cloud.ru/docs/images/ug/topics/concepts__marketplace-images),
  [Публичные образы](https://cloud.ru/docs/images/ug/topics/concepts__public-images),
  [Тарификация образов](https://cloud.ru/docs/images/ug/topics/pricing),
  [Получить аудит-логи](https://cloud.ru/docs/audit-logging/ug/topics/guides__logs__get-logs),
  [Аутентификация Foundation Models](https://cloud.ru/docs/foundation-models/ug/topics/api-ref__authentication),
  [Windows Server Standard 2022 в Маркетплейсе](https://cloud.ru/marketplace/products/ms-std-22).
