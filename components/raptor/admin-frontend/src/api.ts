import { shallowRef } from "vue";
export interface User {
  username: string;
  role: string;
}
export const currentUser = shallowRef<User | null>(null);
let csrf: string | undefined;
export function clearSession() {
  csrf = undefined;
  currentUser.value = null;
}
export async function api<T = any>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  if (!["GET", "HEAD"].includes(method) && path !== "login" && !csrf)
    throw new Error("Sign in required");
  const response = await fetch(`/api/v1/${path}`, {
    method,
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...(csrf ? { "X-CSRF-Token": csrf } : {}),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const result = await response.json();
  if (response.status === 401) {
    clearSession();
    throw new Error("Sign in required");
  }
  if (!response.ok) throw new Error(result.error?.code || "Unavailable");
  return result.data;
}
export async function restoreSession() {
  const session = await api<{ csrf: string; user: User }>("session");
  if (session.user.role !== "admin") {
    clearSession();
    throw new Error("Administrator access required");
  }
  csrf = session.csrf;
  currentUser.value = session.user;
  return session;
}
export async function signIn(username: string, password: string) {
  await api("login", "POST", { username, password });
  return restoreSession();
}
export async function signOut() {
  await api("logout", "POST", {});
  clearSession();
}
