function encodeFont(bytes) {
  if (typeof bytes.toBase64 === "function") return bytes.toBase64();

  // Older browsers have btoa but no typed-array base64 support. Chunk the
  // conversion so a large font never exceeds the function argument limit.
  let binary = "";
  const chunkSize = 0x8000;
  for (let start = 0; start < bytes.length; start += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(start, start + chunkSize));
  }
  return btoa(binary);
}

export function stringifyCompileRequest(request) {
  const options = { ...request.options };
  for (const key of ["fontRegular", "fontItalic", "fontBold", "fontSemibold"]) {
    if (options[key] instanceof Uint8Array) {
      // Go's encoding/json reads []byte from base64 strings or number arrays.
      options[key] = encodeFont(options[key]);
    }
  }
  return JSON.stringify({ ...request, options });
}
