# Rust-ядро Portmaster — спецификация

## Цель
Переписать ядро (`service/`, `base/`, `cmds/portmaster-core`) с Go на Rust с нуля, сохранив совместимость:
UI (Angular/Tauri), данные на диске и поведение файрвола. Go- и Rust-ядро взаимозаменяемы: остановил одно — запустил другое.

## Рамки
- Платформа: сначала Linux. Windows — позже (драйвер `windows_kext` уже на Rust).
- Портированы: обновления Safing (`pm-updates`/`pm-updatemgr`, включая jess-подпись), broadcasts, sync, compat (Linux). SPN уже удалён.
- Не переносим (честный остаток по ревью): eBPF/aya и eBPF C-программы, Windows kernel-драйвер (C + подпись), bbolt и другие альтернативные бэкенды БД (только sqlite), `base/template`, `base/info`, `base/container`, `base/database/{iterator,dbmodule}`, CLI-утилиты `cmds/cmdbase|integrationtest|testsuite|trafficgen`, агрегатор `service/detection`.
- Вместо SPN — интеграция с Xray. Рекомендуемый путь: внешний Rust-бинарник `xray-rs` (отдельный workspace,
  `forbid(unsafe_code)`, MPL-2.0, MSRV 1.96) запускается как дочерний процесс, конфиг генерируется из подписок,
  статистика берётся через его gRPC. Линковка через `xray-ffi` потребовала бы `unsafe` в `pm-core`.
- `unsafe` запрещён на уровне workspace (`[workspace.lints.rust] unsafe_code = "forbid"`). Исключение — только по отдельному решению, с обоснованием (скорость/функционал), в изолированном модуле.
- Одновременно оба ядра не работают (общий порт 817/53, блокировки sqlite).

## Контракты совместимости (нельзя ломать)
| Что | Где в Go | Суть |
|---|---|---|
| Каталог данных | `service/config.go:54` | `/var/lib/portmaster`, флаги `--data-dir/--bin-dir/--log-dir` |
| dsd | `structures@v1.2.0/dsd` | байт формата: RAW=1 CBOR=67 GenCode=71 JSON=74 MsgPack=77 YAML=89 GZIP=90 LIST=76; varint8 |
| Запись БД | `base/database/record` | ключ `<db>:<key>`; Meta GenCode 34 байта (4×i64 LE + 2 bool); wrapper `0x01 + block(meta) + format + payload` |
| sqlite-хранилище | `base/database/storage/sqlite` | `<loc>/db.sqlite`, таблица `records(key,format,value,created,modified,expires,deleted,secret,crownjewel)`, WAL; таблица `gorp_migrations` не трогать |
| bbolt | `storage/bbolt` | только чтение для миграции в sqlite |
| Язык запросов | `base/database/query/parser.go` | `query <prefix> where ... orderby ... limit ... offset ...` |
| JSON | Go `encoding/json` | порядок полей = порядок полей структуры, map — по ключам, HTML-эскейп `<>&` → `<>&`, время RFC3339Nano, nil-срез = `null` |
| config.json | `base/config/persistence.go` | иерархический JSON, отступ 2 пробела, 0600, только заданные пользователем значения |
| HTTP API | `base/api` | `127.0.0.1:817`, `/api/v1/<path>`, `GET /api/v1/endpoints`, авторизация процесса по сокету |
| WS DB API | `base/api/database.go` | `/api/database/v1`, бинарные кадры `id|type|...` (SplitN), данные `J`+JSON+`_meta` |
| Netquery | `service/netquery` | in-memory sqlite + `databases/history.db`, JSON-запросы `$eq/$count/...` |
| iptables | `firewall/interception/nfqueue_linux.go` | цепочки `PORTMASTER-*`, очереди 17040/17140/17060/17160, метки 1700–1799 |
| Вердикты | `network/status.go` | числа 0..8, порядок используется в логике |
| DNS | `nameserver` | 127.0.0.17:53 и [::1]:53 UDP |
| Exit code | `service/instance.go` | 23 = перезапуск |

