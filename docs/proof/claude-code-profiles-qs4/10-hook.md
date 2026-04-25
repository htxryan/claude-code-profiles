# 10 — pre-commit hook install/uninstall

## hook install
```
Pre-commit hook already installed at /private/tmp/cp-proof-90308/.git/hooks/pre-commit
[exit 0]
```

## inspect installed hook
```
-rwxr-xr-x@ 1 redhale  wheel  115 Apr 25 18:41 /tmp/cp-proof-90308/.git/hooks/pre-commit

--- pre-commit content (first 25 lines) ---
#!/bin/sh
command -v claude-profiles >/dev/null 2>&1 || exit 0
claude-profiles drift --pre-commit-warn 2>&1
exit 0
```

## induce drift, simulate a commit attempt
Drift the live .claude/ then run the hook directly to see its warning.
```
[exit 0]
```

## sync to clear drift, hook is silent
```
Synced prod (drift discarded). Backup: /private/tmp/cp-proof-90308/.claude-profiles/.backup/2026-04-25T23-43-36.531Z
---
[exit 0]
```

## hook uninstall
```
Removed pre-commit hook at /private/tmp/cp-proof-90308/.git/hooks/pre-commit
[exit 0]
ls: /tmp/cp-proof-90308/.git/hooks/pre-commit: No such file or directory
```

## bonus: invoke `drift --pre-commit-warn` directly with a binary that exists

The hook runs `claude-profiles drift --pre-commit-warn` from PATH. To prove the warning
behaviour we re-install the hook then induce drift, but invoke the CLI explicitly:
```
Installed pre-commit hook at /private/tmp/cp-proof-90308/.git/hooks/pre-commit
claude-profiles: 1 drifted file(s) in .claude/ vs active profile 'prod'
  M settings.json
[exit 0]
```

## clean up — discard drift
```
Synced prod (drift discarded). Backup: /private/tmp/cp-proof-90308/.claude-profiles/.backup/2026-04-25T23-43-48.771Z
```
