import { afterEach, describe, expect, it, vi } from "vitest";
import { api, clearSession, restoreSession } from "../src/api";
afterEach(() => {
  clearSession();
  vi.unstubAllGlobals();
});
const reply = (data: unknown, status = 200) =>
  new Response(
    JSON.stringify(status < 400 ? { data } : { error: { code: data } }),
    { status },
  );
describe("Admin API session boundary", () => {
  it("uses cookie sessions and sends CSRF for writes without persisting secrets", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        reply({
          csrf: "csrf-fixture",
          user: { username: "owner", role: "admin" },
        }),
      )
      .mockResolvedValueOnce(reply({}));
    vi.stubGlobal("fetch", fetcher);
    await restoreSession();
    await api("users", "POST", { username: "fixture" });
    const options = fetcher.mock.calls[1][1];
    expect(options.credentials).toBe("same-origin");
    expect(options.headers["X-CSRF-Token"]).toBe("csrf-fixture");
  });
  it("rejects ordinary users and clears the previous token", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(reply({ csrf: "private", user: { role: "user" } })),
    );
    await expect(restoreSession()).rejects.toThrow(
      "Administrator access required",
    );
    await expect(api("users", "POST", {})).rejects.toThrow("Sign in required");
  });
  it("clears an expired session and signals sign-in", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(reply({ csrf: "old", user: { role: "admin" } }))
      .mockResolvedValueOnce(reply("Unauthorized", 401));
    vi.stubGlobal("fetch", fetcher);
    await restoreSession();
    await expect(api("users")).rejects.toThrow("Sign in required");
    await expect(api("users", "POST", {})).rejects.toThrow("Sign in required");
  });
  it("retains actionable backend errors such as policy version conflicts", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(reply("VersionConflict", 409)),
    );
    await expect(api("admin/model-providers")).rejects.toThrow(
      "VersionConflict",
    );
  });
});
