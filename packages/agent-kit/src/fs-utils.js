import { existsSync, readFileSync } from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname } from "node:path";

/**
 * Writes `content` to `path`, creating parent directories as needed.
 *
 * - `alwaysOverwrite: true` (generated artifacts like the spec/config) always writes.
 * - Otherwise an existing file is left untouched unless `force` is set.
 *
 * Returns "created" | "overwritten" | "skipped".
 */
export async function writeFileGuarded(path, content, { force = false, alwaysOverwrite = false } = {}) {
  const existed = existsSync(path);
  if (existed && !force && !alwaysOverwrite) {
    return "skipped";
  }
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, content, "utf8");
  return existed ? "overwritten" : "created";
}

/**
 * Removes an exact-match `.flowlens/` ignore line from the repo's
 * .gitignore, left over from an older agent-kit that gitignored it. Only
 * an exact-match line is touched — a broader pattern like `.flowlens*` is
 * left alone, since that's the repo owner's own to manage.
 */
export async function removeGitignoreEntry(gitignorePath, entry) {
  if (!existsSync(gitignorePath)) {
    return "skipped";
  }
  const current = readFileSync(gitignorePath, "utf8");
  const lines = current.split("\n");
  if (!lines.some((line) => line.trim() === entry)) {
    return "skipped";
  }
  const filtered = lines.filter((line) => line.trim() !== entry);
  await writeFile(gitignorePath, filtered.join("\n"), "utf8");
  return "removed";
}
