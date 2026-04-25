# 02 — new / list / status: scaffold profiles & inspect

## `new dev`
```
Created profile "dev" at /private/tmp/cp-proof-90308/.claude-profiles/dev
  edit /private/tmp/cp-proof-90308/.claude-profiles/dev/profile.json to set extends/includes
[exit 0]
```
## `new prod`
```
Created profile "prod" at /private/tmp/cp-proof-90308/.claude-profiles/prod
  edit /private/tmp/cp-proof-90308/.claude-profiles/prod/profile.json to set extends/includes
[exit 0]
```

## `list` (no active yet)
```
  dev   
  prod  
[exit 0]
```
## `list --json`
```
{
    "profiles": [
        {
            "name": "dev",
            "active": false,
            "description": null,
            "extends": null,
            "includes": [],
            "tags": [],
            "lastMaterialized": null
        },
        {
            "name": "prod",
            "active": false,
            "description": null,
            "extends": null,
            "includes": [],
            "tags": [],
            "lastMaterialized": null
        }
    ],
    "stateWarning": null
}
[exit 0]
```
## `status` (no profile materialized)
```
(no active profile — run `claude-profiles use <name>` to activate)
[exit 0]
```
## directory tree
```
/tmp/cp-proof-90308/.claude-profiles
/tmp/cp-proof-90308/.claude-profiles/dev
/tmp/cp-proof-90308/.claude-profiles/dev/.claude
/tmp/cp-proof-90308/.claude-profiles/dev/profile.json
/tmp/cp-proof-90308/.claude-profiles/prod
/tmp/cp-proof-90308/.claude-profiles/prod/.claude
/tmp/cp-proof-90308/.claude-profiles/prod/profile.json

--- dev/profile.json ---
{
  "name": "dev"
}
```
