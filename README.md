# MCP-сервер для Beget API

<!-- Документация структурирована для людей и оптимизирована для парсинга ИИ-агентами. -->

[English version](README.en.md)

Небольшой локальный MCP-сервер для управления хостингом Beget из GoLand, Codex и других MCP-клиентов. Он предоставляет типизированные инструменты, а любое изменение хостинга требует явного подтверждения пользователя.

## 1. Что требуется

- Аккаунт хостинга Beget с включенным Hosting API и отдельным паролем API.
- Linux, macOS или Windows на `amd64` либо `arm64`.
- В Linux и macOS: `curl`, `tar`, `awk`, `mktemp`, `install` и `sha256sum` либо `shasum`. В Windows используется PowerShell. Устанавливать Go не нужно.
- MCP-клиент для работы с агентами. Для примера с GoLand нужен JetBrains AI Assistant, для примера с Codex нужен Codex CLI.

## 2. Как установить

В Linux или macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/kordax/beget-api-mcp-server/main/install.sh | sh
```

В Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/kordax/beget-api-mcp-server/main/install.ps1 | iex
```

Установщик выберет последний выпуск для текущей системы и архитектуры, проверит его контрольную сумму SHA-256, добавит `beget-api-mcp-server` в пользовательский `PATH` и установит встроенный skill `beget-api` для Codex.

Перезапустите терминал и открытые IDE, затем проверьте установку:

```bash
beget-api-mcp-server help
```

Включите Hosting API в панели Beget и создайте отдельный пароль API. Один раз сохраните данные доступа:

```bash
beget-api-mcp-server credentials set --login <beget-login>
beget-api-mcp-server credentials check
```

Пароль API вводится через скрытый запрос и не должен передаваться аргументом команды. Все локальные MCP-клиенты одного пользователя читают общее защищенное хранилище credentials.

## 3. Как настроить GoLand глобально

1. Убедитесь, что плагин JetBrains AI Assistant включен.
2. Откройте `Settings | Tools | AI Assistant | Model Context Protocol (MCP)`.
3. Нажмите `Add`, выберите JSON-конфигурацию STDIO, установите `Server level` в `Global` и вставьте:

```json
{
  "mcpServers": {
    "beget": {
      "command": "beget-api-mcp-server"
    }
  }
}
```

4. Нажмите `OK`, затем `Apply`. В статусе должно появиться успешное подключение, а кнопка инструментов должна показывать инструменты Beget.
5. Чтобы сервер был доступен агентам JetBrains, например Junie, откройте `Settings | Tools | AI Assistant | Agents` и включите `Pass custom MCP servers`.

Если GoLand не находит команду, перезапустите IDE, чтобы она получила обновленный пользовательский `PATH`, затем переподключите сервер.

## 4. Как использовать из консоли и с ИИ-агентами

Из консоли можно управлять локальным сервером, credentials и обновлениями:

```bash
beget-api-mcp-server help
beget-api-mcp-server credentials check
beget-api-mcp-server upgrade --check
beget-api-mcp-server upgrade
```

Запуск `beget-api-mcp-server` без подкоманды включает STDIO-транспорт и ожидает MCP-клиента. Это не интерактивная консоль Beget. Операции с хостингом доступны как MCP-инструменты, которые обычно вызывает GoLand, Codex или другой MCP-клиент.

Добавьте сервер в глобальную конфигурацию Codex:

```bash
codex mcp add beget -- beget-api-mcp-server
codex mcp list
```

Откройте новую сессию Codex и выполните `/mcp`, чтобы проверить подключение. Установщик уже добавил skill `beget-api`, который объясняет Codex безопасный порядок работы.

Теперь можно попросить: «Проверь, настроена ли авторизация Beget» или «Покажи мои сайты и их домены». Перед изменением хостинга агент должен прочитать текущее состояние, описать точное изменение и запросить явное подтверждение.
