# Fable · финальный аудит v1.0.0 · этап 3/5 · контракт ошибок, безопасность, CI/релиз

Голова PR #89: `9386ef815c254eb237ff7dfab8aad2359d6510c1`. Прогоны на этой голове (Linux amd64, go1.26):

| Проверка | Результат |
|---|---|
| `gofmt -l .` | пусто |
| `go vet ./...` | rc=0 |
| `GORACE=atexit_sleep_ms=0 go test -race -count=1 ./...` | все 33 пакета `ok`, FAIL нет |
| `bash .github/scripts/redact_test.sh` | «все образцы замаскированы верно» |
| `govulncheck ./...` (v1.9.0) | 0 вызываемых; 1 в требуемом модуле без вызова |
| `scripts/cloudru-specs.py` + `scripts/api-contract-check.py --specs` | 205 строк: **0 error**, ok=83, warn=37, accepted=62, info=23 (как у Grok на 60f3e13: warn те же `read-unknown` billing/compute и `query-unknown:project_id`) |

## Находки

**[low]** `internal/tools/tools_k8s.go:486-496` (уточнение к этапу 2) — по спецификации mk8s (`NodesNetworkConfigurationRequest.securityGroupId`: «Опциональное поле») API принимает пул без группы безопасности, так что сценарий «нет SG в зоне → пул без группы» реален, не 400. Серьёзность оставляю medium на этапе 2; здесь — подтверждение гипотезы по спецификации (вживую не проверено).

**[low]** `internal/tools/errreason.go:128-131` — `unreachable()` считает `context.Canceled` «нет связи» → `API_UNREACHABLE`. Путь: клиент MCP шлёт `notifications/cancelled` во время долгого `pg_operation` → ответ инструмента `reason: API_UNREACHABLE` «Cloud.ru недоступен», хотя отменил сам клиент. Код выхода тот же (1), страдает только диагностика. Исправление: `Canceled` → отдельная причина (`INTERRUPTED`, как у CLI) либо `USAGE`.

**[nit]** `.github/workflows/tag.yml:48` — единственный checkout без `persist-credentials: false`; это осознанно (A-023 оспорено верно: `git push` тега нужен токен) и права job ограничены `contents: write`. Альтернатива без сохранения токена в `.git/config` — `git push https://x-access-token:${GITHUB_TOKEN}@…` через переменную окружения; не блокер.

**[nit]** `.github/workflows/release.yml:84-87` — `AR_SECRET` задан на уровне job: виден всем шагам, включая `docker build` и smoke, которым он не нужен. Перенести в `env` шага `docker push`.

## Проверено без замечаний

- **Контракт ошибок.** `errreason.uncodedReason`: статус API → `API_ERROR`/`NOT_FOUND`/`API_RATE_LIMITED`; сеть/DNS/дедлайн → `API_UNREACHABLE`; kubectl/helm — по выводу; нет бинаря → `CONFIG_INVALID`; локальное → `USAGE`/`NOT_FOUND`. `SERVER_BUSY` добавлен в `knownCodes`, выход 1, описан в `docs/exit-codes.md:10` и `docs/mcp.md:115`. `docs/exit-codes.md` обновлён под R-06, partial, 409, destroy unconfirmed. `argcheck`: типы скаляров по схеме (`json.Number`, Go-int из стека), массивы/объекты сознательно не проверяются и это задокументировано в `docs/mcp.md:104-107`.
- **MCP HTTP.** Схема `Bearer` без учёта регистра, токен — `ConstantTimeCompare` после `TrimSpace` (двойной пробел проходит, как и ответил автор Qwen); лимиты: тело `maxMessageSize`, батч 100, 32 параллельных вызова → `SERVER_BUSY`; 415 без `application/json`; неизвестный инструмент и инструмент вне `--toolsets` → `-32602`; `--allow-project` проверяется в фабрике движка для всех инструментов; `redactSecrets` маскирует по именам ключей (`password`, `secret_key`, `private_key`/`privateKey`, `client_secret`, …) — `cert_private_key` после PEM-декодирования остаётся под маской (ключ `privateKey` не переименован).
- **kubectl/helm.** `--kuberc` отклонён и в `CheckConnectionFlags` (оба транспорта), и в `kubectlHTTPDeniedFlags`; `childEnv` убирает `CLOUDRU_*`, `EVOLUTION_SERVICE_ACCOUNT_KEY_*`, `MCP_BEARER_TOKEN`, свой `KUBERC` и добавляет `KUBERC=off`; тесты на оба. Таймаут kubectl оборачивает `DeadlineExceeded` → `API_UNREACHABLE`.
- **Секреты в argv/логах.** `docker login --password-stdin`; `configure --secret-stdin`; `--meta поле=-` из stdin без эха; `ca_deploy.env` в превью — только имена; `pg_user_create password` в argv — известное L-20 (v1.1). `--verbose` заголовок `Authorization` не дампится (нет `DumpRequest`).
- **Workflows.** `permissions: {}` на уровне workflow в release/tag/grok/qwen, права по job; `persist-credentials: false` во всех checkout кроме tag.yml (см. nit); `inputs.*`/`github.event.*` попадают в `run:` только через `env`, в `if:` — `author_association == 'OWNER'`; входы `tag.yml` валидируются regex до использования; `cloud-e2e`: `concurrency` без отмены, уборка `if: always()` на каждом создающем job, вывод через `redact.sed`, `dbaas-delete.sh` трогает только `evo-e2e-*`; `release.yml`: `ci` обязателен, `cache: false` у подписываемой сборки, actions закреплены по SHA, smoke общий `.github/scripts/mcp-smoke.sh` с `Content-Type: application/json` (R-01 закрыт), `latest` не двигается pre-release.
- **Поставка.** `.goreleaser.yaml`: cosign keyless подписи `checksums.txt` и каждого архива (`*.sigstore.json`); `install.sh`: `curl --proto '=https' --tlsv1.2`, `cosign verify-blob` с identity `release.yml@refs/tags/<tag>` и issuer GitHub, fallback `--new-bundle-format`, sha256 против `checksums.txt`, старый identity только для rc.1/rc.2; `Dockerfile`: оба базовых образа по digest, distroless nonroot.
