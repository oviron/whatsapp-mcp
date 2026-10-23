# whatsmeow → MCP: análise de features ausentes

**Data:** 2026-10-23
**whatsmeow:** `v0.0.0-20261017175727-b2b1e71d45e2`
**Camadas:** whatsmeow (lib Go) → bridge REST (`main.go` e arquivos por feature) → tools MCP (`main.py`)

## Estado atual (o que já existe)

**49 tools MCP:** lista completa em [README.md](README.md#mcp-tools); registro em `whatsapp-mcp-server/main.py` via `@read_tool`/`@write_tool`.

**52 rotas `/api/*`:** leitura, envio, grupos, enquetes, matérias, favoritos, resumo, drafts, backfill e recuperação de app-state. Registradas em `main.go`, `matters.go`, `favorites.go`, `summary.go` e `drafts.go`. Fora da API: `/qr`, `/qr.png` e `/healthz`.

## Legenda de esforço

| Tier | Significado | Trabalho |
|------|-------------|----------|
| **A** | REST handler **já existe** na bridge; falta só wrapper Python | ~1 helper em `whatsapp.py` + `@read_tool`/`@write_tool` em `main.py`. **Zero Go.** |
| **B** | Método whatsmeow existe; sem REST handler | Handler Go (`main.go`) + helper + tool Python |
| **C** | Pesado/nicho; API existe mas custo alto ou pouco uso pessoal | Handler Go não-trivial + modelagem |

---

## Tabela de gaps (priorizada)

| # | Feature | whatsmeow | REST hoje | MCP hoje | Tier | Valor |
|---|---------|-----------|-----------|----------|------|-------|
| 1 | **Group info** (metadados, participantes) | `GetGroupInfo` | ✅ `/api/group_info` | ✅ `get_group_info` | **A** | Alto, feito |
| 2 | **Arquivar/desarquivar chat** | `SendAppState` | ✅ `/api/archive_chat` | ✅ `archive_chat` | **A** | Médio, feito |
| 3 | **Resolver contato** (telefone → JIDs; QR/link ainda ausente) | `LIDs.GetLIDForPN` (QR/link: `ResolveContactQRLink`) | ✅ `/api/resolve_contact` (telefone) | ✅ `resolve_contact` | **B** | Médio, telefone feito; QR/link pendente |
| 4 | **Reagir a mensagem** (emoji, inclusive de outro autor em grupo) | `BuildReaction`+`SendMessage` | ✅ `/api/react` | ✅ `react_to_message` | **B** | Alto, feito; autor do grupo precisa estar no histórico |
| 5 | **Editar mensagem** | `BuildEdit`+`SendMessage` | ✅ `/api/edit` | ✅ `edit_message` | **B** | Alto, feito |
| 6 | **Apagar p/ todos** (inclusive de outro autor em grupo) | `BuildRevoke`+`SendMessage` | ✅ `/api/revoke` | ✅ `delete_message` | **B** | Alto, feito; mensagem alheia exige autor armazenado e permissão de admin |
| 7 | **Gerenciar participantes** (add/remove/promote/demote) | `UpdateGroupParticipants` | ✅ | ✅ | **B** | Alto, feito |
| 8 | **Typing/presença no chat** | `SendChatPresence` | ✅ | ✅ | **B** | Alto (bot mais natural) |
| 9 | **Está no WhatsApp?** (valida número) | `IsOnWhatsApp` | ✅ | ✅ | **B** | Alto |
| 10 | **Info de usuário** (status, pic, devices) | `GetUserInfo`/`GetProfilePictureInfo` | ❌ | ❌ | **B** | Médio |
| 11 | **Enquete** (criar + ler votos) | `BuildPollCreation`/`DecryptPollVote` | ✅ `/api/poll`, `/api/poll_votes` | ✅ `create_poll`, `get_poll_votes` | **B** | Médio — feito |
| 12 | **Link de convite do grupo** (get/reset/join) | `GetGroupInviteLink`/`JoinGroupWithLink` | ✅ `/api/group_invite`, `/api/group_invite_reset`, `/api/group_join` | ✅ | **B** | Médio — done |
| 13 | **Setters de grupo** (nome/tópico/anúncio/locked; foto fora do escopo) | `SetGroupName`/`SetGroupTopic`/`SetGroupAnnounce`/`SetGroupLocked` | ✅ `/api/group_settings` | ✅ | **B** | Médio — done (`SetGroupPhoto` pending) |
| 14 | **Read receipt explícito e estado não lido** | `MarkRead`, `events.Receipt`, `events.MarkChatAsRead` | ✅ `/api/mark_chat_read`, `/api/mark_chat_unread`; contagens nas leituras | ✅ `mark_chat_as_read`, `mark_chat_as_unread`, `list_chats(unread_only=True)` | **B** | Médio, feito; badge manual separado de receipts |
| 15 | **Presença global** (online/offline) | `SendPresence`/`SubscribePresence` | ❌ | ❌ | **B** | Baixo |
| 16 | **Bloquear/desbloquear** | `UpdateBlocklist`/`GetBlocklist` | ❌ | ❌ | **B** | Baixo |
| 17 | **Business profile** | `GetBusinessProfile` | ❌ | ❌ | **B** | Baixo |
| 18 | **Mensagens efêmeras** (timer) | `SetDisappearingTimer` | ❌ | ❌ | **C** | Baixo |
| 19 | **Newsletters/Canais** (follow/read/react) | `FollowNewsletter`, `GetNewsletterMessages`, … | ❌ | ❌ | **C** | Baixo (nicho) |
| 20 | **Proxy / privacy settings** | `SetProxy`, `GetPrivacySettings` | ❌ | ❌ | **C** | Baixo |
| 21 | **Tipos de mensagem** (stickers, localização, contatos, reações) | `StickerMessage`, `LocationMessage`, `ContactMessage`, `ReactionMessage` | ✅ `/api/messages`, `/api/message_context`; download de stickers | ✅ `list_messages`, `get_message_context`, `download_media` | **B** | Feito, incluindo filtro de `status@broadcast` |
| 22 | **Edições/reações criptografadas e edit/revoke no histórico** | `DecryptSecretEncryptedMessage`, `DecryptReaction`, `ProtocolMessage` | ✅ ingestão live/histórico, `message_edits.go` | ✅ leituras refletem o estado armazenado | **B** | Feito, sem apagar transcrições por edição nem ressuscitar revogações |
| 23 | **Histórico anterior de um chat** | `BuildHistorySyncRequest`+`SendPeerMessage` | ✅ `/api/chat/backfill` (1–50 mensagens, chat não vazio, celular online) | ✅ `request_chat_history` | **B** | Feito, assíncrono; bloqueado nos modos somente leitura |
| 24 | **Pareamento por código** | `PairPhone` | ✅ CLI `-pair-phone` / `WHATSAPP_PAIR_PHONE` | Não se aplica | **B** | Feito, fallback para QR se falhar |
| 25 | **Desvincular sessão** | `Logout` | ✅ CLI `-logout` (parar bridge antes) | Não se aplica | **B** | Feito, desvincula no celular e remove a sessão local |
| 26 | **Download sem guardar mídia inteira em RAM** | `DownloadToFile` | ✅ `/api/download` e media retry, temporário validado + rename | ✅ `download_media` | **B** | Feito |
| 27 | **Busca FTS5** | SQLite local (não depende de whatsmeow) | ✅ `/api/messages`, build com `-tags sqlite_fts5`; fallback LIKE | ✅ `list_messages(query=...)` | **B** | Feito, backfill em background sem re-sync |

> Excluído como plumbing (não é "feature"): `Connect/Disconnect/ResetConnection`, uploads e builders internos, `AddEventHandler*`, `Set*HTTPClient`, `DangerousInternals`. Pareamento, logout, download e decriptação com efeito visível estão registrados acima.

---

## Quick wins recomendados

Os antigos quick wins (group info, archive, resolução por telefone, reações, revoke, edição, participantes, typing e validação de número) já estão implementados. Restam info de usuário (#10), resolução QR/link (#3), foto do grupo (#13) e as features sem endpoint marcadas na tabela. Não há lacuna de wrapper Python nos handlers listados como feitos.

### Adiar — Tier C
Newsletters, proxy, disappearing timer, privacy: API existe mas pouco uso pessoal. Só sob demanda.
