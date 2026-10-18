import { test, expect, type Page } from "@playwright/test";
const env = {
  code: "rdev.ali",
  groupCode: "raptor",
  stage: "dev",
  cloud: "aliyun",
  region: "ap-southeast-1",
  accountId: "fixture-account",
  ackClusterId: "fixture-cluster",
  repositories: [
    {
      purpose: "terraform",
      url: "https://github.com/example/infra",
      baseBranch: "main",
      directory: "infra",
    },
  ],
};
async function fixture(page: Page, role = "admin") {
  const writes: any[] = [];
  let signedIn = true;
  let environments = [structuredClone(env)];
  let pat = {
    configured: false,
    owner: "fixture-owner",
    verifiedAt: "2026-10-18T00:00:00Z",
  };
  const provider = {
    providerId: "codex",
    status: "connected",
    accountLabel: "fixture-account",
    models: [
      {
        modelId: "fixture-model",
        displayName: "Fixture model",
        available: true,
      },
    ],
    enabled: ["fixture-model"],
    defaultModelId: "fixture-model",
    policyVersion: 7,
  };
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname.replace("/api/v1/", ""),
      method = request.method();
    let data: any = {};
    let status = 200;
    if (!["GET", "HEAD"].includes(method) && path !== "login") {
      expect(request.headers()["x-csrf-token"]).toBe("fixture-csrf");
      writes.push({ path, method, body: request.postDataJSON() });
    }
    if (path === "session") {
      if (!signedIn) status = 401;
      else
        data = {
          csrf: "fixture-csrf",
          user: { username: "fixture-owner", role },
        };
    } else if (path === "login") {
      signedIn = true;
      data = { csrf: "fixture-csrf" };
    } else if (path === "logout") signedIn = false;
    else if (path === "users")
      data = [{ username: "fixture-owner", role: "admin" }];
    else if (path === "environment-groups")
      data = [{ code: "raptor", name: "Raptor" }];
    else if (path === "environments") {
      if (method === "POST") environments.push(request.postDataJSON());
      data = environments;
    } else if (path.startsWith("environments/")) {
      if (method === "PATCH") environments[0] = request.postDataJSON();
      data =
        environments.find(
          (e) => e.code === decodeURIComponent(path.split("/")[1]),
        ) || env;
    } else if (path === "admin/github-pat") {
      if (method === "PUT") pat.configured = true;
      if (method === "DELETE") pat.configured = false;
      data = pat;
    } else if (path === "admin/model-providers")
      data = { providers: [provider] };
    else if (path.endsWith("/models")) {
      const body = request.postDataJSON();
      expect(body.expectedVersion).toBe(7);
      provider.enabled = body.enabled;
      provider.defaultModelId = body.defaultModelId;
    } else if (path.endsWith("/browser-connect"))
      data = {
        sessionId: "fixture-connect",
        authorizationUrl: "https://auth.openai.com/fixture",
        expiresAt: "2026-10-18T12:00:00Z",
      };
    else if (path.endsWith("/connect") && method === "POST")
      data = {
        sessionId: "fixture-connect",
        verificationUrl: "https://auth.openai.com/fixture",
        userCode: "ABCD-EFGH",
        expiresAt: "2026-10-18T12:00:00Z",
      };
    else if (path.includes("/connect/")) data = { status: "pending" };
    await route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify(
        status === 401 ? { error: { code: "Unauthorized" } } : { data },
      ),
    });
  });
  return writes;
}
test("lists and creates users; signs out and back in with cookie session", async ({
  page,
}) => {
  const writes = await fixture(page);
  await page.goto("/users");
  await expect(
    page.getByRole("heading", { name: "Users", exact: true }),
  ).toBeVisible();
  await page.getByLabel("Username", { exact: true }).fill("new-fixture");
  await page
    .getByLabel("Password", { exact: true })
    .fill("fixture-password-123");
  await page.getByRole("button", { name: "Create user", exact: true }).click();
  await expect(page.getByText("User created.", { exact: true })).toBeVisible();
  expect(writes[0].body.username).toBe("new-fixture");
  await expect(page.getByLabel("Password", { exact: true })).toHaveValue("");
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByText("Administrator sign in")).toBeVisible();
  await page.getByLabel("Username").fill("fixture-owner");
  await page.getByLabel("Password").fill("fixture-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Users", exact: true }),
  ).toBeVisible();
});
test("denies ordinary users before fetching management records", async ({
  page,
}) => {
  await fixture(page, "user");
  await page.goto("/users");
  await expect(page.getByText("Administrator access required")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Users", exact: true }),
  ).toHaveCount(0);
});
test("creates environment groups", async ({ page }) => {
  const writes = await fixture(page);
  await page.goto("/environment-groups");
  await page.getByLabel("Group code").fill("test");
  await page.getByLabel("Group name").fill("Test group");
  await page.getByRole("button", { name: "Create group", exact: true }).click();
  await expect(page.getByText("Group created.", { exact: true })).toBeVisible();
  expect(writes[0].body).toEqual({ code: "test", name: "Test group" });
});
test("searches, loads and edits structured environment repositories", async ({
  page,
}) => {
  const writes = await fixture(page);
  await page.goto("/environments");
  await page.getByLabel("Search environments").fill("missing");
  await expect(page.getByText("No matching environments.")).toBeVisible();
  await page.getByLabel("Search environments").fill("rdev");
  await page.getByRole("link", { name: "rdev.ali", exact: true }).click();
  await expect(page.getByText("fixture-cluster")).toBeVisible();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await page.getByLabel("Region", { exact: true }).fill("cn-hangzhou");
  await page.getByLabel("Directory", { exact: true }).fill("infra/terraform");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText("cn-hangzhou")).toBeVisible();
  expect(writes[0].body.repositories[0].directory).toBe("infra/terraform");
});
test("creates an environment with structured repository rows", async ({
  page,
}) => {
  const writes = await fixture(page);
  await page.goto("/environments?mode=create");
  await page.getByLabel("Code", { exact: true }).fill("staging");
  await page.getByLabel("Group code", { exact: true }).fill("raptor");
  await page.getByLabel("Stage", { exact: true }).fill("staging");
  await page.getByRole("button", { name: "Add repository" }).click();
  await page
    .getByLabel("HTTPS GitHub URL")
    .fill("https://github.com/example/staging");
  await page
    .getByRole("button", { name: "Create environment", exact: true })
    .click();
  await expect(page).toHaveURL(/\/environments\/staging$/);
  expect(writes[0].body.repositories).toEqual([
    {
      purpose: "terraform",
      url: "https://github.com/example/staging",
      baseBranch: "main",
      directory: ".",
    },
  ]);
});
test("saves and deletes the GitHub token without redisplaying it", async ({
  page,
}) => {
  const writes = await fixture(page);
  await page.goto("/github-pat");
  await page
    .getByLabel("Personal access token", { exact: true })
    .fill("fixture-secret");
  await page.getByRole("button", { name: "Save or replace" }).click();
  await expect(page.getByText("Token saved.", { exact: true })).toBeVisible();
  await expect(
    page.getByLabel("Personal access token", { exact: true }),
  ).toHaveValue("");
  await page.getByRole("button", { name: "Delete token" }).click();
  await expect(page.getByText("Token deleted.", { exact: true })).toBeVisible();
  expect(writes.map((w) => w.method)).toEqual(["PUT", "DELETE"]);
});
test("edits versioned model policy and cancels browser/device connections", async ({
  page,
}) => {
  const writes = await fixture(page);
  await page.goto("/ai-models");
  await page.getByRole("button", { name: "Save enabled models" }).click();
  await expect(
    page.getByText("Model settings saved.", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Connect in browser", exact: true })
    .click();
  await page
    .getByLabel("Final localhost redirect URL")
    .fill("http://localhost:1455/auth/callback?code=fixture");
  await page.getByRole("button", { name: "Complete sign-in" }).click();
  await expect(page.getByLabel("Final localhost redirect URL")).toHaveValue("");
  await page.getByRole("button", { name: "Cancel sign-in" }).click();
  await page.getByRole("button", { name: "Connect with device code" }).click();
  await expect(page.getByText("ABCD-EFGH")).toBeVisible();
  await page.getByRole("button", { name: "Cancel sign-in" }).click();
  expect(writes.some((w) => w.path.endsWith("/complete"))).toBeTruthy();
});
test("stops polling when leaving AI models and preserves legacy hashes", async ({
  page,
}) => {
  const writes = await fixture(page);
  await page.goto("/ai-models");
  await page.getByRole("button", { name: "Connect with device code" }).click();
  let polls = 0;
  page.on("request", (r) => {
    if (r.url().includes("/connect/fixture-connect") && r.method() === "GET")
      polls++;
  });
  await page.getByRole("link", { name: "Users", exact: true }).click();
  await page.waitForTimeout(2400);
  expect(polls).toBe(0);
  await page.goto("/#groups-section");
  await expect(page).toHaveURL(/environment-groups$/);
  expect(writes.length).toBe(1);
});

test("clears the token even when the backend rejects it", async ({ page }) => {
  await fixture(page);
  await page.goto("/github-pat");
  await page.route("**/api/v1/admin/github-pat", async (route) => {
    if (route.request().method() === "PUT")
      await route.fulfill({
        status: 422,
        contentType: "application/json",
        body: JSON.stringify({ error: { code: "InvalidToken" } }),
      });
    else await route.fallback();
  });
  await page
    .getByLabel("Personal access token", { exact: true })
    .fill("fixture-rejected-secret");
  await page.getByRole("button", { name: "Save or replace" }).click();
  await expect(page.getByText("InvalidToken", { exact: true })).toBeVisible();
  await expect(
    page.getByLabel("Personal access token", { exact: true }),
  ).toHaveValue("");
});
test("expired sessions return to sign-in instead of exposing management pages", async ({
  page,
}) => {
  await fixture(page);
  await page.goto("/users");
  await expect(
    page.getByRole("heading", { name: "Users", exact: true }),
  ).toBeVisible();
  await page.route("**/api/v1/environment-groups", (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ error: { code: "Unauthenticated" } }),
    }),
  );
  await page
    .getByRole("link", { name: "Environment groups", exact: true })
    .click();
  await expect(page.getByText("Administrator sign in")).toBeVisible();
});
test("a completed save cannot pull the administrator back after navigating away", async ({
  page,
}) => {
  await fixture(page);
  let pending: any;
  await page.route("**/api/v1/environments/rdev.ali", async (route) => {
    if (route.request().method() === "PATCH") {
      pending = route;
      return;
    }
    await route.fallback();
  });
  await page.goto("/environments/rdev.ali/edit");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect.poll(() => !!pending).toBe(true);
  await page.getByRole("link", { name: "Users", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Users", exact: true }),
  ).toBeVisible();
  await pending.fulfill({
    contentType: "application/json",
    body: JSON.stringify({ data: env }),
  });
  await page.waitForTimeout(300);
  await expect(page).toHaveURL(/\/users$/);
});

test("renders environment management at desktop and mobile widths", async ({
  page,
}) => {
  await fixture(page);
  await page.goto("/environments/rdev.ali");
  await expect(page.getByText("fixture-cluster")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Edit", exact: true }),
  ).toBeVisible();
  if (process.env.RAPTOR_SCREENSHOT_DIR)
    await page.screenshot({
      path: process.env.RAPTOR_SCREENSHOT_DIR + "/raptor-admin-desktop.png",
      fullPage: true,
    });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(
    page.getByRole("button", { name: "Edit", exact: true }),
  ).toBeInViewport();
  expect(
    await page.locator("body").evaluate((el) => el.scrollWidth),
  ).toBeLessThanOrEqual(390);
  expect(
    await page
      .locator(".page-content")
      .evaluate((el) => el.getBoundingClientRect().width),
  ).toBeGreaterThanOrEqual(350);
  if (process.env.RAPTOR_SCREENSHOT_DIR)
    await page.screenshot({
      path: process.env.RAPTOR_SCREENSHOT_DIR + "/raptor-admin-mobile.png",
      fullPage: true,
    });
  await page.locator('[data-layout-action="toggle-sidebar"]').click();
  await page.getByRole("link", { name: "Users", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Users", exact: true }),
  ).toBeVisible();
});
