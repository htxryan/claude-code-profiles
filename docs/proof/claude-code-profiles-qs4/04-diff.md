# 04 — diff: see how prod overrides dev

## `diff dev prod`
```
a=dev b=prod: 2 change(s) (0 added, 0 removed, 2 changed)
  ~ CLAUDE.md
  ~ settings.json
[exit 0]
```

## `diff dev prod --json`
```
{
    "a": "dev",
    "b": "prod",
    "entries": [
        {
            "relPath": "CLAUDE.md",
            "status": "changed"
        },
        {
            "relPath": "settings.json",
            "status": "changed"
        }
    ],
    "totals": {
        "added": 0,
        "removed": 0,
        "changed": 2
    }
}
```
