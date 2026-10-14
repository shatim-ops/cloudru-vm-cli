# BACKLOG (вне scope v1.0)

Сюда исполнитель этапов записывает найденное вне своего scope. Формат:
`- [этап-источник] что · где (file:line) · почему не сейчас`.

Известное на старте (из аудита 2026-10-14):

- [audit] OS keyring (Keychain / Credential Manager / Secret Service) · internal/auth · файл 0600 достаточен для v1.0; **v1.1** (заглушка `KeyringProvider` удалена на этапе 2)
- [audit] Настоящий `Authorization: Api-Key` Cloud.ru · internal/auth · не все сервисы поддерживают
- [audit] OAuth 2.1 для MCP over HTTP · internal/mcp · ждать требований платформы AI Agents
- [audit] `--verbose` → `internal/log` с санитайзером · cmd, internal/log · P2
- [audit] Тесты для 16 пакетов без тестов и для cmd/cloudru-vm · P2
- [audit] `deploy`: `.env`, `build:`, несколько compose-файлов, файлы из volumes · internal/engine/deploy.go:156
- [audit] Переопределение базовых URL API через env (стенды, прокси) · все `*/client.go`
- [audit] Первоклассные деревья команд для kafka, redis, dns, nlb, certmanager, ar, ca, stack, monitoring, audit, billing, admin, snat
- [audit] Homebrew tap, scoop, нотаризация macOS, Authenticode
- [audit] Проверка обновлений (`version --check`), man-страницы
- [audit] `kubetools`: не передавать `CLOUDRU_*` в дочерние процессы (`kubetools.go:65`), пути поиска для Windows — **поднять в этап 5 или 8, если останется время: это утечка секрета**
- [audit] ~~`id.cloud.ru/auth/system/openid/token` (mk8s kubeconfig) не найден в документации~~ — **проверено вживую 2026-10-14**: и `id.cloud.ru`, и `auth.iam.cloud.ru` (`/auth/system/openid/token`) отвечают 200 на `grant_type=access_key`; вопрос закрыт
- [audit] Хост billing API и `CLOUDRU_AGREEMENT_ID`: документация хоста не называет; подтвердить
- [audit] Публичный API цен Cloud.ru отсутствует; `internal/pricing` — оценка по зашитой карте. Для внешнего продукта нужен источник или явный disclaimer в каждом превью
- [06] Жёсткие ожидания в MCP и deployplan: poll 5 с, 8 с, 10 с, `time.Sleep(5s)` · internal/mcp/tools.go:1687, internal/mcp/tools_stack.go:494, internal/mcp/captainkeys.go:79, internal/deployplan/apply.go:17,252 · текущие тесты эти пути не проходят; внедрить, когда появятся тесты
- [06] `go test -race` тратит ≈1 с на каждый пакет: race-рантайм спит `atexit_sleep_ms=1000` при выходе · все пакеты · с ростом числа пакетов с тестами упрёмся в 10 с; тогда `GORACE=atexit_sleep_ms=0` в CI и документацию для разработчика
- [01] Compute API 401/403 даёт `API_ERROR` (код 5), а не код 2 · internal/provision/adapter.go `doWithRetry`, ветка default · в adapter.go этап 1 меняет только http.Client: параллельно идёт этап 6
- [01] MCP-обработчики получают `context.Background()` и не отменяются по сигналу; `Server.Run` без контекста (в `main` обёрнут) · internal/mcp/server.go:216 · MCP — этап 3
- [07] SBOM (syft) и SLSA provenance в релизе · .goreleaser.yaml · бриф не требует; подпись cosign уже закрывает целостность
- [07] `install.ps1` для Windows (скачать zip, sha256, PATH) · корень репозитория · пока инструкция в README, раздел «Установка»
- [02] Organization API `GET /v3/projects` для сервисного аккаунта отвечает 400 «(user_id) is invalid argument: user not found» (проверено 2026-10-14), поэтому `configure` для SA всегда просит project_id вручную · cmd/cloudru-vm/configure.go `chooseProject` · найти эндпоинт, который отдаёт проект SA (например, IAM `serviceAccounts/{sub}`), — в разрешённых в этапе 2 чтениях его не было
- [02] Описания MCP-инструментов (`deploy` и др.) называют только `CLOUDRU_KEY_ID/CLOUDRU_SECRET` · internal/mcp/tools.go:24 · тексты описаний — этап 8/9
- [02] `~/.cloudru-vm.yaml` больше не читается (заменён профилем), `doctor` предупреждает о нём · cmd/cloudru-vm/doctor.go · упомянуть в README и release notes — этап 9
- [02] Минимальные роли SA: с тестовым аккаунтом работают обмен токена, `list-zones`, `list-images`; роли на создание ресурсов не проверялись (чтение только) · docs/auth.md (DECISIONS, открытый вопрос 4) · README — этап 9, проверка ролей на запись — этап 5
- [02] Повтор при обрывах есть только у обмена IAM и у compute-адаптера; клиенты vpc/dns/mk8s/… и `adminGET` в MCP не повторяют транспортные ошибки · internal/*/client.go, internal/mcp/tools_admin.go:30 · вне scope кредов
- [03] `confirmation_required` в MCP возвращается с `isError=true`, хотя по спецификации MCP это не ошибка · internal/mcp/server.go:221 `toolResult`, все гейты в internal/mcp/tools*.go · клиента `image-factory/go/cmd/mcpcall` в этом репозитории нет, нельзя проверить, различает ли он исход по `status`, а не по `isError`; README обещает ему код 3 — оставлено как есть
- [03] Превью MCP `deploy`/`provision` без оценки цены: гейт срабатывает до обращения в облако, а размер flavor без облака неизвестен · internal/mcp/tools.go `handleDeploy`/`handleProvision` · цена есть в `deploy_plan`; при желании — оценка в превью после первого read-only запроса
- [05] cloud-init из `internal/bootstrap` (`Render`, cloudinit.yaml.tmpl) не используется и не может: compute рендерит cloud-init только из `user_data_template` образа (`image_get` для Ubuntu-24.04, ubuntu-22.04, Docker); поле `cloud_init` в `CreateVMRequest` ничем не подтверждено · internal/bootstrap/bootstrap.go:65, internal/provision/types.go:58 · Docker ставится по SSH (`bootstrap.InstallCommand`); удалить шаблон и поле или подтвердить поле в документации compute
- [05] После ошибки `docker compose up` запись остаётся `running`, повторный `deploy` без `--force` отвечает «No changes» · internal/engine/deploy.go:202 · статусы записи не в scope; ошибки SSH/Docker уже ставят свой статус и повторяются
- [05/приёмка] Открытые `deploy` порты остаются в общей группе безопасности зоны после `destroy`: удаления правила SG нет ни в `provision.CloudAPI`, ни в `destroy` · internal/provision/discovery.go, internal/engine/destroy.go · нужен `DeleteSecurityGroupRule` (DELETE /api/v1/security-groups/{id}/rules/{rule_id} — проверить вживую) и учёт открытых правил в state
- [05/приёмка] `deployplan.pickSubnet` выбирает подсеть по `availability_zone_id`, а API отдаёт его пустым (проверено 2026-10-14) — в зоне, отличной от первой подсети, MCP `deploy_plan` возьмёт подсеть чужой зоны · internal/deployplan/resolve.go:363 · перейти на совпадение по имени, как `engine.resolveNetwork`
- [05/приёмка] Повтор при транспортных обрывах есть у IAM (этап 2), но не у compute: `list zones` из контейнера падал с API_UNREACHABLE ~1 раз из 3 · internal/provision/adapter.go `doWithRetry` · повторять идемпотентные GET при обрыве соединения
- [05/приёмка] `deploy --dry-run` создаёт SSH-ключ в `./.cloudru-vm/` (ленивость ключа не учитывает dry-run) · internal/engine/deploy.go · создавать ключ после подтверждения
