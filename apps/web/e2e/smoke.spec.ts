import { test, expect } from "@playwright/test";

// The one true end-to-end smoke test (issue #108): the login -> project ->
// task path is FlowLens's most-used, most-breakable path, and it is the only
// one covered here. GitLab connection is deliberately out of scope — see
// docs/testing.md's E2E section for what belongs in this file and what
// doesn't.
test("signup, create project, create task, then log out", async ({ page }) => {
  const runId = Date.now();
  const username = `e2e${runId}`;
  const projectName = `E2E project ${runId}`;
  const taskTitle = `E2E task ${runId}`;

  await page.goto("/signup");
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Email").fill(`${username}@example.com`);
  await page.getByLabel("Password").fill("hunter22");
  await page.getByRole("button", { name: "Create account" }).click();

  await expect(page).toHaveURL(/\/dashboard$/);

  await page.goto("/projects");
  await page.getByRole("button", { name: "New project" }).click();
  await page.getByLabel("Name").fill(projectName);
  await page.getByRole("button", { name: "Create project" }).click();

  await expect(page.getByRole("link", { name: new RegExp(projectName) })).toBeVisible();
  await page.getByRole("link", { name: new RegExp(projectName) }).click();

  // Scoped to the project sidebar: the app header also has a global "Tasks"
  // link, and an unscoped lookup would match both.
  await page
    .getByRole("navigation", { name: "Project sections" })
    .getByRole("link", { name: "Tasks" })
    .click();
  await expect(page).toHaveURL(/\/tasks$/);

  await page.getByRole("button", { name: "New task" }).click();
  await page.getByLabel("Title").fill(taskTitle);
  await page.getByRole("button", { name: "Create task" }).click();

  await expect(page.getByText(taskTitle)).toBeVisible();

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
});
