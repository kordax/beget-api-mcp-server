# Транспорты MCP

Сервер поддерживает stdio, Streamable HTTP и старый HTTP-транспорт с SSE. В одном процессе можно включить только один транспорт.

## Флаги транспорта

- Транспорт не указан: использовать stdio.
- `--stdio`: явно использовать stdio.
- `--streamable-http`: использовать актуальный Streamable HTTP.
- `--sse`: использовать старый SSE-транспорт из спецификации MCP 2024 года.

Если передать несколько транспортных флагов, сервер завершит запуск с ошибкой.

## Stdio

Stdio является вариантом по умолчанию и проще всего подходит локальному MCP-клиенту. Клиент сам запускает процесс и обменивается сообщениями протокола через stdin и stdout.

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

Аргумент `--stdio` можно убрать, потому что этот транспорт и так выбран по умолчанию.

## Streamable HTTP

Streamable HTTP является основным HTTP-транспортом для актуальных MCP-клиентов. Сервер нужно запустить отдельно:

```bash
BEGET_API_LOGIN=your-beget-login \
BEGET_API_KEY=your-api-password \
beget-api-mcp-server \
  --streamable-http \
  --http-address 127.0.0.1:8080 \
  --http-path /mcp
```

После этого MCP-клиент подключается к endpoint:

```json
{
  "mcpServers": {
    "beget": {
      "url": "http://127.0.0.1:8080/mcp"
    }
  }
}
```

По умолчанию Streamable HTTP хранит MCP-сессии. Поведение меняют отдельные флаги:

- `--streamable-session-timeout` по умолчанию равен `30m` и закрывает неактивную сессию через заданное время. Значение `0` отключает timeout.
- `--streamable-json-response` возвращает `application/json` вместо потока SSE для запросов.
- `--streamable-stateless` создает временную сессию для каждого запроса.

Эти три флага работают только вместе с `--streamable-http`.

## Старый SSE

Отдельный SSE-транспорт нужен клиентам, которые все еще используют транспорт MCP 2024 года. Для новых подключений лучше выбирать Streamable HTTP.

```bash
BEGET_API_LOGIN=your-beget-login \
BEGET_API_KEY=your-api-password \
beget-api-mcp-server \
  --sse \
  --http-address 127.0.0.1:8080 \
  --http-path /sse
```

Клиент с поддержкой старого SSE подключается к адресу:

```json
{
  "mcpServers": {
    "beget": {
      "url": "http://127.0.0.1:8080/sse"
    }
  }
}
```

## Общие HTTP-флаги

- `--http-address` по умолчанию равен `127.0.0.1:8080` и задает TCP-адрес Streamable HTTP или SSE.
- `--http-path` по умолчанию равен `/mcp` или `/sse` и задает путь endpoint для выбранного HTTP-транспорта.

Адрес должен использовать `localhost`, `127.0.0.1` или `::1`. Сервер отклонит wildcard или внешний адрес до запуска HTTP.

## JetBrains и GoLand

GoLand поддерживает stdio, Streamable HTTP и старый SSE. Для stdio используется JSON-конфигурация команды из инструкции по установке. Для Streamable HTTP нужно выбрать HTTP-подключение и указать `http://127.0.0.1:8080/mcp`. Уровень сервера `Global` делает подключение общим для всех проектов.

## Граница безопасности

Инструменты MCP могут изменять DNS и состояние сайтов. Поэтому встроенный HTTP-сервер работает только на loopback. Защита от cross-origin запросов и DNS rebinding остается включенной.

Для доступа с другой машины сервер нужно оставить на loopback и поставить перед ним reverse proxy с авторизацией, VPN или SSH tunnel. Напрямую открывать endpoint в сеть не следует, пока в проекте не появится собственный слой HTTP-аутентификации.
