// safeNext keeps the after-sign-in address on this site: a path, never a scheme
// ("javascript:", "https://evil.com"), a protocol-relative "//host", or one with
// backslashes or control characters, which browsers turn into "//host".
export function safeNext(next: string | null | undefined): string {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.includes("\\") || /[\u0000-\u001f\u007f]/.test(next)) return "/";
  return next;
}
