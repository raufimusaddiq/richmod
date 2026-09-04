const encoder = new TextEncoder();

function hex(bytes) {
  return [...new Uint8Array(bytes)].map((value) => value.toString(16).padStart(2, "0")).join("");
}

async function signature(secret, canonical) {
  const key = await crypto.subtle.importKey("raw", encoder.encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return hex(await crypto.subtle.sign("HMAC", key, encoder.encode(canonical)));
}

async function deliver(item, env) {
  const object = await env.RICHMOD_EMAIL_RAW.get(item.objectKey);
  if (!object) throw new Error("raw email object not found");
  const raw = await object.arrayBuffer();
  const contentHash = hex(await crypto.subtle.digest("SHA-256", raw));
  const timestamp = String(Math.floor(Date.now() / 1000));
  const canonical = `${timestamp}\n${item.recipient}\n${item.envelopeFrom}\n${contentHash}`;
  const response = await fetch(env.RICHMOD_INGRESS_URL, {
    method: "POST",
    headers: {
      "Content-Type": "message/rfc822",
      "X-Richmod-Timestamp": timestamp,
      "X-Richmod-Recipient": item.recipient,
      "X-Richmod-Envelope-From": item.envelopeFrom,
      "X-Richmod-Content-SHA256": contentHash,
      "X-Richmod-Signature": await signature(env.RICHMOD_INGRESS_SECRET, canonical),
      "X-Richmod-Message-ID": item.messageId || "",
      "X-Richmod-Object-Key": item.objectKey,
    },
    body: raw,
  });
  if (!response.ok) throw new Error(`Richmod ingress returned HTTP ${response.status}`);
}

export default {
  async queue(batch, env) {
    for (const message of batch.messages) {
      try {
        await deliver(message.body, env);
        message.ack();
      } catch (error) {
        console.error("email delivery failed", { objectKey: message.body?.objectKey, error: String(error) });
        message.retry();
      }
    }
  },
};

export { deliver, hex, signature };
