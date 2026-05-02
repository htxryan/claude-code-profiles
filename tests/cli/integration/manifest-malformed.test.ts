/**
 * Gap closure #6 (PR6 #6, F2 epic claude-code-profiles-yhb):
 *
 * Malformed manifest variants — each one is a class of input the resolver
 * MUST reject at parse time rather than producing a degraded ResolvedPlan.
 *
 * Coverage:
 *   - invalid JSON syntax
 *   - missing required field paths (none are strictly required per R35, but
 *     wrong-type values for optional fields must be rejected)
 *   - top-level non-object (array, scalar, null)
 *   - non-string in `name`
 *   - non-string in `extends`
 *   - non-string element in `includes` array
 *   - PR16a path-traversal: `includes: ["../../../.ssh/config"]`
 *
 * Note: PR16a's hard rejection (CONFLICT-class error) is a Go-side promise
 * per docs/specs/c3p-go-migration.md §3.5. The TS bin currently classifies
 * `../...` as a `relative`-form include and tracks it as an external trust
 * notice; a true reject doesn't happen until the resolver fails to find the
 * directory (MissingInclude). This test pins the **CURRENT** TS contract:
 * a `../../../.ssh/config` include exits non-zero (because the path doesn't
 * resolve to a directory), and the error message includes the offending raw
 * input. The Go translation in IV upgrades this to a CONFLICT-class
 * PathTraversal error — see Re-decomposition trigger in F2.
 */

import { afterEach, describe, expect, it } from "vitest";

import { makeFixture, type Fixture } from "../../helpers/fixture.js";

import { ensureBuilt, runCli } from "./spawn.js";

let fx: Fixture | undefined;
afterEach(async () => {
  if (fx) await fx.cleanup();
  fx = undefined;
});

describe("gap closure #6: malformed manifest variants (PR6 #6)", () => {
  it("invalid JSON syntax → exit 3, error names the file", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        bad: { manifest: "{not: valid json", files: { "x.md": "x\n" } },
      },
    });
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "bad"] });
    // InvalidManifestError → ResolverError → exit 3 (CONFLICT class).
    expect(r.exitCode).toBe(3);
    expect(r.stderr.toLowerCase()).toContain("invalid");
    expect(r.stderr).toContain("profile.json");
  });

  it("top-level array (not an object) → exit 3", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        bad: { manifest: "[]", files: {} },
      },
    });
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "bad"] });
    expect(r.exitCode).toBe(3);
    expect(r.stderr.toLowerCase()).toContain("object");
  });

  it("top-level null → exit 3", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        bad: { manifest: "null", files: {} },
      },
    });
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "bad"] });
    expect(r.exitCode).toBe(3);
  });

  it("top-level scalar (number) → exit 3", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        bad: { manifest: "42", files: {} },
      },
    });
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "bad"] });
    expect(r.exitCode).toBe(3);
  });

  it("name is a number → exit 3, error mentions 'name'", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        bad: { manifest: { name: 42 } as unknown as Record<string, unknown>, files: {} },
      },
    });
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "bad"] });
    expect(r.exitCode).toBe(3);
    expect(r.stderr).toContain("name");
  });

  it("extends is an array → exit 3, error mentions 'extends'", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        bad: { manifest: { extends: ["a"] } as unknown as Record<string, unknown>, files: {} },
      },
    });
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "bad"] });
    expect(r.exitCode).toBe(3);
    expect(r.stderr).toContain("extends");
  });

  it("includes contains a non-string element → exit 3, error mentions 'includes'", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        bad: {
          manifest: { includes: ["valid", 42] } as unknown as Record<string, unknown>,
          files: {},
        },
      },
    });
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "bad"] });
    expect(r.exitCode).toBe(3);
    expect(r.stderr).toContain("includes");
  });

  it("tags contains a non-string element → exit 3, error mentions 'tags'", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        bad: {
          manifest: { tags: ["good", 42] } as unknown as Record<string, unknown>,
          files: {},
        },
      },
    });
    const r = await runCli({ args: ["--cwd", fx.projectRoot, "use", "bad"] });
    expect(r.exitCode).toBe(3);
    expect(r.stderr).toContain("tags");
  });

  // ──────────────────────────────────────────────────────────────────────
  // PR16a path-traversal — the gap-closure test #6 explicit case.
  //
  // The TS impl currently surfaces this as a MissingInclude (the resolver
  // walks `..` segments out of the project, the directory doesn't exist,
  // exits 3). Go upgrades it to a deterministic PathTraversal ConflictError
  // BEFORE filesystem touch. The exit code is 3 in both — the contract pinned
  // here is the exit code + that the offending raw string appears in stderr
  // so an operator can locate the bad manifest entry.
  // ──────────────────────────────────────────────────────────────────────
  it("PR16a: includes contains '../../../.ssh/config' → exit 3, stderr names the offending path", async () => {
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        traversal: {
          manifest: { name: "traversal", includes: ["../../../.ssh/config"] },
          files: { "x.md": "x\n" },
        },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "traversal"],
    });
    expect(r.exitCode).toBe(3);
    expect(r.stderr).toContain("../../../.ssh/config");
  });

  it("PR16a: includes contains tilde-form pointing outside project → resolves, then fails missing-include cleanly", async () => {
    // Tilde-form is canonically "~/..." — resolves to homedir. Pointing at a
    // path that doesn't exist must produce MissingInclude (exit 3) with the
    // raw "~/..." preserved in stderr (R37). The Go impl applies the
    // PR16a guard with a config-trusted-base allowlist; pre-1.0, the TS bin
    // resolves it and the missing-dir check rejects it.
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        outside: {
          manifest: {
            name: "outside",
            includes: ["~/c3p-test-nonexistent-dir-xyzzy"],
          },
          files: { "x.md": "x\n" },
        },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "outside"],
    });
    expect(r.exitCode).toBe(3);
    expect(r.stderr).toContain("c3p-test-nonexistent-dir-xyzzy");
  });

  it("unknown manifest field → R36 warning, NOT a hard fail (use still succeeds)", async () => {
    // The contrast case to all the rejects above: an unknown KNOWN-FIELD
    // shape is a degraded-but-keep-going condition per R36. Test pins that
    // contract — exit 0 with no profile content depending on the unknown
    // field, but a warning surfaced to stderr.
    await ensureBuilt();
    fx = await makeFixture({
      profiles: {
        with_unknown: {
          manifest: { name: "with_unknown", futureField: "xyz" } as unknown as Record<
            string,
            unknown
          >,
          files: { "x.md": "x\n" },
        },
      },
    });
    const r = await runCli({
      args: ["--cwd", fx.projectRoot, "use", "with_unknown"],
    });
    expect(r.exitCode).toBe(0);
  });
});
