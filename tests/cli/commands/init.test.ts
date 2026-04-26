import { promises as fs } from "node:fs";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { runInit } from "../../../src/cli/commands/init.js";
import { CliUserError } from "../../../src/cli/exit.js";
import { listProfiles } from "../../../src/resolver/discover.js";
import { makeFixture, type Fixture } from "../../helpers/fixture.js";
import { captureOutput } from "../helpers/output-sink.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

describe("init (R26, R27, R28)", () => {
  it("creates .claude-profiles/ in a fresh project and updates .gitignore", async () => {
    fx = await makeFixture({});
    // Ensure no .claude-profiles/ pre-exists (makeFixture only creates it
    // when profiles/components are passed).
    const cap = captureOutput(false);
    const code = await runInit({
      cwd: fx.projectRoot,
      output: cap.channel,
      starterName: "default",
      seedFromClaudeDir: false,
      installHook: false,
      signalHandlers: false,
    });
    expect(code).toBe(0);
    const profilesStat = await fs.stat(path.join(fx.projectRoot, ".claude-profiles"));
    expect(profilesStat.isDirectory()).toBe(true);

    const gitignore = await fs.readFile(path.join(fx.projectRoot, ".gitignore"), "utf8");
    expect(gitignore).toContain(".claude/");
    expect(gitignore).toContain(".claude-profiles/.meta/");
    expect(cap.stdout()).toContain("Initialised claude-profiles");
  });

  it("R27: seeds a starter profile from existing .claude/", async () => {
    fx = await makeFixture({});
    // Create a live `.claude/` to seed from.
    await fs.mkdir(path.join(fx.projectRoot, ".claude"), { recursive: true });
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude", "CLAUDE.md"),
      "# project rules\n",
    );
    await fs.mkdir(path.join(fx.projectRoot, ".claude", "agents"), { recursive: true });
    await fs.writeFile(
      path.join(fx.projectRoot, ".claude", "agents", "reviewer.md"),
      "agent\n",
    );

    const cap = captureOutput(false);
    await runInit({
      cwd: fx.projectRoot,
      output: cap.channel,
      starterName: "default",
      seedFromClaudeDir: true,
      installHook: false,
      signalHandlers: false,
    });

    const seededClaude = path.join(
      fx.projectRoot,
      ".claude-profiles",
      "default",
      ".claude",
    );
    expect(await fs.readFile(path.join(seededClaude, "CLAUDE.md"), "utf8")).toBe(
      "# project rules\n",
    );
    expect(await fs.readFile(path.join(seededClaude, "agents", "reviewer.md"), "utf8")).toBe(
      "agent\n",
    );
    const manifest = JSON.parse(
      await fs.readFile(
        path.join(fx.projectRoot, ".claude-profiles", "default", "profile.json"),
        "utf8",
      ),
    );
    expect(manifest).toEqual({ name: "default" });

    const names = await listProfiles({ projectRoot: fx.projectRoot });
    expect(names).toContain("default");
    expect(cap.stdout()).toContain('Seeded starter profile "default"');
  });

  it("R26: refuses to overwrite an already-initialised .claude-profiles/", async () => {
    fx = await makeFixture({
      profiles: { existing: { manifest: { name: "existing" }, files: {} } },
    });
    const cap = captureOutput(false);
    await expect(
      runInit({
        cwd: fx.projectRoot,
        output: cap.channel,
        starterName: "default",
        seedFromClaudeDir: false,
        installHook: false,
        signalHandlers: false,
      }),
    ).rejects.toBeInstanceOf(CliUserError);
  });

  it("--no-seed skips seeding even when .claude/ exists", async () => {
    fx = await makeFixture({});
    await fs.mkdir(path.join(fx.projectRoot, ".claude"), { recursive: true });
    await fs.writeFile(path.join(fx.projectRoot, ".claude", "x"), "y");

    const cap = captureOutput(false);
    await runInit({
      cwd: fx.projectRoot,
      output: cap.channel,
      starterName: "default",
      seedFromClaudeDir: false,
      installHook: false,
      signalHandlers: false,
    });
    const names = await listProfiles({ projectRoot: fx.projectRoot });
    expect(names).toEqual([]);
  });

  it("rejects invalid starter name", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    await expect(
      runInit({
        cwd: fx.projectRoot,
        output: cap.channel,
        starterName: ".hidden",
        seedFromClaudeDir: false,
        installHook: false,
        signalHandlers: false,
      }),
    ).rejects.toBeInstanceOf(CliUserError);
  });

  it("--json mode emits a structured payload", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(true);
    await runInit({
      cwd: fx.projectRoot,
      output: cap.channel,
      starterName: "default",
      seedFromClaudeDir: false,
      installHook: false,
      signalHandlers: false,
    });
    const lines = cap.jsonLines();
    expect(lines).toHaveLength(1);
    const payload = lines[0] as Record<string, unknown>;
    expect(payload["projectRoot"]).toBe(path.resolve(fx.projectRoot));
    expect(payload["starterProfileSeeded"]).toBe(null);
    expect(payload["gitignoreCreated"]).toBe(true);
    expect(payload["hook"]).toBe(null);
  });

  it("installs the pre-commit hook in a git project", async () => {
    fx = await makeFixture({});
    await fs.mkdir(path.join(fx.projectRoot, ".git", "hooks"), { recursive: true });
    const cap = captureOutput(false);
    await runInit({
      cwd: fx.projectRoot,
      output: cap.channel,
      starterName: "default",
      seedFromClaudeDir: false,
      installHook: true,
      signalHandlers: false,
    });
    const hookPath = path.join(fx.projectRoot, ".git", "hooks", "pre-commit");
    const content = await fs.readFile(hookPath, "utf8");
    expect(content).toContain("command -v claude-profiles");
    expect(content).toContain("--pre-commit-warn");
  });

  it("gracefully skips hook install when .git/ is absent", async () => {
    fx = await makeFixture({});
    const cap = captureOutput(false);
    const code = await runInit({
      cwd: fx.projectRoot,
      output: cap.channel,
      starterName: "default",
      seedFromClaudeDir: false,
      installHook: true,
      signalHandlers: false,
    });
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("not a git project");
  });

  it("treats a populated .backup/ as already-initialised", async () => {
    // Opus review P3: classifyProfilesDir previously skipped `.backup`
    // unconditionally with the comment "empty backup dir from prior tooling
    // is fine", but never verified emptiness — so `.backup/` left behind by
    // a prior init was being mistaken for a fresh project. Under the .meta/
    // layout the same hazard applies one level deeper (populated
    // `.meta/backup/` inside an otherwise-clean `.meta/`) and is guarded by
    // the inner check in classifyProfilesDir.
    fx = await makeFixture({});
    await fs.mkdir(
      path.join(fx.projectRoot, ".claude-profiles", ".meta", "backup", "stale-snapshot"),
      { recursive: true },
    );
    const cap = captureOutput(false);
    await expect(
      runInit({
        cwd: fx.projectRoot,
        output: cap.channel,
        starterName: "default",
        seedFromClaudeDir: false,
        installHook: false,
        signalHandlers: false,
      }),
    ).rejects.toBeInstanceOf(CliUserError);
  });

  it("preserves existing .gitignore content (idempotent re-run)", async () => {
    fx = await makeFixture({});
    await fs.writeFile(
      path.join(fx.projectRoot, ".gitignore"),
      "# user content\nnode_modules/\n",
    );
    const cap = captureOutput(false);
    await runInit({
      cwd: fx.projectRoot,
      output: cap.channel,
      starterName: "default",
      seedFromClaudeDir: false,
      installHook: false,
      signalHandlers: false,
    });
    const gi = await fs.readFile(path.join(fx.projectRoot, ".gitignore"), "utf8");
    expect(gi).toContain("# user content");
    expect(gi).toContain("node_modules/");
    expect(gi).toContain(".claude/");
  });
});
