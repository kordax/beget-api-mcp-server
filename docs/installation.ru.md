# Установка в систему

Эта инструкция устанавливает сервер один раз для текущего пользователя Linux и подключает его ко всем проектам Codex. Права root не нужны.

## Что понадобится

Сначала нужны:

- Go 1.26.5
- Git
- GitHub CLI
- `codex-keyring`

Проверить окружение можно так:

```bash
go version
git --version
gh --version
codex-keyring check beget-api-key
```

Последняя команда только проверяет наличие alias. Пароль API она не показывает.

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

Добавлять каталог в `PATH` необязательно. Ниже в конфигурации Codex используется абсолютный путь. Если хочется запускать бинарник просто по имени, можно добавить `~/.local/bin` в пользовательский `PATH`.

## Глобальное подключение к Codex

В файл `~/.codex/config.toml` нужно добавить:

```toml
[mcp_servers.beget]
command = "codex-keyring"
args = ["run", "beget-api-key", "--", "/home/your-user/.local/bin/beget-api-mcp-server"]
env = { BEGET_API_LOGIN = "your-beget-login" }
```

Нужно заменить `/home/your-user` и `your-beget-login` своими значениями. Пароль API в этот файл записывать нельзя. Разделитель в `args` обязателен для `codex-keyring`.

После изменения конфигурации Codex нужно перезапустить. Инструменты Beget станут доступны из любого проекта.

## Установка из локального клона

Этот вариант удобен во время разработки сервера:

```bash
git clone git@github.com:kordax/beget-api-mcp-server.git
cd beget-api-mcp-server
GOBIN="$HOME/.local/bin" go install ./cmd/beget-api-mcp-server
```

Глобальную конфигурацию Codex менять не придется, потому что путь установленного бинарника останется тем же.

## Обновление

Достаточно повторить команду установки:

```bash
GOBIN="$HOME/.local/bin" go install github.com/kordax/beget-api-mcp-server/cmd/beget-api-mcp-server@latest
```

После обновления нужно перезапустить Codex, чтобы он запустил новый бинарник.

## Удаление

Из `~/.codex/config.toml` нужно убрать секцию `mcp_servers.beget`, затем удалить `~/.local/bin/beget-api-mcp-server`. Запись в keyring можно оставить, если она используется где-нибудь еще.

## Про безопасность

Сервер читает пароль API только из `BEGET_API_KEY`. Глобальная конфигурация запускает его через `codex-keyring`, который передает ключ только дочернему процессу. Не стоит заменять этот способ обычной переменной `env` с паролем прямо в конфигурации.
