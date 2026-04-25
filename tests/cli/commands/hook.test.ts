import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  HOOK_SCRIPT,
  installHook,
  runHook,
  uninstallHook,
} from "../../../src/cli/commands/hook.js";
import { CliUserError } from "../../../src/cli/exit.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";
import { captureOutput } from "../helpers/output-sink.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

async function gitInit(projectRoot: string): Promise<void> {
  await fs.mkdir(path.join(projectRoot, ".git"), { recursive: true });
  await fs.mkdir(path.join(projectRoot, ".git", "hooks"), { recursive: true });
}

describe("hook script (R25a fitness function)", () => {
  it("HOOK_SCRIPT is byte-identical to the spec", () => {
    // The decomposition doc and §3.4 R25a both lock this content. Any change
    // requires a deliberate spec bump (the IV epic asserts byte equality).
    const expected =
      "#!/bin/sh\n" +
      "command -v claude-profiles >/dev/null 2>&1 || exit 0\n" +
      "claude-profiles drift --pre-commit-warn 2>&1\n" +
      "exit 0\n";
    expect(HOOK_SCRIPT).toBe(expected);
  });

  it("contains the command -v guard exactly once (fail-open invariant)", () => {
    const guardOccurrences = HOOK_SCRIPT.split("command -v claude-profiles").length - 1;
    expect(guardOccurrences).toBe(1);
  });

  it("never contains a non-zero exit on the success path", () => {
    expect(HOOK_SCRIPT.trimEnd().endsWith("exit 0")).toBe(true);
  });
});

describe("installHook", () => {
  it("writes the verbatim script with mode 0755 in a fresh repo", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    const r = await installHook({ cwd: fx.projectRoot, force: false });
    expect(r.installed).toBe(true);
    expect(r.preExisting).toBe("absent");
    expect(r.skippedReason).toBe(null);

    const written = await fs.readFile(r.hookPath, "utf8");
    expect(written).toBe(HOOK_SCRIPT);
    const stat = await fs.stat(r.hookPath);
    // Check executable bit (perm bits are platform-dependent on Windows; on
    // POSIX we assert the user-execute bit specifically).
    if (process.platform !== "win32") {
      expect(stat.mode & 0o100).toBe(0o100);
    }
  });

  it("is idempotent when our hook is already installed", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    await installHook({ cwd: fx.projectRoot, force: false });
    const r = await installHook({ cwd: fx.projectRoot, force: false });
    expect(r.preExisting).toBe("ours");
    expect(r.installed).toBe(false);
  });

  it("refuses to overwrite a different existing hook without --force", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    const hookPath = path.join(fx.projectRoot, ".git", "hooks", "pre-commit");
    await fs.writeFile(hookPath, "#!/bin/sh\necho user hook\n");
    const r = await installHook({ cwd: fx.projectRoot, force: false });
    expect(r.preExisting).toBe("other");
    expect(r.installed).toBe(false);
    // Original content preserved.
    expect(await fs.readFile(hookPath, "utf8")).toContain("user hook");
  });

  it("--force overwrites a different existing hook", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    const hookPath = path.join(fx.projectRoot, ".git", "hooks", "pre-commit");
    await fs.writeFile(hookPath, "#!/bin/sh\necho user hook\n");
    const r = await installHook({ cwd: fx.projectRoot, force: true });
    expect(r.preExisting).toBe("other");
    expect(r.installed).toBe(true);
    expect(await fs.readFile(hookPath, "utf8")).toBe(HOOK_SCRIPT);
  });

  it("creates .git/hooks/ if missing (fresh git init)", async () => {
    fx = await makeFixture({});
    // .git/ exists but hooks/ does not.
    await fs.mkdir(path.join(fx.projectRoot, ".git"), { recursive: true });
    const r = await installHook({ cwd: fx.projectRoot, force: false });
    expect(r.installed).toBe(true);
    expect(await fs.readFile(r.hookPath, "utf8")).toBe(HOOK_SCRIPT);
  });

  it("throws when .git/ is missing and allowSkip is false", async () => {
    fx = await makeFixture({});
    await expect(
      installHook({ cwd: fx.projectRoot, force: false, allowSkip: false }),
    ).rejects.toThrow(/not a git project/);
  });

  it("returns skippedReason when .git/ is missing and allowSkip is true", async () => {
    fx = await makeFixture({});
    const r = await installHook({
      cwd: fx.projectRoot,
      force: false,
      allowSkip: true,
    });
    expect(r.installed).toBe(false);
    expect(r.skippedReason).toBe("no-git-dir");
  });

  it("resolves .git as a worktree-linkage file (gitdir: pointer)", async () => {
    fx = await makeFixture({});
    const realGitDir = path.join(fx.projectRoot, "gitdir-real");
    await fs.mkdir(path.join(realGitDir, "hooks"), { recursive: true });
    await fs.writeFile(
      path.join(fx.projectRoot, ".git"),
      `gitdir: ${realGitDir}\n`,
    );
    const r = await installHook({ cwd: fx.projectRoot, force: false });
    expect(r.installed).toBe(true);
    expect(r.hookPath).toBe(path.join(realGitDir, "hooks", "pre-commit"));
  });
});

