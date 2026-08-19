import { existsSync, readFileSync } from "node:fs";
import { mkdir, writeFile, appendFile } from "node:fs/promises";
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
 * Appends a `.flowlens/` ignore entry to the repo's .gitignore if it isn't
 * already covered by an existing line (exact match, since a broader
 * pattern like `.flowlens*` is for the repo owner to write themselves).
 */
export async function ensureGitignoreEntry(gitignorePath, entry) {
  if (!existsSync(gitignorePath)) {
    await writeFile(gitignorePath, `${entry}\n`, "utf8");
    return "created";
  }
  const current = readFileSync(gitignorePath, "utf8");
  const alreadyPresent = current.split("\n").some((line) => line.trim() === entry);
  if (alreadyPresent) {
    return "skipped";
  }
  const separator = current.endsWith("\n") || current.length === 0 ? "" : "\n";
  await appendFile(gitignorePath, `${separator}${entry}\n`, "utf8");
  return "appended";
}
