/**
 * Unit tests for src/markers.ts — single source of truth for the project-root
 * CLAUDE.md managed-block markers (cw6 / spec §12).
 *
 * Coverage: regex constant shape, parseMarkers happy/missing/malformed paths,
 * version capture, namespace tail handling, nested-marker rejection,
 * renderManagedBlock round-trip, and injectMarkersIntoFile (idempotency +
 * byte-for-byte preservation of pre-existing user content).
 */

import { describe, expect, it } from "vitest";

import {
  MARKER_REGEX,
  injectMarkersIntoFile,
  parseMarkers,
  renderManagedBlock,
} from "../src/markers.js";

describe("MARKER_REGEX (spec §12.3)", () => {
  it("matches the canonical begin/end pair around a non-greedy body", () => {
    const text = `<!-- claude-profiles:v1:begin -->
body
<!-- claude-profiles:v1:end -->`;
    const m = text.match(MARKER_REGEX);
    expect(m).not.toBeNull();
    expect(m?.[1]).toBe("1");
    // Tail capture is the bytes between version and `-->` (canonical: " ").
    expect(m?.[3]).toContain("body");
  });

  it("body is non-greedy: the first :end terminates the match", () => {
    // Two managed blocks in one document — the regex must not collapse them.
    const text = [
      "<!-- claude-profiles:v1:begin -->",
      "first",
      "<!-- claude-profiles:v1:end -->",
      "between",
      "<!-- claude-profiles:v1:begin -->",
      "second",
      "<!-- claude-profiles:v1:end -->",
    ].join("\n");
    const m = text.match(MARKER_REGEX);
    // First match must be the FIRST block only.
    expect(m?.[3]).toContain("first");
    expect(m?.[3]).not.toContain("second");
    expect(m?.[3]).not.toContain("between");
  });
});

describe("parseMarkers", () => {
  it("happy path: returns before/section/after split + version", () => {
    const content = [
      "user header",
      "<!-- claude-profiles:v1:begin -->",
      "<!-- Managed block. -->",
      "",
      "managed body line",
      "<!-- claude-profiles:v1:end -->",
      "user footer",
      "",
    ].join("\n");
    const r = parseMarkers(content);
    expect(r.found).toBe(true);
    if (!r.found) return;
    expect(r.version).toBe(1);
    expect(r.before).toBe("user header\n");
    expect(r.after).toBe("\nuser footer\n");
    // section is the captured body between begin/end markers (excluding the
    // marker lines themselves).
    expect(r.section).toContain("managed body line");
  });

  it("returns absent when no markers are present", () => {
    const r = parseMarkers("just user content\n");
    expect(r.found).toBe(false);
    if (r.found) return;
    expect(r.reason).toBe("absent");
  });

  it("returns malformed when only :begin is present", () => {
    const r = parseMarkers("<!-- claude-profiles:v1:begin -->\nno end\n");
    expect(r.found).toBe(false);
    if (r.found) return;
    expect(r.reason).toBe("malformed");
  });

  it("returns malformed when only :end is present", () => {
    const r = parseMarkers("no begin\n<!-- claude-profiles:v1:end -->\n");
    expect(r.found).toBe(false);
    if (r.found) return;
    expect(r.reason).toBe("malformed");
  });

  it("returns malformed when versions mismatch (begin v1 / end v2)", () => {
    const text = [
      "<!-- claude-profiles:v1:begin -->",
      "body",
      "<!-- claude-profiles:v2:end -->",
    ].join("\n");
    const r = parseMarkers(text);
    expect(r.found).toBe(false);
    if (r.found) return;
    expect(r.reason).toBe("malformed");
  });

  it("returns malformed when more than one well-formed block exists", () => {
    // Per spec §12.3: "More than one match is reserved and currently rejected;
    // v1 implementations may treat it as malformed."
    const text = [
      "<!-- claude-profiles:v1:begin -->",
      "first",
      "<!-- claude-profiles:v1:end -->",
      "",
      "<!-- claude-profiles:v1:begin -->",
      "second",
      "<!-- claude-profiles:v1:end -->",
    ].join("\n");
    const r = parseMarkers(text);
    expect(r.found).toBe(false);
    if (r.found) return;
    expect(r.reason).toBe("malformed");
  });

  it("captures higher version numbers verbatim", () => {
    const text = `<!-- claude-profiles:v42:begin -->\nbody\n<!-- claude-profiles:v42:end -->`;
    const r = parseMarkers(text);
    expect(r.found).toBe(true);
    if (!r.found) return;
    expect(r.version).toBe(42);
  });

  it("supports an empty managed section", () => {
    const text = [
      "before",
      "<!-- claude-profiles:v1:begin -->",
      "<!-- claude-profiles:v1:end -->",
      "after",
    ].join("\n");
    const r = parseMarkers(text);
    expect(r.found).toBe(true);
    if (!r.found) return;
    expect(r.section.trim()).toBe("");
    expect(r.before).toBe("before\n");
    expect(r.after).toBe("\nafter");
  });
});

