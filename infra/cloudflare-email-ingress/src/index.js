export default {
  async email(message, env) {
    const raw = await new Response(message.raw).arrayBuffer();
    const receivedAt = new Date();
    const objectKey = [
      "raw",
      receivedAt.getUTCFullYear(),
      String(receivedAt.getUTCMonth() + 1).padStart(2, "0"),
      String(receivedAt.getUTCDate()).padStart(2, "0"),
      `${crypto.randomUUID()}.eml`,
    ].join("/");

    await env.RICHMOD_EMAIL_RAW.put(objectKey, raw, {
      httpMetadata: { contentType: "message/rfc822" },
      customMetadata: {
        recipient: message.to.toLowerCase(),
        envelopeFrom: message.from,
      },
    });

    await env.RICHMOD_EMAIL_DELIVERY.send({
      objectKey,
      recipient: message.to.toLowerCase(),
      envelopeFrom: message.from,
      messageId: message.headers.get("Message-ID") || "",
    });
  },
};
