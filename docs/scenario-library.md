# Scenario Library

English | [Korean](scenario-library.kr.md)

The Control Room stores reusable, validated YAML scenarios on the Controller. Library records are independent of experiment results: saving a scenario does not run it, running edited YAML does not save it automatically, and deleting either record does not delete the other.

## Use the library in the Control Room

1. Open **Run experiment**. The **Saved scenarios** list is loaded from the current Controller.
2. Enter a descriptive **Saved scenario name** and edit the YAML.
3. Choose **Save scenario**. The saved item becomes the selected record.
4. Choose **Load** on a list item to replace the name and YAML in the editor.
5. Edit either the name or YAML and choose **Save changes** to update the selected record. Choose **Save as new** to keep the original and create another record.
6. Choose **Delete**, then **Confirm delete**, to remove a record. **Refresh** reloads the list from the Controller.

**New** resets the editor to the built-in example and clears the selected record. Loading another item or starting a new one replaces unsaved editor changes without a recovery copy. The **Run** button always executes the YAML currently in the editor and can run it 1–100 times whether it has been saved or not.

Names are trimmed, required, limited to 128 Unicode characters, and cannot contain control characters. YAML is required, limited to 1 MiB, and must pass normal scenario validation before it is stored. Names do not have to be unique; each record receives its own generated ID. The list is ordered by most recent update.

## REST API

All request and response bodies use JSON. List responses omit YAML so opening a large library remains inexpensive; fetch an individual record before editing it.

| Method | Path | Body | Success |
|---|---|---|---|
| `GET` | `/api/v1/scenarios` | — | Summary list: `id`, `name`, `createdAt`, `updatedAt` |
| `POST` | `/api/v1/scenarios` | `{ "name": "…", "yaml": "…" }` | `201` with the complete record |
| `GET` | `/api/v1/scenarios/{id}` | — | Complete record including `yaml` |
| `PUT` | `/api/v1/scenarios/{id}` | `{ "name": "…", "yaml": "…" }` | `200` with the updated record |
| `DELETE` | `/api/v1/scenarios/{id}` | — | `204` with no body |

When `KPL_API_TOKEN` is configured, `POST`, `PUT`, and `DELETE` require `Authorization: Bearer <token>`. Reads remain public under the Controller's current authentication policy. Invalid input returns `400`, an oversized request returns `413`, and a missing or invalid ID returns `404`.

```sh
curl --fail http://localhost:8080/api/v1/scenarios

curl --fail -X POST http://localhost:8080/api/v1/scenarios \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${KPL_API_TOKEN:-}" \
  --data-binary @- <<'JSON'
{"name":"Smoke baseline","yaml":"version: 2\nname: smoke-baseline\nphases:\n  - action: stop-all\n"}
JSON
```

## Storage and backup

Records are stored as individual JSON files under `<data-dir>/scenarios`. Compose and Swarm mount the Controller's persistent data volume at `/var/lib/kpl/data`, so ordinary service restarts and `scripts/swarm.sh remove` preserve the library. Back up the entire Controller data directory to keep both the scenario library and experiment results. The library is local to that Controller and is not replicated between control nodes.

The Controller writes each record through a temporary file and atomic filesystem operation. It rejects malformed or unexpected record content when listing or loading the library rather than returning a partially trusted scenario.
