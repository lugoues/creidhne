// Shared helpers for deploy tasks

import $ from "jsr:@david/dax";
import { red, green, yellow, dim } from "jsr:@std/fmt/colors";

export { $, red, green, yellow, dim };

export const QUADLET_EXTENSIONS = new Set([
  ".container", ".pod", ".volume", ".network",
  ".kube", ".build", ".image", ".artifact",
]);

// Categorized file change for plan/diff/apply
export type FileChange =
  | { action: "add"; name: string; content: string }
  | { action: "change"; name: string; content: string; existing: string }
  | { action: "unchanged"; name: string }
  | { action: "remove"; name: string };

// Compare generated files against what's on disk.
// Returns a sorted list of changes.
export async function computePlan(
  files: Record<string, string>,
  dir: string,
): Promise<FileChange[]> {
  const changes: FileChange[] = [];
  const expected = new Set(Object.keys(files));
  const target = $.path(dir);

  for (const fname of [...expected].sort()) {
    const dest = target.join(fname);
    if (!(await dest.exists())) {
      changes.push({ action: "add", name: fname, content: files[fname] });
    } else {
      const existing = await dest.readText();
      if (existing !== files[fname]) {
        changes.push({ action: "change", name: fname, content: files[fname], existing });
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

// Export CUE files as JSON map of filename -> content
// Walks all top-level values and merges their output.files maps.
export async function getFiles(): Promise<Record<string, string>> {
  const root = await $`cue export ./... --out json`.json();

  const files: Record<string, string> = {};
  for (const val of Object.values(root)) {
    const outputFiles = (val as Record<string, unknown>)?.output
      ?.files as Record<string, string> | undefined;
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

// List existing quadlet files in a directory
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

  // images/ subdirectory
  const imagesDir = root.join("images");
  if (await imagesDir.exists()) {
    for await (const entry of imagesDir.readDir()) {
      if (!entry.isFile) continue;
      if (entry.name.endsWith(".Containerfile")) {
        existing.add(`images/${entry.name}`);
      }
    }
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

// Write a file, using sudo if needsElevation is true.
export async function writeFile(path: string, content: string, elevated: boolean): Promise<void> {
  const parent = $.path(path).parent()!;
  if (!(await parent.exists())) {
    if (elevated) {
      await $`sudo mkdir -p ${parent.toString()}`;
    } else {
      await parent.mkdir({ recursive: true });
    }
  }
  if (elevated) {
    const tmp = await Deno.makeTempFile();
    try {
      await Deno.writeTextFile(tmp, content);
      await $`sudo cp ${tmp} ${path}`;
    } finally {
      await Deno.remove(tmp);
    }
  } else {
    await $.path(path).writeText(content);
  }
}

// Remove a file, using sudo if needsElevation is true.
export async function removeFile(path: string, elevated: boolean): Promise<void> {
  if (elevated) {
    await $`sudo rm ${path}`;
  } else {
    await $.path(path).remove();
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

