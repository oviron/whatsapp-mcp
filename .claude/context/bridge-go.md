# Contexto: Bridge Go

Bridge Go (`whatsapp-bridge/main.go` e arquivos de cada feature) — conecta ao WhatsApp via whatsmeow, expõe REST API local, persiste em SQLite (`store/messages.db` por padrão; `WHATSAPP_STORE_DIR` muda a raiz).

## Pontos críticos

- **whatsmeow exige `context.Context`** nas chamadas de API de rede (quebra que congelou o upstream). Versão pinada: `v0.0.0-20261017175727-b2b1e71d45e2`. Go **1.26+**, toolchain **1.27.3** (`go.mod`).
- **REST API liga em `127.0.0.1` por padrão.** Upstream ligava `0.0.0.0` (qualquer um na LAN mandava mensagem como você). `BIND_ADDR=<ip>` (ex. `0.0.0.0`, ou um IP específico como um endereço Tailscale) reabre.
- **Porta** via `WHATSAPP_BRIDGE_PORT` (default 8080).
- **`API_AUTH_TOKEN`**: se `BIND_ADDR` não é `127.0.0.1`/`localhost`, o processo **recusa subir**
  sem essa env var setada (fail-closed, não fail-open). Quando setada, todo `/api/*` exige
  header `Authorization: Bearer <token>` (middleware `apiGuard` em `rest_guard.go`, aplicado só em
  `/api/*` — `/qr` e `/qr.png` ficam sempre abertos, é o próprio fluxo de pareamento).

## Endpoints REST de leitura (`/api/chats`, `/api/messages`, etc.)

- 10 endpoints somam-se aos de escrita já existentes: `/api/chats`, `/api/chats/active`, `/api/messages`,
  `/api/message_context`, `/api/contacts/search`, `/api/contacts/chats`,
  `/api/contacts/last_interaction`, `/api/chat`, `/api/chat/by_contact`, `/api/sender_name`.
  Contrato: os handlers em `whatsapp-bridge/main.go`.
- Motivo de existirem: o MCP Python lia esses dados via SQLite local direto; isso só funciona
  quando MCP e bridge rodam na mesma máquina. Bridge remota (VPS) exige que essas leituras
  também sejam REST — as tools não dependem de SQLite local (ver limites de mídia/scripts em `mcp-python.md`).
- `whatsapp.db` (store do whatsmeow) não expõe `*sql.DB` cru via `sqlstore.Container` — esses
  handlers abrem uma **segunda conexão SQLite read-only** própria só pra leituras de
  `whatsmeow_contacts`/`whatsmeow_lid_map`, separada da conexão de escrita gerenciada pela lib.
- Busca accent-insensitive (`unaccent`) migrou de Python pra um driver SQLite derivado
  (`sqlite3_unaccent`, registrado via `sql.Register`), usado só na conexão de leitura — não
  afeta a conexão de escrita principal.
- Timestamps de resposta usam RFC3339 e preservam o offset do `time.Time` lido do banco (`nullTimeToPtr`/`time.Time.MarshalJSON`); UTC sai como `Z`. Não há offset fixo `-03:00`.

## Captura de conteúdo

- `extractTextContent` retorna captions de image/video/document (`GetCaption()`). Bug original: caption era dropado na escrita → busca não achava texto que existia. Caminho live e caminho de history-sync compartilham a função (antes o sync tinha extractor inline duplicado com o mesmo bug).
- Mensagens enviadas são persistidas localmente na hora (echo multi-device não dispara em conta single-device).

## History sync

- `store.DeviceProps.RequireFullSync = proto.Bool(true)` + `HistorySyncConfig{FullSyncDaysLimit:365,...}` **antes de `NewClient`**. Pede até um ano ao parear; a entrega depende do WhatsApp. `/api/chat/backfill` pede ao celular online mensagens anteriores à mais antiga armazenada de um chat, em lotes de 1–50.
- `StoreMessage` usa `ON CONFLICT` com `COALESCE(NULLIF(...))`: conteúdo vazio do sync não apaga transcrições. Preserva revogações e reaplica edições registradas na mesma transação; edições não substituem transcrições de áudio. Faça backup de `messages.db` e `whatsapp.db` antes de re-parear.

## LID→PN

- `resolveToPN` (`main.go`) normaliza LID→PN na escrita quando o mapeamento é conhecido. Sem ele, `senderKey` (`lid_merge.go`) mantém o JID `@lid` inteiro para não confundir LID com telefone.
- `migrateLIDChats` (`lid_merge.go`) roda no startup e mescla chats `@lid` nos `@s.whatsapp.net` quando há mapeamento (transação por chat, idempotente), incluindo estado associado e autores. Movimentos de mídia ficam registrados para retomar após falha.