describe("renderManagedBlock", () => {
  it("emits the canonical v1 block with begin, self-doc comment, body, end", () => {
    const block = renderManagedBlock("hello world\n");
    expect(block).toContain("<!-- claude-profiles:v1:begin -->");
    expect(block).toContain("<!-- claude-profiles:v1:end -->");
    expect(block).toContain("Managed block");
    expect(block).toContain("hello world");
  });

  it("round-trips through parseMarkers", () => {
    const body = "line a\nline b\n";
    const block = renderManagedBlock(body);
    const r = parseMarkers(block);
    expect(r.found).toBe(true);
    if (!r.found) return;
    expect(r.section).toContain("line a");
    expect(r.section).toContain("line b");
    expect(r.version).toBe(1);
  });

  it("accepts an empty body", () => {
    const block = renderManagedBlock("");
    const r = parseMarkers(block);
    expect(r.found).toBe(true);
    if (!r.found) return;
    expect(r.version).toBe(1);
  });
});

describe("injectMarkersIntoFile", () => {
  it("appends a marker block when input has no markers (preserves content above)", () => {
    const original = "# Project\n\nuser-authored content\n";
    const out = injectMarkersIntoFile(original);
    // Original content present, byte-for-byte, at the start of the new file.
    expect(out.startsWith(original)).toBe(true);
    // Markers appended after.
    expect(out).toContain("<!-- claude-profiles:v1:begin -->");
    expect(out).toContain("<!-- claude-profiles:v1:end -->");
    // The result parses cleanly.
    const parsed = parseMarkers(out);
    expect(parsed.found).toBe(true);
  });

  it("is a no-op when markers already exist", () => {
    const original = [
      "# Project",
      "",
      "<!-- claude-profiles:v1:begin -->",
      "managed body",
      "<!-- claude-profiles:v1:end -->",
      "",
      "footer",
    ].join("\n");
    const out = injectMarkersIntoFile(original);
    expect(out).toBe(original);
  });

  it("is idempotent (running twice == running once)", () => {
    const original = "# user content\n";
    const once = injectMarkersIntoFile(original);
    const twice = injectMarkersIntoFile(once);
    expect(twice).toBe(once);
  });

  it("preserves trailing-newline absence in user content", () => {
    // No trailing newline on user content. We must not silently mutate that —
    // the only added bytes must be the marker block itself.
    const original = "no trailing newline";
    const out = injectMarkersIntoFile(original);
    expect(out.startsWith(original)).toBe(true);
  });

  it("preserves multiline user content with embedded HTML byte-for-byte", () => {
    const original = [
      "# Project",
      "",
      "<details><summary>more</summary>",
      "Some <b>html</b> inside markdown.",
      "</details>",
      "",
    ].join("\n");
    const out = injectMarkersIntoFile(original);
    expect(out.startsWith(original)).toBe(true);
    const parsed = parseMarkers(out);
    expect(parsed.found).toBe(true);
    if (!parsed.found) return;
    expect(parsed.before).toContain("<details>");
  });
});
