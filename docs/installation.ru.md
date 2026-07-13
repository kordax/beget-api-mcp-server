# Установка в систему

Эта инструкция устанавливает сервер один раз для текущего пользователя и подключает его к MCP-клиенту. Codex здесь является одним из примеров. В основной настройке используется stdio. Streamable HTTP и старый SSE описаны в [инструкции по транспортам](transports.ru.md).

## Что понадобится

Готовые архивы не требуют установленного Go. На странице [GitHub Releases](https://github.com/kordax/beget-api-mcp-server/releases) нужно выбрать Linux, macOS или Windows и архитектуру `amd64` или `arm64`. Каждый выпуск содержит `checksums.txt` для проверки SHA-256.

Для установки из исходников нужен Go 1.26.5:

```bash
go version
```

## Первая установка

Архив выпуска нужно распаковать, а `beget-api-mcp-server` или `beget-api-mcp-server.exe` перенести в каталог из `PATH`.

Другой вариант: установить актуальную тегированную версию через Go:

```bash
mkdir -p "$HOME/.local/bin"
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

Проверка без запуска MCP-сервера:

```bash
test -x "$HOME/.local/bin/beget-api-mcp-server"
```

Добавлять каталог в `PATH` необязательно, потому что в конфигурации MCP можно использовать абсолютный путь.

## Сохранение учетных данных Beget

В панели Beget нужно включить Hosting API и создать отдельный пароль API. Логин и пароль сохраняются в системном хранилище секретов:

```bash
beget-api-mcp-server credentials set --login your-beget-login
```

API-ключ вводится через скрытый запрос терминала и не принимается в аргументах команды. На Linux используется Secret Service, на macOS Keychain, на Windows Credential Manager.

Проверить наличие или удалить сохраненные данные можно без вывода их значений:

```bash
beget-api-mcp-server credentials check
beget-api-mcp-server credentials delete
```

В Linux desktop-сессиях Secret Service обычно уже доступен. На headless-сервере без Secret Service нужно использовать переменные окружения из раздела ниже.

## Универсальный контракт MCP

По умолчанию сервер работает через stdio и загружает данные из системного keyring. Он не обращается к API Codex и не требует специальной обертки запуска. Обычный MCP-клиент с JSON-конфигурацией может запускать его так:

```json
{
  "mcpServers": {
    "beget": {
      "command": "/home/your-user/.local/bin/beget-api-mcp-server",
      "args": ["--stdio"]
    }
  }
}
```

Нужно заменить `/home/your-user` на настоящий путь. Такой формат подходит клиентам, которые используют распространенную JSON-структуру `mcpServers`.

## Пример для Codex

Codex использует TOML вместо JSON-структуры выше. В `~/.codex/config.toml` можно добавить:

```toml
[mcp_servers.beget]
command = "/home/your-user/.local/bin/beget-api-mcp-server"
```

После изменения конфигурации Codex нужно перезапустить. Это только представление того же универсального контракта команды и переменных окружения в формате Codex.

## Пример для JetBrains и GoLand

GoLand и другие актуальные JetBrains IDE умеют запускать локальные MCP-серверы через stdio. Нужно открыть `Settings | Tools | AI Assistant | Model Context Protocol (MCP)`, нажать `Add`, выбрать JSON-конфигурацию для STDIO и установить уровень сервера `Global`.

Сервер Beget можно добавить такой JSON-конфигурацией:

```json
{
  "mcpServers": {
    "beget": {
      "command": "/home/your-user/.local/bin/beget-api-mcp-server",
      "args": ["--stdio"]
    }
  }
}
```

После сохранения нужно нажать `OK`, затем `Apply`. В колонке статуса должно появиться успешное подключение, а в списке инструментов должны быть команды Beget. Если автоматический запуск отключен, сервер нужно включить вручную или нажать `Reconnect`.

Чтобы пользовательские MCP-серверы были доступны Junie, нужно открыть `Settings | Tools | AI Assistant | Agents` и включить `Pass custom MCP servers`.

Если процесс не запускается, нужно открыть `Help | Show Log in Explorer`, перейти в каталог `mcp` и посмотреть лог сервера Beget. Чаще всего у IDE, запущенной с рабочего стола, проблема оказывается в неправильном пути к бинарнику.

## Хранение секрета

Для локальной установки рекомендуется встроенная работа с системным keyring. Если заданы переменные окружения, они имеют приоритет:

```bash
BEGET_API_LOGIN=your-beget-login \
BEGET_API_KEY=your-api-password \
beget-api-mcp-server --stdio
```

Этот fallback предназначен для контейнеров, CI, headless-систем и внешних менеджеров паролей. API-ключ нельзя помещать в аргументы процесса или коммитить в конфигурацию MCP.

## Установка из локального клона

Этот вариант удобен во время разработки сервера и требует Git:

```bash
git clone https://github.com/kordax/beget-api-mcp-server.git
cd beget-api-mcp-server
GOBIN="$HOME/.local/bin" go install ./cmd/beget-api-mcp-server
```

Конфигурацию клиента менять не придется, потому что путь установленного бинарника останется тем же.

## Обновление

Можно скачать новый архив и заменить бинарник либо повторить команду установки через Go:

```bash
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

После обновления нужно перезапустить MCP-сервер или выполнить повторное подключение в клиенте.

## Удаление

Нужно выполнить `beget-api-mcp-server credentials delete`, убрать сервер Beget из конфигурации MCP-клиента и удалить установленный бинарник.

## Про безопасность

Ключ API отправляется в Beget только внутри HTTPS POST-запроса. Сервер не помещает его в URL, логи, аргументы MCP-инструментов или результаты. Способ передачи ключа процессу выбирает MCP-клиент или используемый менеджер секретов.
