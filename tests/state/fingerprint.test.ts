import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  compareFingerprint,
  fingerprintFromMergedFiles,
  fingerprintTree,
  hashBytes,
  recordMtimes,
} from "../../src/state/fingerprint.js";
import type { MergedFile } from "../../src/merge/types.js";

describe("fingerprint", () => {
  let root: string;
  beforeEach(async () => {
    root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-fp-"));
  });
  afterEach(async () => {
    await fs.rm(root, { recursive: true, force: true });
  });

  it("hashBytes is sha256 hex", () => {
    expect(hashBytes(Buffer.from("hi"))).toBe(
      "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4",
    );
  });

  it("fingerprintFromMergedFiles records size + content hash, mtime=0", () => {
    const files: MergedFile[] = [
      {
        path: "a.md",
        bytes: Buffer.from("hello"),
        contributors: ["x"],
        mergePolicy: "concat",
        destination: ".claude",
      },
    ];
    const fp = fingerprintFromMergedFiles(files);
    expect(fp.files["a.md"]?.size).toBe(5);
    expect(fp.files["a.md"]?.contentHash).toBe(hashBytes(Buffer.from("hello")));
    expect(fp.files["a.md"]?.mtimeMs).toBe(0);
  });

  it("fingerprintTree walks subdirectories with posix relPaths", async () => {
    await fs.mkdir(path.join(root, "sub"), { recursive: true });
    await fs.writeFile(path.join(root, "top.md"), "TOP");
    await fs.writeFile(path.join(root, "sub", "nested.md"), "NESTED");
    const fp = await fingerprintTree(root);
    expect(Object.keys(fp.files).sort()).toEqual(["sub/nested.md", "top.md"]);
    expect(fp.files["top.md"]?.size).toBe(3);
    expect(fp.files["sub/nested.md"]?.size).toBe(6);
  });

  it("recordMtimes overlays live stat mtimes onto an existing fingerprint", async () => {
    const files: MergedFile[] = [
      { path: "f", bytes: Buffer.from("x"), contributors: ["a"], mergePolicy: "last-wins", destination: ".claude" },
    ];
    const fp = fingerprintFromMergedFiles(files);
    await fs.writeFile(path.join(root, "f"), "x");
    const updated = await recordMtimes(root, fp);
    expect(updated.files["f"]?.mtimeMs).toBeGreaterThan(0);
  });

  it("compareFingerprint flags added files", async () => {
    const fp = fingerprintFromMergedFiles([]);
    await fs.writeFile(path.join(root, "new.md"), "hi");
    const drift = await compareFingerprint(root, fp);
    expect(drift).toEqual([{ relPath: "new.md", kind: "added" }]);
  });

  it("compareFingerprint flags deleted files", async () => {
    const fp = fingerprintFromMergedFiles([
      { path: "gone.md", bytes: Buffer.from("x"), contributors: ["a"], mergePolicy: "concat", destination: ".claude" },
    ]);
    const drift = await compareFingerprint(root, fp);
    expect(drift).toEqual([{ relPath: "gone.md", kind: "deleted" }]);
  });

  it("compareFingerprint flags modified files via content hash slow-path", async () => {
    let fp = fingerprintFromMergedFiles([
      { path: "f.md", bytes: Buffer.from("v1"), contributors: ["a"], mergePolicy: "concat", destination: ".claude" },
    ]);
    await fs.writeFile(path.join(root, "f.md"), "v1");
    fp = await recordMtimes(root, fp);
    // Modify the file with different content but same byte length.
    await fs.writeFile(path.join(root, "f.md"), "v2");
    const drift = await compareFingerprint(root, fp);
    expect(drift).toEqual([{ relPath: "f.md", kind: "modified" }]);
  });

  it("compareFingerprint short-circuits unchanged via mtime+size fast path", async () => {
    let fp = fingerprintFromMergedFiles([
      { path: "f.md", bytes: Buffer.from("v1"), contributors: ["a"], mergePolicy: "concat", destination: ".claude" },
    ]);
    await fs.writeFile(path.join(root, "f.md"), "v1");
    fp = await recordMtimes(root, fp);
    const drift = await compareFingerprint(root, fp);
    expect(drift).toEqual([{ relPath: "f.md", kind: "unchanged" }]);
  });

  it("compareFingerprint handles many files with mostly-unchanged metadata efficiently", async () => {
    // Build a fingerprint of 20 files; touch only one to invalidate via mtime.
    const merged: MergedFile[] = [];
    for (let i = 0; i < 20; i++) {
      merged.push({
        path: `f-${i}.md`,
        bytes: Buffer.from(`v-${i}`),
        contributors: ["a"],
        mergePolicy: "concat",
        destination: ".claude",
      });
      await fs.writeFile(path.join(root, `f-${i}.md`), `v-${i}`);
    }
    let fp = fingerprintFromMergedFiles(merged);
    fp = await recordMtimes(root, fp);

    // Modify one file's content + mtime.
    const target = path.join(root, "f-7.md");
    await new Promise((r) => setTimeout(r, 10));
    await fs.writeFile(target, "MODIFIED");

    const drift = await compareFingerprint(root, fp);
    const modified = drift.filter((d) => d.kind === "modified");
    expect(modified).toEqual([{ relPath: "f-7.md", kind: "modified" }]);
    expect(drift.filter((d) => d.kind === "unchanged")).toHaveLength(19);
  });
});