## Mídia + segurança

- **`safeMediaPath`** (`main.go`): rejeita componentes crus com `/`, `\`, `..`, `.`, vazio (não sanitiza — surface o ataque).
- Path de mídia prefixado com message ID: filename derivado do sync-time colidia (571 áudios → 87 paths únicos), cache devolvia bytes errados.
- **Media retry** (recuperação de mídia expirada): `SendMediaRetryReceipt` → `events.MediaRetry` → `DecryptMediaRetryNotification` → `DownloadToFile`. Downloads usam arquivo temporário, validação e rename (`media_cache.go`). History sync NÃO recupera (re-entrega as mesmas URLs mortas do CDN). Endpoint `/api/mediaretry`. Log contract estável: `MEDIA RETRY <id>: SUCCESS|NOTONPHONE|ERROR`.

## Testes

- Testes legados em `main_test.go` (inclui `TestSafeMediaPath`); cada feature nova tem seu `<feature>_test.go`. CI exige `gofmt`, build/vet/test com `-tags sqlite_fts5` e também `go test ./...` sem a tag.

## Open matters, favorites, summary, drafts (stage 4)

Additive tables created in `NewMessageStore` (each feature owns its schema string): `matters` + `matter_events` + `review_cursor` (matters.go), `favorites` (favorites.go), `summaries` (summary.go), `drafts` (drafts.go), `message_mentions` (mentions.go). Each file has its `_test.go` and registers its routes from main.go with one line (`registerMatterRoutes`, `registerFavoriteRoutes`, `registerSummaryRoute`, `registerDraftRoutes`); handlers use the generic `matterRoute`.

- Matter state is **derived** (`matterState`): open, closed_confirmed, closed_owner, expired (one month after it arose or was reopened). No background job. `needsAttention` holds the "when does it ask for attention" table; the day is read in the zone of `as_of` (the bridge never decides a timezone).
- Windows compare **instants** with `julianday(...)`, never timestamp strings: `messages.timestamp` keeps whatever offset it was written with.
- Rules the code enforces: the owner's own messages never close a matter; groups only with the owner's @mention (`checkTrackedChat`, `mentionsOwner`, owner = phone and LID); status/newsletters/own chat never tracked; closing needs a message newer than the matter.
- `approveDraft` is the **only** path that sends a chase: it claims the draft (`written` -> `sending`) before `sendWhatsAppMessage` and sends exactly the text it receives. The "only after the owner decided" guard is prompt-level (docstring + `/whats:morning`); the server cannot prove a human saw the text. A failed send puts the draft back to pending.
- The summary is a stored photograph per day (`summaries`); announcing a closure clears `announce_pending`.
- WhatsApp Favorites are mirrored from `events.AppState` (index `favorites`); `EmitAppStateEventsOnFullSync` is off, so a list that existed before pairing is picked up on its next change.
- Checagens locais: `go build -tags sqlite_fts5 . && go vet -tags sqlite_fts5 ./... && go test -tags sqlite_fts5 ./... && go test ./...`. CI exige `gofmt` em todos os arquivos Go.

## Deleting a chat (e2e cleanup)

`POST /api/chat/delete` (`chat_delete.go`) deletes a chat from WhatsApp through the `deleteChat` app-state patch (with the stored last message as range). It refuses unless the body carries `expect_name_prefix` and the chat's stored name starts with it, so a mistyped JID cannot delete a real conversation; `forget_local` also drops the local rows. REST only, no MCP tool on purpose (an AI client should not be able to delete chats). Used by the live gate to remove its `e2e-...` groups.

## App-state drift ("mismatching LTHash")

If the log shows `failed to verify patch vNN: mismatching LTHash` and every archive / mark-read / delete-chat answers 500 "server returned error updating app state", the local copy of that app-state collection drifted (the e2e gate skips app-state calls for an account in this state). Repair: `POST /api/appstate/resync {"name":"regular_low"}` (`appstate_resync.go`), a whatsmeow full sync of that collection (cached state dropped, refetched); when the server's own patches no longer verify even that fails, and the repair is `{"name":"regular_low","mode":"recovery"}`, which asks the phone (primary device, must be online) for a fresh copy of the collection through `BuildAppStateRecoveryRequest`; whatsmeow processes the answer by itself. Names: regular, regular_low, regular_high, critical_block, critical_unblock_low. Archive uses regular_low.

