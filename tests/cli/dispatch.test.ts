import { afterEach, describe, expect, it } from "vitest";

import { dispatch } from "../../src/cli/dispatch.js";
import { CliNotImplementedError } from "../../src/cli/exit.js";
import type { Command, GlobalOptions } from "../../src/cli/types.js";
import { makeFixture, type Fixture } from "../helpers/fixture.js";
import { captureOutput } from "./helpers/output-sink.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

function ctxFor(json = false) {
  const cap = captureOutput(json);
  return {
    cap,
    ctx: {
      output: cap.channel,
      mode: "non-interactive" as const,
      version: "9.9.9",
      signalHandlers: false,
    },
  };
}

function global(json = false): GlobalOptions {
  return { json, cwd: ".", onDrift: null, noColor: false };
}

describe("dispatch — top-level routes", () => {
  it("version → prints version string", async () => {
    const { cap, ctx } = ctxFor();
    const code = await dispatch({ kind: "version" }, global(), ctx);
    expect(code).toBe(0);
    expect(cap.stdout()).toContain("claude-profiles 9.9.9");
  });

  it("help (no verb) → prints top-level help with all R29 verbs", async () => {
    const { cap, ctx } = ctxFor();
    await dispatch({ kind: "help", verb: null }, global(), ctx);
    const out = cap.stdout();
    for (const verb of ["init", "list", "use", "status", "drift", "diff", "new", "validate", "sync", "hook"]) {
      expect(out).toContain(verb);
    }
  });

  it("help <verb> → prints verb-specific help", async () => {
    const { cap, ctx } = ctxFor();
    await dispatch({ kind: "help", verb: "use" }, global(), ctx);
    expect(cap.stdout()).toContain("use <name>");
    expect(cap.stdout()).toContain("--on-drift=");
  });
});

describe("dispatch — every command kind dispatches without throwing on baseline fixture", () => {
  it("list / status / drift / diff / validate / new on a happy fixture", async () => {
    fx = await makeFixture({
      profiles: {
        a: { manifest: { name: "a" }, files: { "x.md": "X\n" } },
        b: { manifest: { name: "b" }, files: { "y.md": "Y\n" } },
      },
    });
    const cwd = fx.projectRoot;
    const cases: Command[] = [
      { kind: "list" },
      { kind: "status" },
      { kind: "drift", preCommitWarn: false },
      { kind: "diff", a: "a", b: "b" },
      { kind: "validate", profile: null },
      { kind: "new", profile: "scratch", description: "test" },
    ];
    for (const cmd of cases) {
      const { ctx } = ctxFor();
      const code = await dispatch(cmd, { ...global(), cwd }, ctx);
      expect(code).toBe(0);
    }
  });
});

describe("dispatch — E5/E6 stubs", () => {
  it("init → CliNotImplementedError", async () => {
    const { ctx } = ctxFor();
    await expect(dispatch({ kind: "init" }, global(), ctx)).rejects.toBeInstanceOf(
      CliNotImplementedError,
    );
  });

  it("hook install → CliNotImplementedError", async () => {
    const { ctx } = ctxFor();
    await expect(
      dispatch({ kind: "hook", action: "install" }, global(), ctx),
    ).rejects.toBeInstanceOf(CliNotImplementedError);
  });

  it("hook uninstall → CliNotImplementedError", async () => {
    const { ctx } = ctxFor();
    await expect(
      dispatch({ kind: "hook", action: "uninstall" }, global(), ctx),
    ).rejects.toBeInstanceOf(CliNotImplementedError);
  });
});
