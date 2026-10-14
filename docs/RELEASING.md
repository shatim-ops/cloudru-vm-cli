# Выпуск релиза

Версии — SemVer, тег `vMAJOR.MINOR.PATCH[-rc.N]`. Первый публичный — `v1.0.0-rc.1`.
**minor** — новые команды и инструменты; **patch** — исправления;
ломающие изменения CLI (команды, флаги, коды возврата, формат `--json`) — только в **major**.

1. `main` зелёный в CI; локально `goreleaser check` и, по желанию,
   `goreleaser release --snapshot --clean --skip=sign` — в `dist/` 6 бинарей и `checksums.txt`.
2. `git tag -a v1.2.3 -m v1.2.3 && git push origin v1.2.3` — тег ставится на коммит в `main`.
3. `release.yml` гоняет весь `ci.yml`, затем goreleaser: архивы, `checksums.txt`,
   подписи cosign keyless (`*.sigstore.json`), GitHub Release. `-rc.N` публикуется как pre-release.
4. Проверить подпись скачанного `checksums.txt` (пример для `v1.2.3`):

   ```
   cosign verify-blob --bundle checksums.txt.sigstore.json \
     --certificate-identity-regexp '^https://github.com/shatim-ops/cloudru-vm-cli/\.github/workflows/release\.yml@refs/tags/v' \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
   sha256sum --ignore-missing -c checksums.txt
   ```

5. Неудачный релиз: удалить Release и тег (`git push --delete origin v1.2.3`), исправить, выпустить следующий patch — опубликованный номер не переиспользовать.

Версия goreleaser зафиксирована в `ci.yml` и `release.yml` (`v2.18.3`); поднимать в обоих сразу.
