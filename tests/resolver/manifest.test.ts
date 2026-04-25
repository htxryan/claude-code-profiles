import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { InvalidManifestError } from "../../src/errors/index.js";
import { loadManifest } from "../../src/resolver/manifest.js";

describe("loadManifest()", () => {
  let tmp: string | undefined;

  afterEach(async () => {
    if (tmp) {
      await fs.rm(tmp, { recursive: true, force: true });
      tmp = undefined;
    }
  });

  async function withDir(content: string | null): Promise<string> {
    tmp = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-mf-"));
    if (content !== null) {
      await fs.writeFile(path.join(tmp, "profile.json"), content);
    }
    return tmp;
  }

  it("parses a complete manifest", async () => {
    const dir = await withDir(
      JSON.stringify({
        name: "p",
        description: "d",
        extends: "x",
        includes: ["a", "b"],
        tags: ["t"],
      }),
    );
    const { manifest, warnings } = await loadManifest(dir, "p");
    expect(manifest).toEqual({
      name: "p",
      description: "d",
      extends: "x",
      includes: ["a", "b"],
      tags: ["t"],
    });
    expect(warnings).toEqual([]);
  });

  it("returns MissingManifest warning for a directory without profile.json", async () => {
    const dir = await withDir(null);
    const { manifest, warnings } = await loadManifest(dir, "p");
    expect(manifest).toEqual({});
    expect(warnings).toHaveLength(1);
    expect(warnings[0]!.code).toBe("MissingManifest");
    expect(warnings[0]!.source).toBe("p");
  });

  it("emits an UnknownManifestField warning for unrecognized keys (R36)", async () => {
    const dir = await withDir(JSON.stringify({ name: "p", weird: 1 }));
    const { warnings } = await loadManifest(dir, "p");
    const u = warnings.filter((w) => w.code === "UnknownManifestField");
    expect(u).toHaveLength(1);
    expect(u[0]!.message).toContain("weird");
  });

  it("throws InvalidManifestError on unparseable JSON", async () => {
    const dir = await withDir("{not valid");
    await expect(loadManifest(dir, "p")).rejects.toThrow(InvalidManifestError);
  });

  it("throws InvalidManifestError on top-level array", async () => {
    const dir = await withDir(JSON.stringify(["nope"]));
    await expect(loadManifest(dir, "p")).rejects.toThrow(InvalidManifestError);
  });

  it("throws on wrong type for extends", async () => {
    const dir = await withDir(JSON.stringify({ extends: 42 }));
    await expect(loadManifest(dir, "p")).rejects.toThrow(InvalidManifestError);
  });

  it("throws on includes containing non-strings", async () => {
    const dir = await withDir(JSON.stringify({ includes: ["ok", 7] }));
    await expect(loadManifest(dir, "p")).rejects.toThrow(InvalidManifestError);
  });
});
