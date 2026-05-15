// Shared helpers for deploy tasks

import $ from "jsr:@david/dax";
import { red, green, yellow, dim } from "jsr:@std/fmt/colors";

export { $, red, green, yellow, dim };

export const QUADLET_EXTENSIONS = new Set([
  ".container", ".pod", ".volume", ".network",
  ".kube", ".build", ".image", ".artifact",
]);

// File values in the output map are either plain strings (quadlet files,
// Containerfiles) or structured objects with content and mode (context files).
export type FileValue = string | { content: string; mode: string };

// Normalize a file value to content string and optional mode.
export function normalizeFileValue(val: FileValue): { content: string; mode?: string } {
  if (typeof val === "string") return { content: val };
  return { content: val.content, mode: val.mode };
}

// Categorized file change for plan/diff/apply
export type FileChange =
  | { action: "add"; name: string; content: string; mode?: string }
  | { action: "change"; name: string; content: string; existing: string; mode?: string }
  | { action: "unchanged"; name: string }
  | { action: "remove"; name: string };

// Compare generated files against what's on disk.
// Returns a sorted list of changes.
export async function computePlan(
  files: Record<string, FileValue>,
  dir: string,
): Promise<FileChange[]> {
  const changes: FileChange[] = [];
  const expected = new Set(Object.keys(files));
  const target = $.path(dir);

  for (const fname of [...expected].sort()) {
    const { content, mode } = normalizeFileValue(files[fname]);
    const dest = target.join(fname);
    if (!(await dest.exists())) {
      changes.push({ action: "add", name: fname, content, mode });
    } else {
      const existing = await dest.readText();
      if (existing !== content) {
        changes.push({ action: "change", name: fname, content, existing, mode });
      } else {
        changes.push({ action: "unchanged", name: fname });
      }
    }
  }

  const onDisk = await listExistingFiles(dir);
  for (const name of [...onDisk].sort()) {
    if (expected.has(name)) continue;
    changes.push({ action: "remove", name });
  }

  return changes;
}

// Summary counts from a plan
export function planSummary(changes: FileChange[]): { added: number; changed: number; unchanged: number; removed: number; total: number } {
  let added = 0, changed = 0, unchanged = 0, removed = 0;
  for (const c of changes) {
    switch (c.action) {
      case "add": added++; break;
      case "change": changed++; break;
      case "unchanged": unchanged++; break;
      case "remove": removed++; break;
    }
  }
  return { added, changed, unchanged, removed, total: added + changed + unchanged };
}

// Export CUE files as JSON map of filename -> content or {content, mode}
// Walks all top-level values and merges their output.files maps.
export async function getFiles(): Promise<Record<string, FileValue>> {
  const root = await $`cue export ./... --out json`.json();

  const files: Record<string, FileValue> = {};
  for (const val of Object.values(root)) {
    const outputFiles = (val as Record<string, unknown>)?.output
      ?.files as Record<string, FileValue> | undefined;
    if (outputFiles) {
      Object.assign(files, outputFiles);
    }
  }

  if (Object.keys(files).length === 0) {
    $.logError("no quadlet files found (do your quadlets have output.files?)");
    Deno.exit(1);
  }
  return files;
}

// Get QUADLET_DIR from environment or exit
export function getQuadletDir(): string {
  const dir = Deno.env.get("QUADLET_DIR");
  if (!dir) {
    $.logError("QUADLET_DIR not set (configure in .mise/config.toml)");
    Deno.exit(1);
  }
  return dir;
}

// List existing files in the quadlet directory.
// Walks images/ subdirectory for Containerfiles and context files.
export async function listExistingFiles(dir: string): Promise<Set<string>> {
  const existing = new Set<string>();
  const root = $.path(dir);
  if (!(await root.exists())) return existing;

  // Flat quadlet files
  for await (const entry of root.readDir()) {
    if (!entry.isFile) continue;
    const ext = entry.name.substring(entry.name.lastIndexOf("."));
    if (QUADLET_EXTENSIONS.has(ext)) {
      existing.add(entry.name);
    }
  }

  // images/ subdirectory (Containerfiles and context files)
  const imagesDir = root.join("images");
  if (await imagesDir.exists()) {
    async function walkDir(base: typeof imagesDir, prefix: string) {
      for await (const entry of base.readDir()) {
        const relPath = prefix ? `${prefix}/${entry.name}` : entry.name;
        if (entry.isFile) {
          existing.add(`images/${relPath}`);
        } else if (entry.isDirectory) {
          await walkDir(base.join(entry.name), relPath);
        }
      }
    }
    await walkDir(imagesDir, "");
  }

  return existing;
}

