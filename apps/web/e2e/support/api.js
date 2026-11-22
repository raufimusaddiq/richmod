const REPLY = Symbol("reply");

export function reply(body, { status = 200, headers = {}, contentType = "application/json", delay = 0 } = {}) {
  return { [REPLY]: true, body, status, headers, contentType, delay };
}

const PIXEL = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64");
const IMAGE_PATH = /\/api\/v1\/documents\/[^/]+\/(thumbnail|content|pages\/\d+\/content)$/;

function describe(request) {
  const url = new URL(request.url());
  let body = null;
  try {
    body = request.postDataJSON();
  } catch {
    body = null;
  }
  return { path: url.pathname, search: url.search, query: url.searchParams, method: request.method(), headers: request.headers(), body };
}

function matches(matcher, request) {
  if (typeof matcher === "string") return request.path === matcher;
  if (matcher instanceof RegExp) return matcher.test(request.path);
  return Boolean(matcher(request));
}

export function createApi(user) {
  const handlers = [];
  const api = {
    user,
    requests: [],
    telemetry: [],
    resolver: null,
    use(resolver) {
      api.resolver = resolver;
      return api;
    },
    on(matcher, respond, { method } = {}) {
      handlers.unshift({ matcher, respond, method });
      return api;
    },
    calls(matcher, method) {
      return api.requests.filter(request => matches(matcher, request) && (!method || request.method === method));
    },
    async resolve(request) {
      for (const handler of handlers) {
        if (handler.method && handler.method !== request.method) continue;
        if (!matches(handler.matcher, request)) continue;
        const result = typeof handler.respond === "function" ? await handler.respond(request) : handler.respond;
        if (result !== undefined) return result;
      }
      if (api.resolver) {
        const result = await api.resolver(request);
        if (result !== undefined) return result;
      }
      if (request.path === "/api/v1/auth/me") return api.user;
      return [];
    },
    async attach(page) {
      await page.route("**/api/v1/**", async route => {
        const request = describe(route.request());
        if (request.path === "/api/v1/telemetry/events") {
          api.telemetry.push(request.body);
          await route.fulfill({ status: 204 });
          return;
        }
        api.requests.push(request);
        if (IMAGE_PATH.test(request.path)) {
          await route.fulfill({ contentType: "image/png", body: PIXEL });
          return;
        }
        const result = await api.resolve(request);
        if (result && result[REPLY]) {
          if (result.delay) await new Promise(resolve => setTimeout(resolve, result.delay));
          await route.fulfill({ status: result.status, contentType: result.contentType, headers: result.headers, body: typeof result.body === "string" ? result.body : JSON.stringify(result.body) });
          return;
        }
        await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(result) });
      });
    },
  };
  return api;
}
