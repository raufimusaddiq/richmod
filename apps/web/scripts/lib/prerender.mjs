export async function loadPrerenderer() {
  process.env.NODE_ENV = "production";
  const [{ renderToString }, { createElement }, { default: App }] = await Promise.all([
    import("react-dom/server"),
    import("react"),
    import("../../app/App.js"),
  ]);
  return (pathname, search = "") => renderToString(createElement(App, { initial: { pathname, search } }), { onError() {} });
}
