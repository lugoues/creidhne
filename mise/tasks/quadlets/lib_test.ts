#!/usr/bin/env -S deno test --allow-read --allow-write --allow-run --allow-env
/**
 * Integration tests for deploy/lib.ts
 *
 * Exercises listExistingFiles, computePlan, writeFile, removeFile,
 * pruneEmptyDirs, and ensureDir against real filesystem state.
 *
 * These tests exist because dax's path API returns objects where
 * isFile/isDirectory are METHODS (not boolean properties). A missing
 * () turns `if (entry.isFile)` into `if (<function>)` which is always
 * truthy — causing directories to be treated as files and vice versa.
 * This bug caused `sudo rm -rf` on the entire quadlet directory.
 */

import { assertEquals } from "jsr:@std/assert";
import { join } from "jsr:@std/path";
import {
  listExistingFiles,
  computePlan,
  planSummary,
  writeFile,
  removeFile,
  pruneEmptyDirs,
  normalizeFileValue,
  QUADLET_EXTENSIONS,
  type FileValue,
  type FileChange,
} from "./lib.ts";

// --- Helpers ---

async function makeTempDir(): Promise<string> {
  return await Deno.makeTempDir({ prefix: "lib_test_" });
}

async function writeTestFile(dir: string, relPath: string, content = ""): Promise<void> {
  const full = join(dir, relPath);
  await Deno.mkdir(join(full, "..").replace(/\/\.\.$/, ""), { recursive: true });
  // Ensure parent exists properly
  const parent = full.substring(0, full.lastIndexOf("/"));
  await Deno.mkdir(parent, { recursive: true });
  await Deno.writeTextFile(full, content);
}

// --- listExistingFiles ---

Deno.test("listExistingFiles: returns empty set for nonexistent dir", async () => {
  const result = await listExistingFiles("/tmp/does-not-exist-" + Date.now());
  assertEquals(result.size, 0);
});