// Check if the quadlet directory requires elevated permissions.
// Returns true if the directory exists and is not writable by the current user.
export async function needsElevation(dir: string): Promise<boolean> {
  const exists = await $.path(dir).exists();
  if (!exists) return false;
  return !(await isWritable(dir));
}

// Check if a directory is writable by the current user.
export async function isWritable(dir: string): Promise<boolean> {
  try {
    const tmp = $.path(dir).join(`.write-test-${Date.now()}`);
    await tmp.writeText("");
    await tmp.remove();
    return true;
  } catch {
    return false;
  }
}

// Ensure a directory exists, removing any file that blocks the path.
// Handles file-to-directory transitions (e.g. a stale flat file where a
// subdirectory is now needed).
async function ensureDir(dir: string, elevated: boolean): Promise<void> {
  const p = $.path(dir);
  if (await p.exists()) {
    if ((await p.stat())?.isFile) {
      // Stale file blocking a directory path. Remove it first.
      await removeFile(dir, elevated);
      await mkdirp(dir, elevated);
    }
    return;
  }
  // Walk upward to clear any file blocking an ancestor.
  const parentStr = p.parent()?.toString();
  if (parentStr) {
    await ensureDir(parentStr, elevated);
  }
  await mkdirp(dir, elevated);
}

// Write a file, using sudo if needsElevation is true.
// Creates parent directories if needed. Sets file mode if specified.
export async function writeFile(path: string, content: string, elevated: boolean, mode?: string): Promise<void> {
  const parent = $.path(path).parent()!.toString();
  await ensureDir(parent, elevated);
  if (elevated) {
    const tmp = await Deno.makeTempFile();
    try {
      await Deno.writeTextFile(tmp, content);
      if (mode) await $`chmod ${mode} ${tmp}`;
      await $`sudo cp -p ${tmp} ${path}`;
    } finally {
      await Deno.remove(tmp);
    }
  } else {
    await $.path(path).writeText(content);
    if (mode) await $`chmod ${mode} ${path}`;
  }
}

// Remove a file or directory, using sudo if needsElevation is true.
export async function removeFile(path: string, elevated: boolean): Promise<void> {
  if (elevated) {
    await $`sudo rm -rf ${path}`;
  } else {
    await $.path(path).remove({ recursive: true });
  }
}

// Create a directory, using sudo if needsElevation is true.
export async function mkdirp(dir: string, elevated: boolean): Promise<void> {
  if (elevated) {
    await $`sudo mkdir -p ${dir}`;
  } else {
    await $.path(dir).mkdir({ recursive: true });
  }
}

// Remove empty directories under a root path, walking bottom-up.
// Useful after file removals to clean up stale directory structure.
export async function pruneEmptyDirs(dir: string, elevated: boolean): Promise<void> {
  const root = $.path(dir);
  if (!(await root.exists())) return;

  async function walk(p: typeof root): Promise<void> {
    if (!(await p.stat())?.isDirectory) return;
    for await (const entry of p.readDir()) {
      if (entry.isDirectory) {
        await walk(p.join(entry.name));
      }
    }
    // After recursing into children, check if this dir is now empty.
    // Never remove the root itself.
    if (p.toString() === root.toString()) return;
    let empty = true;
    for await (const _ of p.readDir()) {
      empty = false;
      break;
    }
    if (empty) {
      if (elevated) {
        await $`sudo rmdir ${p.toString()}`;
      } else {
        await p.remove();
      }
    }
  }
  await walk(root);
}

// Run a diff tool (configurable via DIFF_TOOL env, defaults to "diff")
export async function runDiff(
  livePath: string,
  newContent: string,
  liveLabel: string,
  newLabel: string,
): Promise<string> {
  const tool = Deno.env.get("DIFF_TOOL") ?? "diff";

  if (tool === "diff") {
    return await $`diff --color=always -u --label ${liveLabel} --label ${newLabel} ${livePath} -`
      .stdin(newContent).noThrow().text();
  }

  // For tools like delta that read files, write to a temp file
  const tmp = $.path(await Deno.makeTempFile());
  try {
    await tmp.writeText(newContent);
    return await $`${tool} ${livePath} ${tmp}`.noThrow().text();
  } finally {
    await tmp.remove();
  }
}
