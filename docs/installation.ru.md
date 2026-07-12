# Установка в систему

Эта инструкция устанавливает сервер один раз для текущего пользователя и подключает его к MCP-клиенту. Codex здесь является одним из примеров. В основной настройке используется stdio. Streamable HTTP и старый SSE описаны в [инструкции по транспортам](transports.ru.md).

## Что понадобится

Сначала нужны:

- Go 1.26.5
- Git
- GitHub CLI

Проверить окружение можно так:

```bash
go version
git --version
gh --version
```

## Первая установка

Репозиторий на GitHub приватный. Нужно подключить Git к текущей сессии GitHub CLI и запретить Go использовать публичный proxy для репозиториев kordax:

```bash
gh auth status
gh auth setup-git
go env -w 'GOPRIVATE=github.com/kordax/*'
```

Теперь можно установить бинарник в пользовательский каталог:

```bash
mkdir -p "$HOME/.local/bin"
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

Проверка без запуска MCP-сервера:

```bash
test -x "$HOME/.local/bin/beget-api-mcp-server"
```

Добавлять каталог в `PATH` необязательно, потому что в конфигурации MCP можно использовать абсолютный путь.

## Универсальный контракт MCP

Сервер работает через stdio и читает две переменные окружения:

- `BEGET_API_LOGIN` с логином хостинг-аккаунта
- `BEGET_API_KEY` с отдельным паролем Hosting API

Он не обращается к API Codex и не требует специальной обертки запуска. Обычный MCP-клиент с JSON-конфигурацией может запускать его так:

```json
{
  "mcpServers": {
    "beget": {
      "command": "/home/your-user/.local/bin/beget-api-mcp-server",
      "args": ["--stdio"],
      "env": {
        "BEGET_API_LOGIN": "your-beget-login",
        "BEGET_API_KEY": "your-api-password"
      }
    }
  }
}
```

Нужно заменить `/home/your-user`, логин и пароль API. Такой формат подходит клиентам, которые используют распространенную JSON-структуру `mcpServers`.

## Пример для Codex

Codex использует TOML вместо JSON-структуры выше. В `~/.codex/config.toml` можно добавить:

```toml
[mcp_servers.beget]
command = "/home/your-user/.local/bin/beget-api-mcp-server"
env = { BEGET_API_LOGIN = "your-beget-login", BEGET_API_KEY = "your-api-password" }
```

После изменения конфигурации Codex нужно перезапустить. Это только представление того же универсального контракта команды и переменных окружения в формате Codex.

## Пример для JetBrains и GoLand

GoLand и другие актуальные JetBrains IDE умеют запускать локальные MCP-серверы через stdio. Нужно открыть `Settings | Tools | AI Assistant | Model Context Protocol (MCP)`, нажать `Add`, выбрать JSON-конфигурацию для STDIO и установить уровень сервера `Global`.

Этот пример оставляет существующий Gortex и добавляет рядом Beget:

```json
{
  "mcpServers": {
    "gortex": {
      "command": "gortex",
      "args": [
        "mcp",
        "--proxy"
      ]
    },
    "beget": {
      "command": "/home/your-user/.local/bin/beget-api-mcp-server",
      "args": ["--stdio"],
      "env": {
        "BEGET_API_LOGIN": "your-beget-login",
        "BEGET_API_KEY": "your-api-password"
      }
    }
  }
}
```

После сохранения нужно нажать `OK`, затем `Apply`. В колонке статуса должно появиться успешное подключение, а в списке инструментов должны быть команды Beget. Если автоматический запуск отключен, сервер нужно включить вручную или нажать `Reconnect`.

Чтобы пользовательские MCP-серверы были доступны Junie, нужно открыть `Settings | Tools | AI Assistant | Agents` и включить `Pass custom MCP servers`.

Если процесс не запускается, нужно открыть `Help | Show Log in Explorer`, перейти в каталог `mcp` и посмотреть лог сервера Beget. Чаще всего у IDE, запущенной с рабочего стола, проблема оказывается в неправильном пути к бинарнику.

## Хранение секрета

Передача `BEGET_API_KEY` прямо в конфигурации клиента является самым совместимым вариантом, но пароль хранится открытым текстом. Если MCP-клиент умеет работать с защищенными секретами, лучше использовать эту возможность. Другой хороший вариант: внешний менеджер паролей, который передает переменную окружения только дочернему процессу.

`codex-keyring` является самописной утилитой из моего локального окружения. Она не распространяется вместе с этим проектом и не является обязательной. Моя локальная конфигурация Codex использует ее так:

```toml
[mcp_servers.beget]
command = "codex-keyring"
args = ["run", "beget-api-key", "--", "/home/your-user/.local/bin/beget-api-mcp-server"]
env = { BEGET_API_LOGIN = "your-beget-login" }
```

Другим пользователям нужно подставить свой менеджер паролей или использовать прямую передачу переменных окружения.

## Установка из локального клона

Этот вариант удобен во время разработки сервера:

```bash
git clone git@github.com:kordax/beget-api-mcp-server.git
cd beget-api-mcp-server
GOBIN="$HOME/.local/bin" go install ./cmd/beget-api-mcp-server
```

Конфигурацию клиента менять не придется, потому что путь установленного бинарника останется тем же.

## Обновление

Достаточно повторить команду установки:

```bash
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

После обновления нужно перезапустить MCP-сервер или выполнить повторное подключение в клиенте.

## Удаление

Нужно убрать сервер Beget из конфигурации MCP-клиента, затем удалить `~/.local/bin/beget-api-mcp-server`.

## Про безопасность

Ключ API отправляется в Beget только внутри HTTPS POST-запроса. Сервер не помещает его в URL, логи, аргументы MCP-инструментов или результаты. Способ передачи ключа процессу выбирает MCP-клиент или используемый менеджер секретов.
