# WS

WS is a small tenant-scoped WebSocket chat library for Go. It provides direct
messages, typing and presence events, durable offline delivery, idempotent
client retries, and periodic session validation.

## Security model

Every connection has a server-derived `ClientIdentity{TenantID, UserID}`. The
embedding application must populate `ClientIdentityFromRequest` from trusted
authentication state; query parameters and client frames are not identities.
Tenant ID never appears in a client frame, and sender, time, and delivery state
are always authored by the server.

```go
cfg := chat.DefaultHubConfig()
cfg.ClientIdentityFromRequest = func(r *http.Request) (chat.ClientIdentity, error) {
	identity, ok := trustedIdentityFromContext(r.Context())
	if !ok {
		return chat.ClientIdentity{}, chat.ErrUnauthorized
	}
	return chat.ClientIdentity{
		TenantID: identity.TenantID,
		UserID:   identity.UserID,
	}, nil
}

hub := chat.NewHubWithConfig(db, cfg)
go hub.Run()
defer hub.Stop()

http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
	chat.ServeWs(hub, w, r)
})
```

`ServeWs` fails closed when no identity resolver is configured. Applications
may also set `ValidateClientSession`; it runs before upgrade and periodically
for the life of each socket.

## Wire protocol

The client creates and durably stores a canonical lowercase UUID before its
first send:

```json
{"type":"message","id":"5213f8b6-9c56-4ca4-88bb-e114405194a9","to_user_id":42,"text":"hello"}
```

After persistence the sender receives:

```json
{"ack":{"kind":"persisted","message_ids":["5213f8b6-9c56-4ca4-88bb-e114405194a9"]}}
```

The recipient receives a server-authored message:

```json
{"messages":[{"type":"message","id":"5213f8b6-9c56-4ca4-88bb-e114405194a9","from_user_id":41,"to_user_id":42,"text":"hello","date":1784450000}]}
```

Only after writing that message to durable client storage should the recipient
acknowledge it:

```json
{"type":"ack","message_ids":["5213f8b6-9c56-4ca4-88bb-e114405194a9"]}
```

The server then returns a `delivered` acknowledgement. Until this application
ack arrives, the message remains in the offline backlog. Clients must UPSERT by
message ID because a disconnect can cause a safe re-delivery.

Typing is transient and scoped to the sender's authenticated tenant:

```json
{"type":"typing","to_user_id":42,"is_typing":true}
```

Unknown fields are rejected. In particular, clients cannot submit `tenant_id`,
`from_user_id`, `date`, or `is_delivered`. An exact retry of a persisted message
ID is acknowledged without creating another row; changing its recipient or
text returns `message_conflict`.

## Persistence

The embedding database must provide the equivalent of the additive SQLite
migration in `cli/migrations/000005_create_tenant_scoped_chat.up.sqlite3`:

- `chats_v2`, primary key `(tenant_id, id)` and unread index
  `(tenant_id, to_user_id, is_delivered, date, id)`;
- `contacts_v2`, primary key
  `(tenant_id, owner_user_id, contact_user_id)` and the reverse presence index.

Legacy mobile-only rows must not be inferred into a tenant. Leave them
quarantined or migrate them only from an authoritative tenant/user mapping.

Identity lookup is intentionally outside this package. Resolve address-book
entries in the owning identity service, then call `AddContacts` with stable user
IDs. `SubmitContacts` is only an adapter for already-resolved `user_id` values;
it never queries or mirrors an identity `users` table.

User IDs are positive 64-bit integers and map to `BIGINT` in PostgreSQL. A
socket upgrade is rejected with HTTP 503 when persistence is unavailable; if a
backlog query fails after upgrade, the socket closes with code 1011. Messages
are never routed through an ephemeral fallback. Presence is deliberately
best-effort: a contact-query failure suppresses only the status event and never
changes message persistence or delivery.

## Migrating from v0.1

- Replace `ClientIDFromRequest` with `ClientIdentityFromRequest`.
- Replace mobile/string client IDs with `ClientIdentity`; `UserID` and all
  message/contact user IDs are positive `int64` values.
- Send `to_user_id`, not `to`, and provide a canonical UUID `id`.
- Read messages from the `Response.messages` envelope using `from_user_id` and
  `to_user_id`.
- Persist received messages before sending an ACK.
- Replace mobile-keyed `chats`/`contacts` queries with the tenant-scoped v2
  schema. Do not backfill ambiguous legacy rows.
