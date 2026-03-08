# Desktop Mode

Desktop mode lets `slacrawl` ingest local Slack Desktop state into SQLite without depending on Slack search.

This path is read-only. It snapshots Slack Desktop artifacts, parses supported local state, and upserts what it can recover into the database.

## What Desktop Mode Ingests

Today the desktop adapter can ingest:

- workspace metadata from local desktop state
- cached public and private channel metadata
- cached user/member profiles
- cached channel message history recovered from IndexedDB redux persistence blobs
- cached thread roots and cached thread replies recovered from IndexedDB redux persistence blobs when Slack Desktop has them
- draft messages
- read markers from local persisted API calls
- custom status metadata
- object store inventory for IndexedDB drift tracking

The desktop adapter intentionally does not use local desktop auth material for write actions.

## What It Does Not Yet Cover

Desktop mode is still partial in a few areas:

- DM and MPIM ingestion is intentionally excluded from V1
- attachment blobs are not downloaded
- background file/media caches are not indexed as searchable attachments

## Path Detection

On macOS, leave the desktop path blank to auto-detect:

```toml
[slack.desktop]
enabled = true
path = ""
```

The supported default target is:

```text
~/Library/Containers/com.tinyspeck.slackmacgap/Data/Library/Application Support/Slack
```

If your Slack Desktop data lives elsewhere, set the path explicitly.

## One-Shot Desktop Sync

Run a one-shot desktop import:

```bash
slacrawl sync --source desktop
```

This will:

1. snapshot the local Slack Desktop storage
2. parse Local Storage and IndexedDB data
3. merge supported rows into SQLite

## Continuous Desktop Refresh

Use `watch` to keep refreshing the DB from local desktop state:

```bash
slacrawl watch --desktop-every 5m
```

This loop does not truncate the database. It repeatedly upserts and appends event history so the local DB stays current as Slack Desktop changes.

## Validation Commands

After a desktop sync, the most useful checks are:

```bash
slacrawl doctor
slacrawl status
slacrawl channels
slacrawl users
slacrawl messages --limit 20
slacrawl sql "select entity_id, value from sync_state where source_name = 'desktop' order by entity_type, entity_id"
```

## Operational Notes

- Rich IndexedDB redux blob decoding uses `node` when available.
- If `node` is unavailable, desktop sync still runs, but decoded cached channel/user/message coverage will be reduced.
- Desktop data is merged at lower precedence than API data.
- Re-running desktop sync is safe; canonical rows are upserted by Slack-native keys.
