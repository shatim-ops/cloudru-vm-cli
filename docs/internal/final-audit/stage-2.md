# Fable · финальный аудит v1.0.0 · этап 2/5 · сервисы и стыковка

Голова PR #89: `9386ef815c254eb237ff7dfab8aad2359d6510c1`. Проверено по коду: `tools_k8s.go`, `tools_nlb.go`, `tools_ca.go`, `tools_vpc.go`, `tools_dns.go`, `tools_snat.go`, `tools_ar.go`, `tools_certmanager.go`, `tools_secret.go`, `delete_preview.go`, `registry.go` (`projectTools`/`engineFor`), `internal/mk8s`, `internal/vpc`, путь вызова CLI (`internal/cli/tools.go invokeTool`). Облако не вызывалось.

## Находки

**[medium]** `internal/tools/tools_k8s.go:486-496` — `k8s_add_nodepool` без `security_group_id`: если `PickSecurityGroup` ничего не нашёл в зоне (или `ListSecurityGroups` упал — ошибка молча проглочена `se == nil`), `sgID` остаётся пустым и запрос уходит без `securityGroupId` (`mk8s/nodepool.go:42`, `omitempty`). Описание аргумента обещает «default auto zone-matched SSH-access»; `provision` и `deploy_plan` в той же ситуации отказывают (CHANGELOG: «нет группы безопасности в зоне … отказ»). Путь: проект без SG в зоне кластера (или транзиентный сбой списка) → `k8s_add_nodepool confirmed=true` → пул создаётся без группы (если API принимает) либо 400 после подтверждения. Гипотеза: что делает mk8s с пустым `securityGroupId` — по спецификации проверю на этапе 3. Исправление: при `sgID == ""` отвечать `CONFIG_INVALID` с подсказкой `list_security_groups`/`security_group_id`, ошибку списка не глотать.

**[low]** `internal/tools/tools_k8s.go:262-275` — у `k8s_create` превью строится до `resolveK8sPlan`: зона, подсеть VIP, версия и флейвор проверяются только после `confirmed=true`. Ничего не создаётся, но подтверждение «слепое» (превью — сырые аргументы; текст отсылает к `k8s_plan`). `lb_create`, `pg_create`, `snat_create` резолвят до гейта. Исправление: вызвать `resolveK8sPlan` до гейта и показать `zone`, `subnet_id`, `version`, `flavor_id` в превью.

**[low]** `internal/tools/tools_k8s.go:565` (`k8sZone`), `tools_nlb.go:349` (`lbZoneAndSubnet`), `tools_kafka.go:68` (`dbaasZone`) — нет проверки `provision.ClosedZoneError`, которая есть у `deploy`, `provision`, `deploy_plan` и `stack_plan` (`engine/network.go:46`, `tools.go:1693`, `tools_stack.go:577`). Это класс N-02/N-04 из BACKLOG `[audit-90]`; перенос приемлем, но для k8s это платный кластер, который в закрытой зоне не поднимется. Исправление — один вызов после резолва зоны в каждом из трёх мест.

**[low]** Превью удаления без GET (после закрытия R-04 для ВМ, k8s, PG/Kafka/Redis, AR, NLB/TG, VPC, SNAT, стеков): `ca_delete` (`tools_ca.go:213`), `cert_delete` (`tools_certmanager.go:143`), `disk_delete` (`tools_disk.go:328`), `image_delete` (`tools_image.go:253`), `dns_zone_delete`/`dns_record_delete` (`tools_dns.go:110,258`), `secret_delete` (`tools_secret.go:148`), `vpc_route_delete` (`tools_vpc.go:224`), `pg_db_delete`/`pg_user_delete`, `s3_rm`/`s3_bucket_delete`. Несуществующий id узнаётся после подтверждения. Для v1.0 достаточно `disk_delete` и `image_delete` (платные, UUID → `deletePreview` с `disk_list`/`image_get` — по образцу `snat_delete`), остальное — v1.1.

**[nit]** `internal/tools/delete_preview.go:19-22` — `status` берётся из `status`/`state`; у ресурсов с другим ключом (например, `lifecycleStatus`) в `target.status` будет пусто. Косметика.

## Проверено без замечаний

- CLI-дерево и `evo tools call` вызывают тот же `t.Handler`, что MCP (`internal/cli/tools.go:629,650`): обёртки `checkedArgs` (типы, неизвестные аргументы, enum) и `requireProject` общие; коды выхода берутся из `reason` ответа. Расхождений проверок между CLI и MCP не нашёл.
- `projectTools`/`engineFor` (`registry.go:224-271`): проектные списки и create отвечают `CONFIG_INVALID` до токена; инструменты по id (`vm_get` → `GET /vms/{id}`, `disk_delete` с необязательным `project_id`) и каталоги работают без проекта — как заявлено в CHANGELOG. Каталоги mk8s в списке.
- `lb_create`: VPC из группы целей, отказ при чужом `vpc_id` (N-01), зона — из аргумента/подсети/ВМ группы/региона с отказом «cannot tell the zone», подсеть только той VPC и зоны, предупреждение о ВМ в нескольких зонах; всё до гейта.
- `k8s_plan/create`: `master_count` ∉ {1,3,5} → `USAGE` до гейта; `Zones` в запросе (L-08); подсеть VIP только зоны кластера; `k8s_add_nodepool`: зона из `controlPlane.zones[0]`, отказ если кластер её не сообщает, подсеть нод только той зоны, имя пула в превью = имени в create.
- `ca_deploy`: RFC 1123 имя, диапазон и зарезервированные порты, `0 ≤ min ≤ max`, `privileged` явный, превью со всеми параметрами и `runs_as`; `env` — только имена.
- Превью удаления с GET и предупреждением о `DEPLOYING/CREATING/PROVISIONING`: k8s, pg, kafka, redis, ar, lb, tg, vpc, snat; `stack_destroy` — список ресурсов и проверка проекта.
- `dns_record_create`: тип приводится к нижнему регистру и проверяется по списку до гейта; DNS `type` public/private.
- `vpc_get`/`vpc_routes`: UUID до запроса; VPC API 404 → `NOT_FOUND` (код 4).
- `cert_private_key`: PEM из Base64 с `key_format`; ключ `privateKey` не переименован, так что HTTP-редакция по ключу остаётся применимой (сверю на этапе 3).
- `ar_create`: занятое имя → `USAGE` + `existing_id`; `ar_delete` с GET.
- mk8s/containerapp: ошибки разбора JSON обёрнуты «parse <что>» → `API_ERROR` (ревью Cursor).
- Описания инструментов `disk_create`, `lb_create`, `kafka_create`, `snat_create`, `tg_create`, `pg_create`, `destroy`, `k8s_add_nodepool` соответствуют коду (с оговоркой по SG выше); `docs/tools.md` генерируется из реестра и закреплён `TestToolsDoc`.
