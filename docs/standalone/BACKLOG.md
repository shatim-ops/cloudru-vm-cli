# BACKLOG (вне scope v1.0)

Сюда исполнитель этапов записывает найденное вне своего scope. Формат:
`- [этап-источник] что · где (file:line) · почему не сейчас`.

Известное на старте (из аудита 2026-10-14):

- [audit] OS keyring (Keychain / Credential Manager / Secret Service) · internal/auth · файл 0600 достаточен для v1.0
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
- [audit] `id.cloud.ru/auth/system/openid/token` (mk8s kubeconfig) не найден в документации; quickstart называет `auth.iam.cloud.ru` — проверить вживую до релиза
- [audit] Хост billing API и `CLOUDRU_AGREEMENT_ID`: документация хоста не называет; подтвердить
- [audit] Публичный API цен Cloud.ru отсутствует; `internal/pricing` — оценка по зашитой карте. Для внешнего продукта нужен источник или явный disclaimer в каждом превью
