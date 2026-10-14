# cloudru-vm

Командная строка и MCP-сервер для [Cloud.ru Evolution](https://cloud.ru):
виртуальные машины, диски, сети, Managed Kubernetes, PostgreSQL, Object
Storage, Secret Manager — и развёртывание `docker-compose`-проекта на ВМ одной
командой.

- **CLI для человека и скриптов**: `cloudru-vm vm list`, `cloudru-vm deploy`,
  `cloudru-vm k8s kubeconfig …`; у каждой команды есть `--json` и
  [стабильные коды выхода](docs/exit-codes.md).
- **MCP-сервер для AI-агентов** (Claude Code, Cursor и др.): `cloudru-vm mcp`
  по stdio или Streamable HTTP, в том числе из Docker-образа. Те же
  инструменты доступны из CLI: `cloudru-vm tools list|describe|call`.
- **Ничего платного без согласия**: создание ресурсов и удаление данных
  показывают превью (с оценкой цены, где она есть) и ждут подтверждения —
  одинаково у человека и у агента ([модель подтверждений](#подтверждения)).

## Установка

Linux и macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/shatim-ops/cloudru-vm-cli/main/install.sh | sh
cloudru-vm version
```

Скрипт проверяет sha256 архива и, если установлен `cosign`, подпись релиза.
Windows, ручная установка, проверка подписи, Gatekeeper и SmartScreen —
[docs/install.md](docs/install.md).

## Быстрый старт

Нужен ключ доступа сервисного аккаунта (Key ID и Key Secret) и UUID проекта —
как их получить, в [docs/auth.md](docs/auth.md).

```sh
cloudru-vm configure          # ключ и проект → профиль "default"
cloudru-vm whoami             # ключ принят IAM, какой проект выбран
cloudru-vm doctor             # конфиг, права файлов, доступность API
cloudru-vm vm list            # ВМ проекта

cd my-app                     # каталог с docker-compose.yml
cloudru-vm init               # .cloudru-vm.yaml: зона и образ из API
cloudru-vm deploy --dry-run   # что будет создано и сколько стоит; ничего не создаёт
cloudru-vm deploy             # ВМ + публичный IP + docker compose up -d
cloudru-vm verify             # порты приложения отвечают
cloudru-vm logs -f            # логи контейнеров
cloudru-vm destroy            # удалить ВМ и освободить IP
```

Для `deploy` машине, с которой он запускается, нужен исходящий доступ к
порту 22 созданной ВМ (SSH). Зону лучше выбрать явно: подробности и другие
известные ограничения — в [docs/troubleshooting.md](docs/troubleshooting.md).

## Что ещё умеет

| Команда | Что делает |
|---|---|
| `vm`, `disk`, `vpc` | ВМ, диски, сети, подсети, группы безопасности, floating IP |
| `k8s` | кластеры Managed Kubernetes, node pool'ы, kubeconfig, `kubectl`/`helm` |
| `pg` | кластеры PostgreSQL, базы, пользователи, бэкапы |
| `s3` | бакеты и объекты Object Storage |
| `secret` | Secret Manager |
| `tools list` / `describe` / `call` | любой из ~150 инструментов MCP-сервера, включая DNS, балансировщики, Kafka, Redis, Artifact Registry, сертификаты, мониторинг, биллинг |
| `deploy`, `status`, `logs`, `verify`, `destroy` | сценарий «compose → ВМ» |
| `configure`, `whoami`, `doctor` | профили, проверка ключа и окружения |
| `mcp` | MCP-сервер для агентов |

Полная справка по всем командам и флагам — [docs/commands.md](docs/commands.md)
(генерируется из бинаря). Справка самого CLI (`--help`) — на английском.

## Подтверждения

Команды, которые создают платные ресурсы или удаляют данные (`deploy`,
`destroy`, `vm create`, `disk delete`, `tools call` изменяющих инструментов…),
сначала показывают, что произойдёт. В терминале спрашивают `[y/N]`; `--yes`
(`-y`) — согласие заранее; `--dry-run` — только превью, код выхода `0`. Без
терминала и без `--yes` команда ничего не делает и выходит с кодом `3`, а с
`--json` печатает в stdout `{"status":"confirmation_required","preview":{…}}`.
MCP-сервер держит ту же границу: изменяющий инструмент без `confirmed=true`
возвращает `status: confirmation_required` и превью, действие выполняется
только повторным вызовом с `confirmed=true` — после согласия человека.

```sh
cloudru-vm disk delete 1b2c… --dry-run   # что будет удалено
cloudru-vm disk delete 1b2c… --yes       # удалить без вопроса (скрипт, CI)
cloudru-vm disk delete 1b2c… </dev/null; echo $?   # 3: без терминала и без --yes ничего не удалено
```

Цена в превью — **оценка** по встроенной таблице тарифов, а не счёт: публичного
API цен у Cloud.ru нет.

## Документация

| | |
|---|---|
| [docs/install.md](docs/install.md) | установка на Linux, macOS, Windows; проверка подписи; kubectl/helm |
| [docs/auth.md](docs/auth.md) | сервисный аккаунт и ключ, роли, профили, все переменные окружения, что хранится на диске, ротация ключа |
| [docs/mcp.md](docs/mcp.md) | MCP по stdio и HTTP, конфиги Claude Code и Cursor, Docker-образ |
| [docs/commands.md](docs/commands.md) | справка по всем командам |
| [docs/exit-codes.md](docs/exit-codes.md) | коды выхода и формат ошибок `--json` |
| [docs/troubleshooting.md](docs/troubleshooting.md) | типовые ошибки и что приложить к обращению |
| [docs/RELEASING.md](docs/RELEASING.md) | для сопровождающих: выпуск релиза |

**Переход со старых версий:** файл `~/.cloudru-vm.yaml` больше не читается —
ключ и проект живут в профиле (`cloudru-vm configure`), а настройки проекта —
в `./.cloudru-vm.yaml`. `cloudru-vm doctor` предупреждает, если старый файл
остался. `CLOUDRU_API_KEY=keyId:secret` пока работает, но устарел: используйте
`CLOUDRU_KEY_ID` и `CLOUDRU_SECRET`.

## Поддержка

Ошибки и вопросы — в [Issues](https://github.com/shatim-ops/cloudru-vm-cli/issues)
этого репозитория. Приложите вывод `cloudru-vm doctor --json` и
`cloudru-vm version` (секретов в них нет) — подробнее в
[docs/troubleshooting.md](docs/troubleshooting.md#обращение-в-поддержку).

## Сборка из исходников

```sh
go build -o bin/cloudru-vm ./cmd/cloudru-vm
gofmt -l . && go vet ./... && go test -race ./... && govulncheck ./...
```

Нужен Go из `go.mod`; CI гоняет то же на Linux, macOS и Windows
(`.github/workflows/ci.yml`). После изменения команд или флагов обновите
справку: `go test ./cmd/cloudru-vm -run TestCommandsDoc -update-docs`.
Внутренние планы и история проекта — `docs/internal/`.

## Лицензия

Apache-2.0 (см. `LICENSE` и `NOTICE`). Правообладатель — ООО «Облачные
технологии» (Cloud.ru).
