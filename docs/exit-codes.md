# Коды выхода

Коды — часть публичного контракта: скрипты на них опираются, поэтому значения
не меняются, только добавляются новые. Тот же список печатает
`cloudru-vm --help`.

| Код | Значение | Типичные причины (внутренний код ошибки) |
|---|---|---|
| `0` | успех; также `--dry-run` | |
| `1` | прочая ошибка, в том числе неизвестная команда или флаг | `USAGE`, `SSH_TIMEOUT`, `SSH_AUTH_FAILED`, `VM_PROVISION_FAILED`, `COMPOSE_INVALID`, `COMPOSE_NOT_FOUND`, `API_UNREACHABLE`; `verify`: сервис не отвечает (`UNHEALTHY`) |
| `2` | ошибка аутентификации или конфигурации | `AUTH_REQUIRED` (нет ключа), `AUTH_FAILED` (IAM отверг ключ), `AUTH_EXPIRED`, `CONFIG_INVALID` (нет профиля, битый YAML, не задан `MCP_BEARER_TOKEN`) |
| `3` | нужно подтверждение, а его нет | `CONFIRMATION_REQUIRED`: нет терминала и нет `--yes`, `--non-interactive`, ответ не «y» |
| `4` | объект не найден | `VM_NOT_FOUND`, `NOT_FOUND` |
| `5` | API Cloud.ru ответил ошибкой (HTTP 4xx/5xx, кроме аутентификации при обмене ключа) | `API_ERROR`, `API_RATE_LIMITED` |
| `130` | прервано пользователем (Ctrl-C, SIGTERM) | |

Особенности:

- `tools call` и команды деревьев (`vm`, `disk`, `vpc`, `k8s`, `pg`, `s3`,
  `secret`): `4`, если инструмент ответил, что объект не найден; `5` — любая
  другая ошибка инструмента; `2`, если ошибка — про креды.
- `doctor`: `2`, если сломаны конфигурация или ключ; `1`, если упали только
  проверки сети.
- `403` от API сервиса (compute, vpc, …) сейчас даёт `5`, а не `2`: права
  сервисного аккаунта проверяются сервисом, а не при обмене ключа.
- Прерванный `deploy` после создания ВМ выходит с `130` и пишет, какая ВМ
  осталась и как её удалить.

## Формат ошибки

Текстом (stderr) — сообщение и подсказка следующей строкой:

```text
Error: IAM rejected the credentials (status 401)
Verify the key ID and secret: 'cloudru-vm configure', or CLOUDRU_KEY_ID / CLOUDRU_SECRET
```

С `--json` — один объект JSON в stderr, stdout остаётся для данных:

```json
{"error":{"code":2,"message":"IAM rejected the credentials (status 401)","hint":"Verify the key ID and secret: 'cloudru-vm configure', or CLOUDRU_KEY_ID / CLOUDRU_SECRET"}}
```

`code` — код выхода. Символьный код из таблицы (`AUTH_FAILED`) попадает в
`message` только у ошибок с добавленным контекстом
(`checking the access key: AUTH_FAILED: …`); опирайтесь на `code`, а не на
текст.

Отказ без подтверждения с `--json` дополнительно печатает в **stdout**
`{"status":"confirmation_required","preview":{…},"hint":"…"}` — превью того,
что было бы сделано; `--dry-run --json` печатает
`{"status":"dry_run","preview":{…}}` и выходит с `0`.

## В скриптах

```sh
cloudru-vm deploy --yes --json > deploy.json
case $? in
  0)   echo "развёрнуто" ;;
  2)   echo "проверьте ключ: cloudru-vm whoami" ;;
  3)   echo "нужно подтверждение" ;;
  130) echo "прервано" ;;
  *)   echo "ошибка" ;;
esac
```
