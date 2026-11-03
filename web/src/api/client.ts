// Thin fetch wrapper for /api/v1. The session token from POST /auth/login
// lives in sessionStorage and is sent as "Authorization: Bearer".

const KEY = "msapi-session";

export const session = {
  get: (): string | null => {
    try {
      return sessionStorage.getItem(KEY);
    } catch {
      return null;
    }
  },
  set: (k: string) => {
    try {
      sessionStorage.setItem(KEY, k);
    } catch {
      /* private mode: key lives only in memory for this tab */
    }
    memoryKey = k;
  },
  clear: () => {
    try {
      sessionStorage.removeItem(KEY);
    } catch {
      /* ignore */
    }
    memoryKey = null;
  },
};
let memoryKey: string | null = null;

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public details: string[] = [],
  ) {
    super(message);
  }
  /** Message plus details, for toasts. */
  get full(): string {
    return this.details.length ? `${this.message}: ${this.details.join("; ")}` : this.message;
  }
}

type Body = object | string | FormData | Blob | undefined;

export async function request<T>(method: string, path: string, body?: Body, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  const token = session.get() ?? memoryKey;
  if (token) headers.set("Authorization", `Bearer ${token}`);

  let payload: BodyInit | undefined;
  if (body instanceof FormData || body instanceof Blob || typeof body === "string") {
    payload = body;
  } else if (body !== undefined) {
    headers.set("Content-Type", "application/json");
    payload = JSON.stringify(body);
  }

  const res = await fetch(`/api/v1${path}`, { ...init, method, headers, body: payload });
  if (res.status === 401) {
    session.clear();
    window.dispatchEvent(new Event("msapi:unauthorized"));
  }
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    let details: string[] = [];
    try {
      const j = await res.json();
      msg = j.error ?? msg;
      details = j.details ?? [];
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, msg, details);
  }
  if (res.status === 204) return undefined as T;
  const ct = res.headers.get("Content-Type") ?? "";
  if (ct.includes("application/json")) return res.json();
  if (ct.startsWith("text/")) return (await res.text()) as T;
  return (await res.blob()) as T;
}

export const api = {
  get: <T>(p: string) => request<T>("GET", p),
  post: <T>(p: string, b?: Body, init?: RequestInit) => request<T>("POST", p, b, init),
  put: <T>(p: string, b?: Body, init?: RequestInit) => request<T>("PUT", p, b, init),
  del: <T = void>(p: string) => request<T>("DELETE", p),
};

/** Fetches an image endpoint (needs the session header) as an object URL. */
export async function fetchImageURL(path: string, signal?: AbortSignal): Promise<string> {
  const blob = await request<Blob>("GET", path, undefined, { signal });
  return URL.createObjectURL(blob);
}

/**
 * GET /health returns 503 with a useful body when degraded; keep the body.
 * The token is sent so signed-in users see error details; /health never
 * extends the session, so polling does not defeat the idle timeout.
 */
export async function fetchHealth<T>(withStores: boolean): Promise<T> {
  const headers = new Headers();
  const token = session.get() ?? memoryKey;
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(`/api/v1/health${withStores ? "" : "?stores=false"}`, { headers });
  try {
    return (await res.json()) as T;
  } catch {
    throw new ApiError(res.status, `API unreachable (${res.status})`);
  }
}

/** POST /auth/login; keeps the token on success. */
export async function login(username: string, password: string): Promise<void> {
  const res = await fetch("/api/v1/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  let j: { token?: string; error?: string } = {};
  try {
    j = await res.json();
  } catch {
    /* not JSON: proxy or API down */
  }
  if (!res.ok || !j.token) {
    let msg = j.error ?? `The API answered ${res.status}. Is msapi running?`;
    if (res.status === 401) msg = "Wrong username or password.";
    if (res.status === 429) msg = j.error ? `${j.error[0].toUpperCase()}${j.error.slice(1)}.` : "Too many sign-in attempts. Try again later.";
    throw new ApiError(res.status, msg);
  }
  session.set(j.token);
}

/** Ends the session on the server too (POST /auth/logout), then locally. */
export async function logout(): Promise<void> {
  const token = session.get() ?? memoryKey;
  session.clear();
  if (!token) return;
  try {
    await fetch("/api/v1/auth/logout", { method: "POST", headers: { Authorization: `Bearer ${token}` } });
  } catch {
    /* offline: the session still expires on the server */
  }
}
