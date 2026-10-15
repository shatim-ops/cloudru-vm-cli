# API Cloud.ru: проверенные факты

Что этот проект узнал о поведении API Cloud.ru на практике: живые ответы
валидатора, сквозные прогоны cloud-e2e, разбор отказов. Документация API
местами расходится с этим — верить нужно этому файлу и коду, на который он
ссылается. Нашли новое расхождение — допишите сюда в том же PR.

Ревьюеру: если код идёт против этих фактов, это находка; если код с ними
согласен, а «по документации» должно быть иначе — это не находка.

## Авторизация

- Токен: `POST https://iam.api.cloud.ru/api/v1/auth/token`, тело
  `{"keyId": "...", "secret": "..."}`, ответ — `access_token` со сроком жизни
  около часа. Срок берётся из ответа; кэш и обновление при остатке меньше
  5 минут — `internal/auth/iam.go`.
- Foundation Models: `GET /v1/models` принимает и токен IAM, и API-ключ, а
  `POST /v1/chat/completions` — только API-ключ (`Authorization: Bearer
  <api-key>`); с токеном IAM отвечает 400 `invalid api key secret`.

## Где что живёт

- Проекты, квоты, биллинг: `organization.api.cloud.ru`
  (`/v3/projects`, `/v3/quotas/usages`, `/v2/consumption`).
- ВМ, диски, сети, FIP, образы: `compute.api.cloud.ru`. Список `/vms`,
  подсети, группы безопасности, типы дисков, FIP — только с `project_id`.

## Создание ВМ (`POST /api/v1/vms`)

- Поля по **именам**: `flavor_name`, `image_name`, `disk_type_name`. UUID в
  них — 500 или 422 (`flavor_name` с UUID — 422).
- Интерфейс обязателен и явный: `type: "regular"`, `subnet_id`,
  `interface_security_enabled: true`, `security_groups`. Без интерфейса — 500.
- Публичный IP при создании — только через `attach_external_ip_id` (ID
  заранее созданного свободного FIP). `new_external_ip` даёт
  500 `ExternalNetNotFound` и никогда не отправляется.
- Загрузочный диск не меньше `min_disk` образа (у ubuntu-22.04 — 11 ГБ),
  иначе 422 `vm_root_disk_too_small` — `internal/provision/bootdisk.go`.
- `image_metadata`: обязательные поля образа нужно передать все, иначе
  `image_metadata_fields_missing`. Поля `vmm_enabled` и `guest_agent_ready` —
  только для чтения: их отправка (и любого поля с пустым значением) — 500.
- POST создания на 5xx **не повторяется**: ВМ могла создаться, а ответ
  потеряться. После ошибки ВМ ищут по имени и времени создания
  (`provision.FindCreatedVM`). Повтор с backoff — только для GET.
- У подсетей и групп безопасности `availability_zone_id` пустой: зона
  определяется по имени (`Default_ru.AZ-1`, `SSH-access_ru.AZ-1`). Группа из
  чужой зоны не даёт ВМ загрузиться — `internal/engine/network.go`.

## Публичные IP (FIP)

- Создание: достаточно `name`, `project_id`, `availability_zone_id`.
  Состояния: `available` (свободен), `in_use`/`active` (привязан).
- Привязка к интерфейсу, который ещё создаётся, — 422
  `interface_can_not_be_changed_from_current_state`: ждать состояния
  `created` у основного интерфейса.
- Привязанный FIP не удаляется; удалять после удаления ВМ.
- Ошибка квоты содержит `quota` или `organization_quota_exceeded`
  (`provision.IsQuotaError`). При квоте ВМ не создаётся вовсе: IP после
  загрузки упёрся бы в ту же квоту.

## Первая загрузка образов Cloud.ru

- bootcmd образа `configure-apt-mirrors` при первой загрузке переключает apt
  на зеркало Cloud.ru, и ему нужен интернет. ВМ без публичного IP может
  10+ минут ждать таймаутов до старта sshd — поэтому IP привязывается при
  создании. Маркер выполнения:
  `/var/lib/cloud/instance/sem/configure-apt-mirrors`.

## Диски, образы, прочее

- `POST /api/v1/disks`: обязательны `name`, `size`, `project_id`,
  `availability_zone_id` (UUID) и ровно одно из `disk_type_id` /
  `disk_type_name`. Размер до 65536 ГБ; уменьшение и resize бесплатных
  дисков — 422.
- Образ из диска: диск загрузочный и **отключён** от ВМ (состояние
  `available`); остановки ВМ мало — `invalid_disk_state_to_create_image`.
- NAT (SNAT): обязательны `name`, `nat_type`, `project_id` и ровно одно из
  `availability_zone_id` / `availability_zone_name`; лишние поля запрещены.
- Redis: тип `STANDALONE` API отвергает; `MASTER_REPLICA` — ровно 3 реплики
  на шард.
