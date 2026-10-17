# Коды выхода

Коды — часть публичного контракта: скрипты на них опираются, поэтому значения
не меняются, только добавляются новые. Тот же список печатает
`evo --help`.

| Код | Значение | Типичные причины (внутренний код ошибки) |
|---|---|---|
| `0` | успех; также `--dry-run` | |
| `1` | прочая ошибка, в том числе неизвестная команда или флаг | `USAGE`, `SSH_TIMEOUT`, `SSH_AUTH_FAILED`, `SSH_HOST_KEY_CHANGED` (ключ хоста ВМ не совпал с записанным в known_hosts проекта), `VM_PROVISION_FAILED`, `COMPOSE_INVALID`, `COMPOSE_NOT_FOUND`, `API_UNREACHABLE`, `STATE_CORRUPTED` (битый или более новый state), `STATE_LOCKED` (в каталоге уже идёт `deploy`/`destroy`); `verify`: сервис не отвечает (`UNHEALTHY`) |
| `2` | ошибка аутентификации или конфигурации | `AUTH_REQUIRED` (нет ключа), `AUTH_FAILED` (IAM отверг ключ), `AUTH_EXPIRED`, `CONFIG_INVALID` (нет профиля, битый YAML, не задан `MCP_BEARER_TOKEN`) |
| `3` | нужно подтверждение, а его нет | `CONFIRMATION_REQUIRED`: нет терминала и нет `--yes`, `--non-interactive`, ответ не «y» |
| `4` | объект не найден | `VM_NOT_FOUND`, `NOT_FOUND` |
| `5` | API Cloud.ru ответил ошибкой (HTTP 4xx/5xx, кроме аутентификации при обмене ключа) | `API_ERROR`, `API_RATE_LIMITED` |
| `130` | прервано пользователем (Ctrl-C, SIGTERM) | |

Особенности:

- `tools call` и команды деревьев (`vm`, `image`, `disk`, `vpc`, `k8s`, `pg`,
  `s3`, `secret`): ошибка инструмента с кодом из таблицы даёт код выхода этой
  таблицы, `reason` и подсказку — как та же ошибка у других команд
  (`API_UNREACHABLE` — `1`, как у `whoami`; `USAGE` — `1`, если инструмент
  отверг аргументы до обращения к облаку; `2` — креды, в том числе нет
  ключей Object Storage). Код CLI берёт из поля `reason` ответа (у ответов
  старого вида — из префикса `КОД: ` сообщения); `reason` есть у каждой
  ошибки инструмента, см. [MCP](mcp.md#ошибки). Ошибка без своего
  кода: `4`, если инструмент ответил, что объект не найден, иначе `5`
  (`API_ERROR`).
- Образ по имени (`evo image get`, `evo vm create --image`, `evo deploy
  --image`, `image:` в `.evo.yaml`, `evo init --image`, `image_get`,
  `provision`): несколько совпадений — `1` (`USAGE`, в сообщении до 10
  кандидатов), ни одного — `4` (`NOT_FOUND`, подсказка
  `evo image list <запрос>`).
- Образ есть в общем каталоге Cloud.ru, но недоступен проекту (его нет в
  каталоге, каким его видит проект — `project_id`): `evo vm create`,
  `evo deploy`, `evo init --image`, `provision`, `deploy`, `deploy_plan` —
  `4` (`NOT_FOUND`, «образ … есть в каталоге Cloud.ru, но недоступен
  проекту»), до вопроса и до создания ВМ и floating IP; иначе API ответил бы
  422 `wrong_image`. Если и ОС без Docker, `deploy` называет сначала
  недоступность. `evo image get` такого образа — `0`, карточка с пометкой.
  MCP `provision` проверяет это при вызове с `confirmed=true`: превью в
  облако не ходит.
- Поля образа: у `evo vm create` / `provision` неизвестное поле в `--meta`
  (`image_metadata`) или незаполненное обязательное поле — `1` (`USAGE`); у
  `deploy` то же из `image_metadata:` в `.evo.yaml` — `2`
  (`CONFIG_INVALID`). Отказ всегда до создания ВМ и floating IP.
- `deploy` на образе Astra Linux, Альт или РЕД ОС — `2` (`CONFIG_INVALID`,
  до создания ВМ; `evo init --image` с таким образом — `1`, `USAGE`).
  `image` и `image_id` в одном слое настроек — `2` (`CONFIG_INVALID`).
- Плагины Kubernetes (`evo k8s addon-versions`, `evo k8s add-addon`):
  неизвестный плагин или версия — `4` (`NOT_FOUND`, подсказка
  `evo k8s addons <подстрока>`); кластер точно не подходит версии
  (Kubernetes или число узлов) — `1` (`USAGE`) до подтверждения.
- Нет развёртывания в каталоге: `status` и `destroy` — `0` (сообщают, что
  нечего показывать или удалять), `logs` и `verify` — `4`: им нечего
  проверять, и «нечего» не значит «здорово».
- `doctor`: `2`, если сломаны конфигурация или ключ; `1`, если упали только
  проверки сети.
- `401`/`403` от compute API (ВМ, диски, `deploy`, `destroy`) дают `2`
  (`AUTH_EXPIRED` / `AUTH_FAILED`). От других сервисов (vpc, dns, …) `403`
  пока даёт `5`: их клиенты статус не различают.
- Прерванный `deploy` после создания ВМ выходит с `130` и пишет, какая ВМ
  осталась и как её удалить.

## Формат ошибки

Текстом (stderr) — сообщение и подсказка следующей строкой:

```text
Ошибка: IAM отклонил ключ доступа (статус 401)
Проверьте Key ID и секрет: 'evo configure' или CLOUDRU_KEY_ID / CLOUDRU_SECRET
```

С `--json` — один объект JSON в stderr, stdout остаётся для данных:

```json
{"error":{"code":2,"reason":"AUTH_FAILED","message":"IAM отклонил ключ доступа (статус 401)","hint":"Проверьте Key ID и секрет: 'evo configure' или CLOUDRU_KEY_ID / CLOUDRU_SECRET"}}
```

Сообщения и подсказки — на языке CLI (по умолчанию русский, `EVO_LANG=en` —
английский), ключи JSON и коды — всегда английские.

Предупреждения (устаревшая переменная, права `credentials.yaml`, порты, которые
не удалось открыть) с `--json` — тоже строки JSON в stderr, перед ошибкой:
`{"warning":"…"}`. Так каждая строка stderr разбирается `jq`.

`code` — код выхода. `reason` — символьный код из таблицы (`AUTH_FAILED`);
у прерванной команды — `INTERRUPTED`, у ошибки без символьного кода —
`ERROR`. У ошибки без подсказки ключа `hint` нет. В `message` символьный код попадает только у ошибок с добавленным
контекстом (`проверка ключа доступа: AUTH_FAILED: …`). Опирайтесь на `code`
и `reason`, а не на текст: он зависит от языка.

Отказ без подтверждения с `--json` дополнительно печатает в **stdout**
`{"status":"confirmation_required","preview":{…},"hint":"…"}` — превью того,
что было бы сделано; `--dry-run --json` печатает
`{"status":"dry_run","preview":{…}}` и выходит с `0`.

## В скриптах

```sh
evo deploy --yes --json > deploy.json
case $? in
  0)   echo "развёрнуто" ;;
  2)   echo "проверьте ключ: evo whoami" ;;
  3)   echo "нужно подтверждение" ;;
  130) echo "прервано" ;;
  *)   echo "ошибка" ;;
esac
```
