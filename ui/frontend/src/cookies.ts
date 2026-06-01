// ABOUTME: Browser cookie helpers for reading and deleting document cookies.
// ABOUTME: Used by the settings menu to switch between the new and old UI.

export function getCookie(name: string): string | undefined {
  const prefix = `${name}=`;
  for (const part of document.cookie.split(";")) {
    const cookie = part.trim();
    if (cookie.startsWith(prefix)) {
      return decodeURIComponent(cookie.slice(prefix.length));
    }
  }
  return undefined;
}

export function deleteCookie(name: string): void {
  document.cookie = `${name}=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT`;
}
