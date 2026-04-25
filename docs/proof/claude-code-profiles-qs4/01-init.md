# 01 — init: bootstrap .claude-profiles/ on a fresh project

## Initial dir
```
total 0
drwxr-xr-x@   3 redhale  wheel    96 Apr 25 18:40 .
drwxrwxrwt  229 root     wheel  7328 Apr 25 18:41 ..
drwxr-xr-x@   9 redhale  wheel   288 Apr 25 18:40 .git
```

## `claude-profiles init`
```
Initialised claude-profiles in /private/tmp/cp-proof-90308
  No .claude/ to seed; create profiles via "claude-profiles new <name>"
  Created .gitignore with 7 entries
  Installed pre-commit hook at /private/tmp/cp-proof-90308/.git/hooks/pre-commit
[exit 0]
```

## After init
```
total 8
drwxr-xr-x@   5 redhale  wheel   160 Apr 25 18:41 .
drwxrwxrwt  229 root     wheel  7328 Apr 25 18:41 ..
drwxr-xr-x@   3 redhale  wheel    96 Apr 25 18:41 .claude-profiles
drwxr-xr-x@   9 redhale  wheel   288 Apr 25 18:40 .git
-rw-r--r--@   1 redhale  wheel   189 Apr 25 18:41 .gitignore

--- .claude-profiles/ ---
total 0
drwxr-xr-x@ 3 redhale  wheel   96 Apr 25 18:41 .
drwxr-xr-x@ 5 redhale  wheel  160 Apr 25 18:41 ..
drwxr-xr-x@ 2 redhale  wheel   64 Apr 25 18:41 .tmp

--- .gitignore ---
# Added by claude-profiles
.claude/
.claude-profiles/.state.json
.claude-profiles/.lock
.claude-profiles/.pending/
.claude-profiles/.prior/
.claude-profiles/.backup/
.claude-profiles/.tmp/
```
