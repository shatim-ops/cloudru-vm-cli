# MCP-сервер

`cloudru-vm mcp` — сервер [Model Context Protocol](https://modelcontextprotocol.io):
AI-агент (Claude Code, Cursor и любой другой MCP-клиент) получает ~150
инструментов Cloud.ru — ВМ, диски, сети, DNS, балансировщики, Kubernetes,
PostgreSQL, Kafka, Redis, S3, секреты, сертификаты, Artifact Registry,
мониторинг, биллинг, `deploy` docker-compose на ВМ. Список с описаниями и
схемами аргументов — `cloudru-vm tools list` и `cloudru-vm tools describe <имя>`;
любой инструмент можно вызвать и без агента: `cloudru-vm tools call`.

Два транспорта:

| | stdio | Streamable HTTP |
|---|---|---|
| Запуск | клиент сам запускает `cloudru-vm mcp` | `cloudru-vm mcp --http :8080` или Docker-образ |
| Для чего | агент на вашей машине | общий сервер, контейнер, Marketplace |
| Аутентификация клиента | не нужна (локальный процесс) | `Authorization: Bearer $MCP_BEARER_TOKEN` |
| Креды Cloud.ru | ваш профиль / переменные окружения | профиль / переменные окружения **сервера** |

Версии протокола: `2025-06-18`, `2025-03-26`, `2024-11-05` (сервер отвечает
той, что запросил клиент, если знает её).

Креды Cloud.ru всегда принадлежат серверу и находятся так же, как для
обычных команд ([auth.md](auth.md#6-откуда-берутся-настройки)): переменные
окружения, профиль из `cloudru-vm configure`, `./.cloudru-vm.yaml` в рабочем
каталоге. Клиент их не передаёт и не видит.

## Подтверждения

Изменяющий инструмент (создаёт платный ресурс или удаляет данные) без
`confirmed=true` ничего не делает и возвращает превью:

```json
{"status": "confirmation_required", "message": "deploy creates a billable VM … call again with confirmed=true.", "will_deploy": {…}}
```

Поле `status` одинаково у всех инструментов; остальные поля превью (что
будет создано или удалено) у каждого свои.

Агент должен показать превью человеку и, получив согласие, вызвать тот же
инструмент с теми же аргументами и `"confirmed": true`. Граница проходит в
сервере, до обращения к облаку: агент, который «сам решил» удалить кластер,
упирается в неё. Поле `mutating` в `cloudru-vm tools list --json` показывает,
какие инструменты так устроены. CLI ведёт себя так же (`--yes`, `--dry-run`,
код выхода `3`, см. [README](../README.md#подтверждения)).

Сейчас ответ `confirmation_required` приходит с `isError: true` — клиенту
стоит различать исход по полю `status`, а не по `isError`. Без исключений:
`s3_bucket_create` тоже ждёт `confirmed=true`.

## stdio

```sh
cloudru-vm configure      # один раз: ключ и проект в профиль
```

### Claude Code

```sh
claude mcp add --transport stdio --scope project cloudru-vm -- cloudru-vm mcp
```

или файл `.mcp.json` в корне проекта:

```json
{
  "mcpServers": {
    "cloudru-vm": {
      "command": "cloudru-vm",
      "args": ["mcp"]
    }
  }
}
```

Другой профиль — `"args": ["--profile", "staging", "mcp"]` или
`"env": {"CLOUDRU_PROFILE": "staging"}`. Ключ можно передать и через `env`
(`CLOUDRU_KEY_ID`, `CLOUDRU_SECRET`, `CLOUDRU_PROJECT_ID`), но тогда он лежит в
файле проекта открытым текстом — профиль надёжнее.

### Cursor

`~/.cursor/mcp.json` (для всех проектов) или `.cursor/mcp.json` в проекте:

```json
{
  "mcpServers": {
    "cloudru-vm": {
      "command": "cloudru-vm",
      "args": ["mcp"]
    }
  }
}
```

Если клиент не находит бинарь (у GUI-приложений на macOS свой `PATH`), укажите
полный путь: `"command": "/Users/me/.local/bin/cloudru-vm"`.

Рабочий каталог сервера — тот, в котором клиент его запускает (обычно корень
проекта). Там же `deploy` и другие инструменты ведут `./.cloudru-vm/` —
состояние и SSH-ключ к созданным ВМ ([auth.md](auth.md#что-хранится-на-машине)).
По stdio stdout занят протоколом; человекочитаемого вывода нет.

## Streamable HTTP

```sh
export MCP_BEARER_TOKEN="$(openssl rand -hex 32)"
cloudru-vm mcp --http :8080
```

- `POST /mcp` — MCP Streamable HTTP; без верного `Authorization: Bearer …` —
  `401`.
- `GET /healthz` — проверка живости, без аутентификации.
- Без `MCP_BEARER_TOKEN` сервер не стартует (код `2`). Токен короче 16
  символов — предупреждение в лог.
- `--auth none` — без аутентификации, **только** на loopback-адресе
  (`--http 127.0.0.1:8080`); на `:8080` или внешнем адресе сервер откажется
  стартовать. В этом режиме заголовок `Host` тоже должен быть loopback
  (`localhost`, `127.0.0.1`, `[::1]`), иначе `403`: так страница в браузере
  не доберётся до сервера через DNS rebinding. Запросы с чужим `Origin` —
  `403` в любом режиме.
- Тело запроса — до 16 МБ (больше — `413`), и прийти оно должно за 60 с
  (иначе `408`).
- Принимаются батчи JSON-RPC (массив сообщений); на уведомления (без `id`)
  сервер не отвечает. То же по stdio: одна строка — одно сообщение или батч,
  строка длиннее 16 МБ получает ошибку, а сервер продолжает работу.
- В stderr — одна строка на вызов инструмента: имя, статус, длительность;
  аргументы не пишутся. `--verbose` — подробнее.

Сервер однопользовательский: все клиенты с токеном работают от имени одного
ключа Cloud.ru, в одном рабочем каталоге и подтверждают операции сами за себя
(`confirmed=true`). Выдавайте токен только тем, кому доверили бы и сам ключ;
наружу публикуйте только за TLS (обратный прокси).

### Claude Code

```sh
claude mcp add --transport http cloudru-vm http://localhost:8080/mcp \
  --header "Authorization: Bearer $MCP_BEARER_TOKEN"
```

или в `.mcp.json` (Claude Code подставляет `${…}` из окружения):

```json
{
  "mcpServers": {
    "cloudru-vm": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": {"Authorization": "Bearer ${MCP_BEARER_TOKEN}"}
    }
  }
}
```

### Cursor

```json
{
  "mcpServers": {
    "cloudru-vm": {
      "url": "http://localhost:8080/mcp",
      "headers": {"Authorization": "Bearer <токен>"}
    }
  }
}
```

### Ограничения HTTP-режима

Набор инструментов тот же, но часть вызовов отвечает ошибкой:

- `k8s_kubeconfig` со встроенным токеном (`embed_token`, по умолчанию) и
  `cert_private_key` — отключены, пока сервер не запущен с
  `--allow-secret-export`: секрет ушёл бы с хоста, где лежит ключ Cloud.ru.
  Без флага используйте `k8s_kubeconfig` с `embed_token=false` — kubeconfig с
  exec-плагином `cloudlogin`, без кредов внутри. По той же причине без флага
  закрыт `k8s_kubectl config …` (`config view --raw` печатает токен).
- `k8s_kubectl`, `k8s_apply`, `k8s_helm` — если на сервере нет `kubectl` /
  `helm` (в Docker-образе их нет): «not available in this mode». Запускайте
  `kubectl` у себя или используйте stdio-сервер на машине с `kubectl`.
- `secret_value` по HTTP **отдаёт** расшифрованный секрет и без
  `--allow-secret-export` — учитывайте это, выдавая токен.

## Docker-образ

Образ `cloudru-vm` — distroless, без shell, пользователь `nonroot`, слушает
`:8080`, `HEALTHCHECK` встроен (`cloudru-vm mcp healthcheck`). Релизные образы
публикуются в Artifact Registry Cloud.ru (`<реестр>.cr.cloud.ru/cloudru-vm:<версия>`,
адрес реестра — в карточке Marketplace); собрать самому:

```sh
docker build -t cloudru-vm:local .
```

Запуск — креды Cloud.ru и токен передаются переменными окружения (в образе
нет профиля):

```sh
export MCP_BEARER_TOKEN="$(openssl rand -hex 32)"
docker run -d --name cloudru-vm -p 127.0.0.1:8080:8080 \
  -e MCP_BEARER_TOKEN \
  -e CLOUDRU_KEY_ID -e CLOUDRU_SECRET -e CLOUDRU_PROJECT_ID \
  -e CLOUDRU_REGION \
  cloudru-vm:local

curl -s localhost:8080/healthz
docker logs -f cloudru-vm
```

`-e NAME` без значения берёт его из вашего окружения — секрет не попадает в
историю оболочки. Лучше — `--env-file` с правами `0600`. Все переменные — в
[auth.md](auth.md#7-переменные-окружения); для S3 добавьте `CLOUDRU_S3_*`.

Состояние (`.cloudru-vm/` с SSH-ключом к созданным `deploy` ВМ, кэш токена)
живёт в `/home/nonroot` контейнера и пропадает вместе с ним. Если сервер будет
создавать ВМ, подключите том: `-v cloudru-vm-data:/home/nonroot` — иначе после
пересоздания контейнера к этим ВМ не будет SSH-доступа.

Без `MCP_BEARER_TOKEN` контейнер завершается с кодом `2`.