describe("uninstallHook", () => {
  it("removes our hook", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    await installHook({ cwd: fx.projectRoot, force: false });
    const r = await uninstallHook({ cwd: fx.projectRoot });
    expect(r.removed).toBe(true);
    await expect(fs.access(r.hookPath)).rejects.toThrow();
  });

  it("leaves a different hook untouched", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    const hookPath = path.join(fx.projectRoot, ".git", "hooks", "pre-commit");
    await fs.writeFile(hookPath, "#!/bin/sh\necho user hook\n");
    const r = await uninstallHook({ cwd: fx.projectRoot });
    expect(r.removed).toBe(false);
    expect(r.preExisting).toBe("other");
    expect(await fs.readFile(hookPath, "utf8")).toContain("user hook");
  });

  it("no-op when no hook exists", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    const r = await uninstallHook({ cwd: fx.projectRoot });
    expect(r.removed).toBe(false);
    expect(r.preExisting).toBe("absent");
  });
});

describe("runHook command", () => {
  it("install: human output reports installation", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    const cap = captureOutput(false);
    const code = await runHook({
      cwd: fx.projectRoot,
      output: cap.channel,
      action: "install",
      force: false,
    });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("Installed pre-commit hook");
  });

  it("install: --json emits structured payload", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    const cap = captureOutput(true);
    await runHook({
      cwd: fx.projectRoot,
      output: cap.channel,
      action: "install",
      force: false,
    });
    const lines = cap.jsonLines();
    expect(lines).toHaveLength(1);
    const payload = lines[0] as Record<string, unknown>;
    expect(payload["action"]).toBe("install");
    expect(payload["installed"]).toBe(true);
    expect(payload["preExisting"]).toBe("absent");
  });

  it("install: throws CliUserError when other hook exists without --force", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    await fs.writeFile(
      path.join(fx.projectRoot, ".git", "hooks", "pre-commit"),
      "#!/bin/sh\necho other\n",
    );
    const cap = captureOutput(false);
    await expect(
      runHook({
        cwd: fx.projectRoot,
        output: cap.channel,
        action: "install",
        force: false,
      }),
    ).rejects.toBeInstanceOf(CliUserError);
  });

  it("install --json: still throws CliUserError on foreign-hook block (exit-code parity)", async () => {
    // Sonnet/Opus review P2: in JSON mode the function previously emitted
    // {installed: false, preExisting: "other"} and returned 0, while human
    // mode threw and exited 1. JSON consumers can't rely on exit codes.
    // After the fix, JSON mode emits the payload AND throws, so the exit
    // code matches human mode.
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    await fs.writeFile(
      path.join(fx.projectRoot, ".git", "hooks", "pre-commit"),
      "#!/bin/sh\necho other\n",
    );
    const cap = captureOutput(true);
    await expect(
      runHook({
        cwd: fx.projectRoot,
        output: cap.channel,
        action: "install",
        force: false,
      }),
    ).rejects.toBeInstanceOf(CliUserError);
    // Payload is still emitted before the throw — scripts get visibility
    // into preExisting/hookPath even when the install was refused.
    const lines = cap.jsonLines();
    expect(lines).toHaveLength(1);
    const payload = lines[0] as Record<string, unknown>;
    expect(payload["installed"]).toBe(false);
    expect(payload["preExisting"]).toBe("other");
  });

  it("uninstall: reports removal", async () => {
    fx = await makeFixture({});
    await gitInit(fx.projectRoot);
    await installHook({ cwd: fx.projectRoot, force: false });
    const cap = captureOutput(false);
    await runHook({
      cwd: fx.projectRoot,
      output: cap.channel,
      action: "uninstall",
      force: false,
    });
    expect(cap.stdout()).toContain("Removed pre-commit hook");
  });
});
