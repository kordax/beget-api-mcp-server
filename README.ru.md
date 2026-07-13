# MCP-сервер для Beget API

[![Tests](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Tests.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Tests.yml)
[![Lint](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Lint.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Lint.yml)
[![Security](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Security.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/Security.yml)
[![Gitleaks](https://github.com/kordax/beget-api-mcp-server/actions/workflows/gitleaks.yml/badge.svg?branch=main)](https://github.com/kordax/beget-api-mcp-server/actions/workflows/gitleaks.yml)
[![Coverage](https://raw.githubusercontent.com/kordax/beget-api-mcp-server/badges/.badges/main/coverage.svg)](https://github.com/kordax/beget-api-mcp-server/tree/badges)

Русская документация переведена с помощью ИИ.

Я сделал этот MCP-сервер для управления обычным хостингом Beget из разных клиентов. В примерах встречается Codex, но сам сервер от него не зависит. Он так же работает с JetBrains AI Assistant, Claude Desktop, Cursor, VS Code и другими совместимыми клиентами.

Сервер написан на Go, а весь граф зависимостей собран на Uber Fx. `app.Run` выбирает запуск MCP или команды управления credentials, при этом оба пути получают зависимости через Fx. Конфигурация, системный keyring, HTTP-клиент, адаптер Beget, MCP и жизненный цикл процесса оформлены отдельными модулями.

## Что уже можно делать

Без изменения аккаунта:

- посмотреть тариф, сервер и лимиты аккаунта
- получить список сайтов и доменов
- посмотреть DNS-записи домена
- получить список задач cron и резервных копий
- посмотреть среднюю нагрузку сайтов и баз данных

С изменением аккаунта:

- заменить группу DNS-записей
- заморозить файлы сайта
- снять заморозку с сайта

У изменяющих инструментов есть два уровня защиты. MCP-клиент видит пометку destructive, а сервер требует явно передать `confirm: true`. Без подтверждения запрос к Beget не выполняется.

Я не добавлял универсальный инструмент для произвольных методов API. Такой инструмент проще написать, но им намного легче случайно удалить или изменить что-нибудь важное.

## Версии и зависимости

Проект рассчитан на Go 1.26 и фиксирует toolchain `go1.26.5`.

Основные зависимости:

- Uber Fx 1.24.0 отвечает за сборку приложения
- Testify 1.11.1 используется во всех тестах через `assert` и `require`
- `basic-utils/v3` 3.4.0 помогает аккуратно читать настройки окружения
- `go-keyring` 0.2.8 работает с системными хранилищами секретов
- официальный Go SDK реализует протокол MCP

## Транспорты

Сервер поддерживает три взаимоисключающих транспорта:

- stdio используется по умолчанию и явно включается через `--stdio`
- Streamable HTTP включается через `--streamable-http`
- старый SSE включается через `--sse`

HTTP-транспорты по умолчанию слушают `127.0.0.1:8080` и не могут привязаться к внешнему адресу. Для endpoint, поведения сессий, формата ответа и опциональной bearer-авторизации предусмотрены отдельные флаги.

Все флаги и примеры настройки клиентов собраны в [инструкции по транспортам](docs/transports.ru.md).

## Сборка

```bash
go build -o bin/beget-api-mcp-server ./cmd/beget-api-mcp-server
```

Проверки перед коммитом:

```bash
go fmt ./...
go vet ./...
go test -race ./...
```

Те же проверки запускаются в GitHub Actions. Отчет покрытия сохраняется как артефакт сборки.

Полный набор тестов, проверки покрытия, линтеров, поиска уязвимостей, статического анализа безопасности и поиска секретов запускается командой `task verify`. Перед первым запуском нужно установить зафиксированные версии инструментов через `task tools`. Обязательный порог покрытия составляет 90%, текущий набор тестов покрывает 94,9%, а badge публикуется из ветки `badges`. Те же категории проверок выполняются в GitHub Actions, а Dependabot следит за модулями Go и workflow.

Команда `task mcp-inspector` запускает зафиксированную версию официального MCP Inspector для интерактивной проверки протокола и инструментов. Для нее нужны Node.js, npm и доступная команда `npx`.

## Установка в систему

Последний выпуск можно глобально установить для текущего пользователя Linux или macOS одной командой:

```bash
curl -fsSL https://raw.githubusercontent.com/kordax/beget-api-mcp-server/main/install.sh | sh
```

Для Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/kordax/beget-api-mcp-server/main/install.ps1 | iex
```

Установщик сам определяет систему и архитектуру, проверяет контрольную сумму выпуска и добавляет команду в пользовательский `PATH`. Ручная установка, обновление, удаление и настройка MCP-клиентов описаны в [отдельной инструкции](docs/installation.ru.md).

## Настройка доступа

Сначала в панели Beget нужно включить доступ к Hosting API и задать отдельный пароль для API. Обычный пароль от панели лучше не использовать.

Учетные данные можно один раз сохранить в системном хранилище секретов:

```bash
beget-api-mcp-server credentials set --login your-login
```

Команда скрыто запрашивает API-ключ в терминале. Для автоматизации ключ можно передать через stdin. На Linux используется Secret Service, на macOS Keychain, на Windows Credential Manager. Проверить наличие или удалить данные можно без вывода их значений:

```bash
beget-api-mcp-server credentials check
beget-api-mcp-server credentials delete
```

После этого MCP-клиенту достаточно указать команду:

```toml
[mcp_servers.beget]
command = "beget-api-mcp-server"
```

Stdio является транспортом по умолчанию, поэтому указывать его аргументом не требуется.

MCP-сервер запускается и без настроенных учетных данных. Агент может вызвать `beget_auth_status`, определить состояние авторизации и получить безопасную инструкцию. Остальные инструменты Beget проверяют авторизацию только при вызове. Установщик также добавляет Codex skill `beget-api`, который описывает этот порядок работы и запрещает передавать API-ключ через аргументы MCP.

Переменные `BEGET_API_LOGIN` и `BEGET_API_KEY` продолжают поддерживаться и имеют приоритет над сохраненными значениями. Они подходят для контейнеров, CI, headless Linux без Secret Service и запуска через внешний менеджер паролей.

Ключ уходит в Beget внутри HTTPS POST-запроса. Он не попадает в URL, логи, аргументы MCP-инструментов или ответы. Тесты работают только с локальным HTTP-сервером и вымышленными данными.

## Границы проекта

Проект намеренно поддерживает классический Hosting API по адресу `https://api.beget.com/api`. Расширение или изменение этого внешнего API не входит в область репозитория: сервер остается небольшим типизированным адаптером над поддерживаемыми операциями.

## Автор и лицензия

Автор: Dmitry Morozov, kordax. Почта: `kordaxmint@gmail.com`.

Код распространяется по лицензии MIT. Полный текст находится в [LICENSE](LICENSE).