## Архитектура
Cargo workspace `crates/`, один бинарник `portmaster-core-rs` (крейт `pm-core`). Async — tokio. Логи — tracing.

| Крейт | Отвечает за | Аналог Go |
|---|---|---|
| `pm-dsd` | форматы dsd, varint, gzip, dsdl-списки, Go-совместимый JSON | structures/dsd, varint |
| `pm-record` | Key, Meta, Record (wrapper), `_meta`-JSON, accessor (get/set по пути) | base/database/record, accessor |
| `pm-query` | парсер и вычисление условий запросов | base/database/query |
| `pm-database` | реестр БД, controller, хранилища (sqlite, injected), подписки | base/database |
| `pm-config` | опции, валидация, config.json, перспективы профилей | base/config |
| `pm-mgr` | модули, воркеры, события, порядок старта/стопа | service/mgr |
| `pm-api` | HTTP (axum), реестр эндпоинтов, авторизация, WS DB API, раздача UI из zip | base/api, service/ui |
| `pm-netstate` | сокет→PID (`/proc/net/*` + `/proc/<pid>/fd`) | network/state, proc |
| `pm-process`, `pm-profile` | процессы, отпечатки, профили, endpoint-правила | service/process, profile |
| `pm-intel` | фильтр-списки, GeoIP, сущности, загрузка с проверкой подписи jess | service/intel, updates (часть) |
| `pm-resolver`, `pm-nameserver` | резолвер (hickory), DNS-сервер | service/resolver, nameserver |
| `pm-firewall` | nfqueue, iptables, conntrack, решения | service/firewall |
| `pm-network` | соединения, БД `network:` | service/network |
| `pm-netquery` | история, графики | service/netquery |
| `pm-netenv` | онлайн-статус, интерфейсы, шлюзы | service/netenv |
| `pm-proxy` (есть) | split-tunnel прокси | service/splittun |

`pm-dns` и `pm-filter` удаляются (несовместимы с форматами Go).

## Проверка совместимости
- Go-генератор эталонов `tools/rust-fixtures/` (Go, использует пакеты `base/...`) пишет файлы в `crates/fixtures/`.
- Rust-тесты сверяют вывод с эталонами байт в байт; и в обратную сторону — Go-тест читает файлы, созданные Rust.
- Клиент `desktop/tauri/src-tauri/src/portapi` — образец WS-протокола.
- Смоук на живой системе — только с копией каталога данных и с явным согласованием.

## Этапы
| # | Содержание | Критерий готовности |
|---|---|---|
| M0 | pm-dsd, pm-record, pm-query, pm-database (sqlite+injected), pm-config, pm-mgr | тесты на эталонах; чтение реальной копии `core` БД и `config.json` |
| M1 | pm-api + бинарник: HTTP, WS, авторизация, раздача UI | UI открывается на Rust-ядре, настройки сохраняются |
| M2 | netstate (готов), process (готов), profile (готов без слоёв/endpoints), notifications (готов), runtime (готов), status (готов; netenv — только состояние) | видны приложения/профили |
| M3 | intel: dsdl + bloom + filterlists + GeoIP + Entity + индекс обновлений, загрузчик и jess (готовы) | списки обновляются |
| M4 | resolver, nameserver | DNS через Rust |
| M5 | firewall: nfqueue, iptables, conntrack, deciders; network | Rust-ядро фильтрует трафик |
| M6 | netquery (готов); eBPF (aya) — не портировано, зависимости в workspace нет | история и графики |
| M7 | splittun (pm-proxy), netenv | split-tunnel |
| M8 | systemd/упаковка, переключение | Rust по умолчанию |

## Процесс работы
- Opus: ТЗ и интерфейсы на крейт, ревью, интеграция. Sonnet / agy (Gemini): реализация по ТЗ в отдельном git worktree. Промпты для Gemini — в `docs/rust-rewrite/prompts/` (англ.).
- Каждый шаг: тесты зелёные → `cargo clippy -D warnings` → ревью → локальный коммит. Пуш — только по команде.
- Сборки — `nice`, половина ядер.
