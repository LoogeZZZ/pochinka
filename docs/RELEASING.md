# Выпуск релиза (для автора)

1. Поднять версию: `productVersion` в `wails.json` и `Version` в `internal/app/app.go`.
2. Если менялись зависимости: `bash tools/genlicenses.sh`.
3. Прогнать тесты: `go test ./... -count=1`.
4. Собрать: `wails build -skipbindings -trimpath -ldflags "-s -w"`, скопировать `build/bin/Pochinka.exe` в `release/Pochinka-X.Y.Z.exe`.
5. **Подписать:** `go run ./tools/signer sign release/Pochinka-X.Y.Z.exe` → появится `.exe.sig`.
   Проверить: `go run ./tools/signer verify release/Pochinka-X.Y.Z.exe <публичный ключ из internal/selfupdate>`.
6. Создать релиз `vX.Y.Z` и приложить **оба файла**: `Pochinka-X.Y.Z.exe` и `Pochinka-X.Y.Z.exe.sig`.
   Без `.sig` (или с подписью другим ключом) встроенное обновление откажется ставить версию.

Ключ подписи лежит в `%USERPROFILE%\.pochinka-release\signing.key`, в репозиторий не попадает. Держите резервную копию: без ключа новые версии не подписать.
