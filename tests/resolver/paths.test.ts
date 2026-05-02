import * as os from "node:os";
import * as path from "node:path";

import { describe, expect, it } from "vitest";

import { InvalidManifestError } from "../../src/errors/index.js";
import {
  buildPaths,
  classifyInclude,
  isExternal,
  isValidProfileName,
} from "../../src/resolver/paths.js";

describe("classifyInclude() — R37", () => {
  const projectRoot = path.resolve("/tmp/some-project");
  const referencingDir = path.join(projectRoot, ".claude-profiles", "myprofile");
  const paths = buildPaths(projectRoot);

  it("treats bare names as components rooted at _components/<name>/", () => {
    const ref = classifyInclude("compA", referencingDir, paths, "p");
    expect(ref.kind).toBe("component");
    expect(ref.resolvedPath).toBe(
      path.join(projectRoot, ".claude-profiles", "_components", "compA"),
    );
    expect(ref.external).toBe(false);
  });

  it("treats './...' as relative to the referencing profile dir", () => {
    const ref = classifyInclude("./neighbor", referencingDir, paths, "p");
    expect(ref.kind).toBe("relative");
    expect(ref.resolvedPath).toBe(path.resolve(referencingDir, "./neighbor"));
  });

  it("treats '../...' as relative", () => {
    const ref = classifyInclude("../sib", referencingDir, paths, "p");
    expect(ref.kind).toBe("relative");
    expect(ref.resolvedPath).toBe(path.resolve(referencingDir, "../sib"));
  });

  it("expands '~/...' to the home directory and tags kind=tilde", () => {
    const ref = classifyInclude("~/some/path", referencingDir, paths, "p");
    expect(ref.kind).toBe("tilde");
    expect(ref.resolvedPath).toBe(path.resolve(path.join(os.homedir(), "some/path")));
  });

  it("expands bare '~' to the home directory", () => {
    const ref = classifyInclude("~", referencingDir, paths, "p");
    expect(ref.kind).toBe("tilde");
    expect(ref.resolvedPath).toBe(path.resolve(os.homedir()));
  });

  it("flags an absolute path outside the project root as external (kind unchanged)", () => {
    const outside = "/var/tmp/external-profile";
    const ref = classifyInclude(outside, referencingDir, paths, "p");
    expect(ref.external).toBe(true);
    expect(ref.kind).toBe("absolute"); // syntactic kind preserved
  });

  it("does NOT flag an absolute path inside the project root as external", () => {
    const inside = path.join(projectRoot, ".claude-profiles", "_components", "x");
    const ref = classifyInclude(inside, referencingDir, paths, "p");
    expect(ref.external).toBe(false);
    expect(ref.kind).toBe("absolute");
  });

  it("rejects bare-with-slashes strings (R37 admits exactly four forms)", () => {
    expect(() => classifyInclude("foo/bar", referencingDir, paths, "p")).toThrow(
      InvalidManifestError,
    );
  });

  it("rejects bare-with-backslashes strings on every platform", () => {
    expect(() => classifyInclude("foo\\bar", referencingDir, paths, "p")).toThrow(
      InvalidManifestError,
    );
  });

  it("rejects '~user' form as unsupported", () => {
    expect(() => classifyInclude("~bob/path", referencingDir, paths, "p")).toThrow(
      InvalidManifestError,
    );
  });

  it("rejects empty string", () => {
    expect(() => classifyInclude("", referencingDir, paths, "p")).toThrow(
      InvalidManifestError,
    );
  });

  it("rejects '.' and '..' (would otherwise resolve to _components/ or .claude-profiles/)", () => {
    expect(() => classifyInclude(".", referencingDir, paths, "p")).toThrow(
      InvalidManifestError,
    );
    expect(() => classifyInclude("..", referencingDir, paths, "p")).toThrow(
      InvalidManifestError,
    );
  });
});

describe("isValidProfileName()", () => {
  it("accepts a plain bare name", () => {
    expect(isValidProfileName("frontend")).toBe(true);
    expect(isValidProfileName("staging-leaf")).toBe(true);
  });

  it("rejects path traversal forms", () => {
    expect(isValidProfileName("..")).toBe(false);
    expect(isValidProfileName(".")).toBe(false);
    expect(isValidProfileName("../outside")).toBe(false);
    expect(isValidProfileName("foo/bar")).toBe(false);
    expect(isValidProfileName("foo\\bar")).toBe(false);
  });

  it("rejects empty / hidden / underscore-prefixed names", () => {
    expect(isValidProfileName("")).toBe(false);
    expect(isValidProfileName("_components")).toBe(false);
    expect(isValidProfileName(".hidden")).toBe(false);
  });
});

describe("isExternal()", () => {
  it("returns true when the path is outside the project root", () => {
    expect(isExternal("/var/data", "/Users/x/proj")).toBe(true);
  });

  it("returns false when the path is inside the project root", () => {
    expect(isExternal("/Users/x/proj/sub/dir", "/Users/x/proj")).toBe(false);
  });

  it("returns false when the path is the project root itself", () => {
    expect(isExternal("/Users/x/proj", "/Users/x/proj")).toBe(false);
  });

  it("does NOT flag a filename whose first component begins with '..' but is not a traversal segment", () => {
    // Regression guard for the prefix-vs-segment bug: `..hidden` is a
    // legitimate in-root filename, not a `..` parent-traversal segment.
    expect(isExternal("/proj/..hidden", "/proj")).toBe(false);
  });
});