Deno.test("listExistingFiles: finds flat quadlet files only", async () => {
  const dir = await makeTempDir();
  try {
    await Deno.writeTextFile(join(dir, "app.container"), "[Container]\n");
    await Deno.writeTextFile(join(dir, "net.network"), "[Network]\n");
    await Deno.writeTextFile(join(dir, "README.md"), "ignore me");
    await Deno.writeTextFile(join(dir, "notes.txt"), "ignore me too");

    const result = await listExistingFiles(dir);
    assertEquals(result.size, 2);
    assertEquals(result.has("app.container"), true);
    assertEquals(result.has("net.network"), true);
    assertEquals(result.has("README.md"), false);
    assertEquals(result.has("notes.txt"), false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("listExistingFiles: directories at root are NOT listed as files", async () => {
  // This is the exact bug that caused the catastrophic rm -rf.
  // A directory named like "images" or "foo.context" must NOT appear in the set.
  const dir = await makeTempDir();
  try {
    await Deno.mkdir(join(dir, "images"));
    await Deno.mkdir(join(dir, "some-dir.container")); // tricky: dir with quadlet extension
    await Deno.writeTextFile(join(dir, "app.container"), "[Container]\n");

    const result = await listExistingFiles(dir);
    assertEquals(result.has("app.container"), true);
    // Directories must never be listed, even if they have a quadlet extension
    assertEquals(result.has("images"), false);
    assertEquals(result.has("some-dir.container"), false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("listExistingFiles: walks images/ subdirectory recursively", async () => {
  const dir = await makeTempDir();
  try {
    await Deno.mkdir(join(dir, "images", "app.context", "scripts"), { recursive: true });
    await Deno.writeTextFile(join(dir, "images", "app.Containerfile"), "FROM node:20\n");
    await Deno.writeTextFile(join(dir, "images", "app.context", "config.toml"), "[app]\n");
    await Deno.writeTextFile(join(dir, "images", "app.context", "scripts", "entrypoint.sh"), "#!/bin/bash\n");

    const result = await listExistingFiles(dir);
    assertEquals(result.has("images/app.Containerfile"), true);
    assertEquals(result.has("images/app.context/config.toml"), true);
    assertEquals(result.has("images/app.context/scripts/entrypoint.sh"), true);
    assertEquals(result.size, 3);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("listExistingFiles: subdirectories inside images/ are NOT listed as files", async () => {
  // The core regression test: a directory like images/brain-gbrain.context/
  // must not be added to the set as a file.
  const dir = await makeTempDir();
  try {
    await Deno.mkdir(join(dir, "images", "brain-gbrain.context"), { recursive: true });
    await Deno.writeTextFile(
      join(dir, "images", "brain-gbrain.context", "entrypoint.sh"),
      "#!/bin/bash\nexec python main.py\n"
    );

    const result = await listExistingFiles(dir);
    // The directory itself must not be in the set
    assertEquals(result.has("images/brain-gbrain.context"), false);
    // But the file inside it should be
    assertEquals(result.has("images/brain-gbrain.context/entrypoint.sh"), true);
    assertEquals(result.size, 1);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("listExistingFiles: all quadlet extensions are recognized", async () => {
  const dir = await makeTempDir();
  try {
    for (const ext of QUADLET_EXTENSIONS) {
      await Deno.writeTextFile(join(dir, `test${ext}`), "");
    }
    const result = await listExistingFiles(dir);
    assertEquals(result.size, QUADLET_EXTENSIONS.size);
    for (const ext of QUADLET_EXTENSIONS) {
      assertEquals(result.has(`test${ext}`), true, `missing test${ext}`);
    }
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- computePlan ---

Deno.test("computePlan: empty dir produces all adds", async () => {
  const dir = await makeTempDir();
  try {
    const files: Record<string, FileValue> = {
      "app.container": "[Container]\nImage=nginx\n",
      "net.network": "[Network]\n",
    };
    const changes = await computePlan(files, dir);
    const adds = changes.filter(c => c.action === "add");
    assertEquals(adds.length, 2);
    assertEquals(changes.filter(c => c.action === "remove").length, 0);
    assertEquals(changes.filter(c => c.action === "unchanged").length, 0);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("computePlan: matching files are unchanged", async () => {
  const dir = await makeTempDir();
  try {
    const content = "[Container]\nImage=nginx\n";
    await Deno.writeTextFile(join(dir, "app.container"), content);

    const files: Record<string, FileValue> = { "app.container": content };
    const changes = await computePlan(files, dir);
    assertEquals(changes.length, 1);
    assertEquals(changes[0].action, "unchanged");
    assertEquals(changes[0].name, "app.container");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("computePlan: differing content produces change", async () => {
  const dir = await makeTempDir();
  try {
    await Deno.writeTextFile(join(dir, "app.container"), "old content");

    const files: Record<string, FileValue> = { "app.container": "new content" };
    const changes = await computePlan(files, dir);
    assertEquals(changes.length, 1);
    assertEquals(changes[0].action, "change");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("computePlan: extra files on disk are marked for removal", async () => {
  const dir = await makeTempDir();
  try {
    await Deno.writeTextFile(join(dir, "app.container"), "[Container]\n");
    await Deno.writeTextFile(join(dir, "stale.volume"), "[Volume]\n");

    const files: Record<string, FileValue> = { "app.container": "[Container]\n" };
    const changes = await computePlan(files, dir);
    const removes = changes.filter(c => c.action === "remove");
    assertEquals(removes.length, 1);
    assertEquals(removes[0].name, "stale.volume");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("computePlan: directories on disk do NOT produce spurious removals", async () => {
  // Regression: if listExistingFiles incorrectly reports directories as files,
  // computePlan will mark them for removal, and apply will rm -rf them.
  const dir = await makeTempDir();
  try {
    await Deno.writeTextFile(join(dir, "app.container"), "[Container]\n");
    await Deno.mkdir(join(dir, "images", "app.context"), { recursive: true });
    await Deno.writeTextFile(join(dir, "images", "app.context", "run.sh"), "#!/bin/bash\n");

    const files: Record<string, FileValue> = {
      "app.container": "[Container]\n",
      "images/app.context/run.sh": { content: "#!/bin/bash\n", mode: "0755" },
    };
    const changes = await computePlan(files, dir);
    const removes = changes.filter(c => c.action === "remove");
    assertEquals(removes.length, 0, `Unexpected removals: ${JSON.stringify(removes)}`);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("computePlan: handles FileValue with mode", async () => {
  const dir = await makeTempDir();
  try {
    const files: Record<string, FileValue> = {
      "images/app.context/run.sh": { content: "#!/bin/bash\n", mode: "0755" },
    };
    const changes = await computePlan(files, dir);
    assertEquals(changes.length, 1);
    assertEquals(changes[0].action, "add");
    if (changes[0].action === "add") {
      assertEquals(changes[0].mode, "0755");
      assertEquals(changes[0].content, "#!/bin/bash\n");
    }
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- writeFile + removeFile (non-elevated only in tests) ---

Deno.test("writeFile: creates file and parent directories", async () => {
  const dir = await makeTempDir();
  try {
    const path = join(dir, "images", "app.context", "scripts", "run.sh");
    await writeFile(path, "#!/bin/bash\n", false);
    const content = await Deno.readTextFile(path);
    assertEquals(content, "#!/bin/bash\n");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("writeFile: sets file mode", async () => {
  const dir = await makeTempDir();
  try {
    const path = join(dir, "run.sh");
    await writeFile(path, "#!/bin/bash\n", false, "0755");
    const stat = await Deno.stat(path);
    // Check executable bit is set (mode & 0o111 !== 0)
    assertEquals((stat.mode! & 0o111) !== 0, true, "File should be executable");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("writeFile: does NOT rm -rf an existing directory", async () => {
  // Regression: ensureDir must not treat directories as blocking files.
  // If ensureDir sees an existing directory and calls removeFile on it,
  // that's the catastrophic bug.
  const dir = await makeTempDir();
  try {
    // Pre-create the directory structure with existing content
    await Deno.mkdir(join(dir, "images", "app.context"), { recursive: true });
    await Deno.writeTextFile(join(dir, "images", "app.context", "existing.conf"), "keep me");

    // Write a new file into that same directory tree
    const path = join(dir, "images", "app.context", "new-file.sh");
    await writeFile(path, "#!/bin/bash\n", false);

    // The existing file must still be there
    const existing = await Deno.readTextFile(join(dir, "images", "app.context", "existing.conf"));
    assertEquals(existing, "keep me");
    // And the new file should exist
    const created = await Deno.readTextFile(path);
    assertEquals(created, "#!/bin/bash\n");
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("writeFile: handles file-to-directory transition", async () => {
  // If a flat file exists where we now need a directory, ensureDir should
  // remove the file and create the directory.
  const dir = await makeTempDir();
  try {
    // Create a flat file at "images"
    await Deno.writeTextFile(join(dir, "images"), "stale flat file");

    // Now write into images/app.Containerfile - this requires images/ to be a directory
    const path = join(dir, "images", "app.Containerfile");
    await writeFile(path, "FROM node:20\n", false);

    const content = await Deno.readTextFile(path);
    assertEquals(content, "FROM node:20\n");
    // images should now be a directory
    const stat = await Deno.stat(join(dir, "images"));
    assertEquals(stat.isDirectory, true);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("removeFile: removes files", async () => {
  const dir = await makeTempDir();
  try {
    const path = join(dir, "test.container");
    await Deno.writeTextFile(path, "content");
    await removeFile(path, false);
    const exists = await Deno.stat(path).then(() => true).catch(() => false);
    assertEquals(exists, false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("removeFile: removes directories recursively", async () => {
  const dir = await makeTempDir();
  try {
    await Deno.mkdir(join(dir, "sub", "deep"), { recursive: true });
    await Deno.writeTextFile(join(dir, "sub", "deep", "file.txt"), "hi");
    await removeFile(join(dir, "sub"), false);
    const exists = await Deno.stat(join(dir, "sub")).then(() => true).catch(() => false);
    assertEquals(exists, false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

// --- pruneEmptyDirs ---

Deno.test("pruneEmptyDirs: removes empty subdirectories", async () => {
  const dir = await makeTempDir();
  try {
    await Deno.mkdir(join(dir, "a", "b", "c"), { recursive: true });
    await pruneEmptyDirs(dir, false);
    // All subdirs should be gone since they're empty
    const exists = await Deno.stat(join(dir, "a")).then(() => true).catch(() => false);
    assertEquals(exists, false);
    // Root dir itself should still exist
    const rootExists = await Deno.stat(dir).then(() => true).catch(() => false);
    assertEquals(rootExists, true);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("pruneEmptyDirs: preserves directories with files", async () => {
  const dir = await makeTempDir();
  try {
    await Deno.mkdir(join(dir, "keep"), { recursive: true });
    await Deno.writeTextFile(join(dir, "keep", "file.txt"), "hi");
    await Deno.mkdir(join(dir, "remove"), { recursive: true });

    await pruneEmptyDirs(dir, false);

    const keepExists = await Deno.stat(join(dir, "keep")).then(() => true).catch(() => false);
    assertEquals(keepExists, true);
    const removeExists = await Deno.stat(join(dir, "remove")).then(() => true).catch(() => false);
    assertEquals(removeExists, false);
  } finally {
    await Deno.remove(dir, { recursive: true });
  }
});

Deno.test("pruneEmptyDirs: does not crash on nonexistent dir", async () => {
  await pruneEmptyDirs("/tmp/does-not-exist-" + Date.now(), false);
  // Should not throw
});

// --- normalizeFileValue ---

Deno.test("normalizeFileValue: string passthrough", () => {
  const result = normalizeFileValue("hello");
  assertEquals(result, { content: "hello" });
});

Deno.test("normalizeFileValue: object with mode", () => {
  const result = normalizeFileValue({ content: "#!/bin/bash\n", mode: "0755" });
  assertEquals(result, { content: "#!/bin/bash\n", mode: "0755" });
});

// --- planSummary ---

Deno.test("planSummary: counts correctly", () => {
  const changes: FileChange[] = [
    { action: "add", name: "a.container", content: "" },
    { action: "add", name: "b.container", content: "" },
    { action: "change", name: "c.container", content: "", existing: "" },
    { action: "unchanged", name: "d.container" },
    { action: "unchanged", name: "e.container" },
    { action: "unchanged", name: "f.container" },
    { action: "remove", name: "g.container" },
  ];
  const s = planSummary(changes);
  assertEquals(s.added, 2);
  assertEquals(s.changed, 1);
  assertEquals(s.unchanged, 3);
  assertEquals(s.removed, 1);
  assertEquals(s.total, 6); // total = add + change + unchanged (not removed)
});
