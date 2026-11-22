export const sample = (page, reader, steps) => page.evaluate(async ([source, times]) => {
  const read = new Function(`return (${source})()`);
  const out = [];
  let last = 0;
  for (const at of times) {
    await new Promise(resolve => setTimeout(resolve, at - last));
    last = at;
    out.push(read());
  }
  return out;
}, [reader.toString(), steps]);

export const frames = (page, clickSelector, reader, duration = 900) => page.evaluate(async ([selector, source, ms]) => {
  const read = new Function(`return (${source})()`);
  const out = [];
  const start = performance.now();
  if (selector) document.querySelector(selector).click();
  await new Promise((resolve, reject) => {
    const tick = () => {
      try {
        out.push({ t: performance.now() - start, ...read() });
      } catch (error) {
        reject(error);
        return;
      }
      if (performance.now() - start < ms) requestAnimationFrame(tick);
      else resolve();
    };
    requestAnimationFrame(tick);
  });
  return out;
}, [clickSelector, reader.toString(), duration]);
