import { afterEach, describe, expect, it } from "vitest";

import { listProfiles, profileExists } from "../../src/resolver/index.js";
import { makeFixture, type Fixture } from "../helpers/fixture.js";

describe("listProfiles() — R1", () => {
  let fx: Fixture | undefined;
  afterEach(async () => {
    if (fx) {
      await fx.cleanup();
      fx = undefined;
    }
  });

  it("returns top-level dir names of .claude-profiles, lex-sorted", async () => {
    fx = await makeFixture({
      profiles: {
        zeta: { manifest: {} },
        alpha: { manifest: {} },
        mid: { manifest: {} },
      },
    });
    const names = await listProfiles({ projectRoot: fx.projectRoot });
    expect(names).toEqual(["alpha", "mid", "zeta"]);
  });

  it("excludes entries beginning with _", async () => {
    fx = await makeFixture({
      profiles: { real: { manifest: {} } },
      components: { compA: { files: { "f": "1" } } }, // creates _components/
    });
    const names = await listProfiles({ projectRoot: fx.projectRoot });
    expect(names).toEqual(["real"]);
  });

  it("excludes entries beginning with .", async () => {
    fx = await makeFixture({
      profiles: { real: { manifest: {} } },
    });
    // Manually create a hidden dir to verify filtering.
    const fs = await import("node:fs");
    const path = await import("node:path");
    fs.mkdirSync(path.join(fx.projectRoot, ".claude-profiles", ".hidden"));

    const names = await listProfiles({ projectRoot: fx.projectRoot });
    expect(names).toEqual(["real"]);
  });

  it("returns [] when .claude-profiles/ does not exist", async () => {
    fx = await makeFixture({});
    // Note: makeFixture only creates .claude-profiles when there are profiles
    // or components. With an empty spec, .claude-profiles/ may not exist.
    const names = await listProfiles({ projectRoot: fx.projectRoot });
    expect(names).toEqual([]);
  });
});

describe("profileExists()", () => {
  let fx: Fixture | undefined;
  afterEach(async () => {
    if (fx) {
      await fx.cleanup();
      fx = undefined;
    }
  });

  it("returns true for an existing profile", async () => {
    fx = await makeFixture({ profiles: { p: { manifest: {} } } });
    expect(await profileExists("p", fx.projectRoot)).toBe(true);
  });

  it("returns false for a missing profile", async () => {
    fx = await makeFixture({ profiles: { p: { manifest: {} } } });
    expect(await profileExists("ghost", fx.projectRoot)).toBe(false);
  });
});
