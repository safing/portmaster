# M1: API и бинарник

Цель: Rust-ядро поднимает HTTP/WS API на `127.0.0.1:817`, UI открывается и сохраняет настройки.

## Сделано
- `pm-api`: реестр эндпоинтов (`Endpoint`, `Parameter`, проверки как в Go), паттерны `{name}`, экспорт `GET /api/v1/endpoints`.
- Права: `Permission`, `AuthToken`, API-ключи из `core/apiKeys` (`<key>?read=user&write=admin`), hook `Authenticator`, dev-mode.
- Роутер: чистка путей и 301, security headers, CSP, CORS-проверки и preflight, 401/403/405/404 с текстами Go.
- Эндпоинты: `endpoints`, `auth/permissions`, `auth/bearer`, `auth/basic`, `auth/reset`, `config/options` (из `pm-config`).
- Сериализация: `text/plain; charset=utf-8`, Go-JSON с HTML-эскейпом, CBOR/MsgPack по `Accept`, записи с `_meta`.
- Сервер на axum (`pm_api::serve`), смоук по TCP в тестах.

- WS DB API `/api/database/v1`: кадры `id|type|key|data`, `J`+JSON+`_meta`, `get/query/sub/qsub/cancel/create/update/insert/delete`,
  подписки с `upd/new/del`, права admin до апгрейда, сквозной тест по реальному WebSocket.
  Отклонение: удалённые записи отдаются как `del` (в Go они превращаются в `warning` из-за ошибки маршалинга).

- Сессии-куки `Portmaster-API-Token` (TTL 5 мин, продление при использовании, `auth/reset` удаляет сессию).
- Бинарник `pm-core` (`portmaster-core-rs`): флаги как у Go, каталоги, БД `core`/`cache`, опции ядра,
  загрузка `config.json` (конфиг сильнее флага), API, обслуживание БД по таймеру, SIGTERM → выход 0, код 23 для рестарта.
- Эндпоинты `ping` и `ready`.

- Раздача UI из zip (`service/ui`): `/assets/*`, `/ui/modules/<module>/*`, редирект `/` → default module,
  fallback на `index.html` для клиентских роутов, кэш архивов в памяти. Проверено на реальном UI-архиве.

## Осталось
1. Авторизация по сокету (`SO_PEERCRED`): нужен доступ к fd соединения; без `unsafe` — только через безопасную обёртку,
   либо оставить API-ключи/сессии как основной путь (решение за пользователем).
3. Эндпоинты `status`/`debug/*` по мере появления модулей (M2).
4. `--recover-iptables` (и остальные recover-команды) всё ещё отсутствуют в Rust CLI; появятся вместе с firewall (M5).

## Проверка
- `cargo test -p pm-api`, `cargo clippy --workspace --all-targets -- -D warnings`.
- Ручной смоук: `curl -s http://127.0.0.1:817/api/v1/endpoints`.
