# 09 — negative paths: missing profile, missing parent, cycle, bad argv

## use a non-existent profile
```
claude-profiles: Profile "ghost" does not exist
[exit 3]
```

## extends a non-existent profile
```
FAIL  missing-parent: [MissingProfile] Profile "does-not-exist" does not exist (referenced by "missing-parent")
validate: 0 pass, 1 fail
claude-profiles: validation failed for 1 profile(s)
[exit 3]
```

## cycle: cycle-a -> cycle-b -> cycle-a
```
FAIL  cycle-a: [Cycle] Cycle in extends chain: cycle-a → cycle-b → cycle-a
validate: 0 pass, 1 fail
claude-profiles: validation failed for 1 profile(s)
[exit 3]
```

## bad argv (unknown verb)
```
claude-profiles: unknown command "yolo"; run "claude-profiles --help" for usage
[exit 1]
```

## bad flag --no-color (removed dead flag is rejected)
```
claude-profiles: unknown flag "--no-color" (colour output is not yet implemented)
[exit 1]
```

## new with invalid name (path traversal)
```
claude-profiles: new: invalid profile name "../escape" — must be a bare directory name without /, \, leading . or _
[exit 1]
```
