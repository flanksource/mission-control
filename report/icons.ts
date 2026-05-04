// Audit Findings Icon Set — Selected from REQUIREMENTS-audit-icon-set.pdf
// Sources: Phosphor (ph/256), MDI (24), Tabler (24), Lucide (24), Icons8 (50)
// Render: <svg viewBox={icon.viewBox} width={size} height={size} dangerouslySetInnerHTML={{ __html: icon.body }} />

export interface IconDef { body: string; viewBox: string }

function ph(body: string): IconDef { return { body, viewBox: "0 0 256 256" }; }
function mdi(body: string): IconDef { return { body, viewBox: "0 0 24 24" }; }
function tabler(body: string): IconDef { return { body, viewBox: "0 0 24 24" }; }
function lucide(body: string): IconDef { return { body, viewBox: "0 0 24 24" }; }

// ── Outcomes ────────────────────────────────────────────────────────

// ph:power
export const ICON_SAFETY_SWITCH = ph(`<path fill="currentColor" d="M120 128V48a8 8 0 0 1 16 0v80a8 8 0 0 1-16 0m60.37-78.7a8 8 0 0 0-8.74 13.4C194.74 77.77 208 101.57 208 128a80 80 0 0 1-160 0c0-26.43 13.26-50.23 36.37-65.3a8 8 0 0 0-8.74-13.4C47.9 67.38 32 96.06 32 128a96 96 0 0 0 192 0c0-31.94-15.9-60.62-43.63-78.7"/>`);

// icons8:fire-alarm (kept as tabler:bell-ringing equivalent — stroke 24x24)
export const ICON_PAGE_ONCALL = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 18v-6a5 5 0 1 1 10 0v6M5 21a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-1a2 2 0 0 0-2-2H7a2 2 0 0 0-2 2zm16-9h1m-3.5-7.5L18 5M2 12h1m9-10v1M4.929 4.929l.707.707M12 12v6"/>`);

// icons8:high-risk (kept as tabler:alert-triangle — stroke 24x24)
export const ICON_HIGH_TICKET = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v4m-1.637-9.409L2.257 17.125a1.914 1.914 0 0 0 1.636 2.871h16.214a1.914 1.914 0 0 0 1.636-2.87L13.637 3.59a1.914 1.914 0 0 0-3.274 0M12 16h.01"/>`);

// icons8:ticket (kept as tabler:ticket — stroke 24x24)
export const ICON_LOW_TICKET = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 5v2m0 4v2m0 4v2M5 5h14a2 2 0 0 1 2 2v3a2 2 0 0 0 0 4v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-3a2 2 0 0 0 0-4V7a2 2 0 0 1 2-2"/>`);

// ph:scroll
export const ICON_INFORMATIONAL = ph(`<path fill="currentColor" d="M96 104a8 8 0 0 1 8-8h64a8 8 0 0 1 0 16h-64a8 8 0 0 1-8-8m8 40h64a8 8 0 0 0 0-16h-64a8 8 0 0 0 0 16m128 48a32 32 0 0 1-32 32H88a32 32 0 0 1-32-32V64a16 16 0 0 0-32 0c0 5.74 4.83 9.62 4.88 9.66A8 8 0 0 1 24 88a7.9 7.9 0 0 1-4.79-1.61C18.05 85.54 8 77.61 8 64a32 32 0 0 1 32-32h136a32 32 0 0 1 32 32v104h8a8 8 0 0 1 4.8 1.6c1.2.86 11.2 8.79 11.2 22.4M96.26 173.48A8.07 8.07 0 0 1 104 168h88V64a16 16 0 0 0-16-16H67.69A31.7 31.7 0 0 1 72 64v128a16 16 0 0 0 32 0c0-5.74-4.83-9.62-4.88-9.66a7.82 7.82 0 0 1-2.86-8.86M216 192a12.58 12.58 0 0 0-3.23-8h-94a27 27 0 0 1 1.21 8a31.8 31.8 0 0 1-4.29 16H200a16 16 0 0 0 16-16"/>`);

// ── Attack Categories ───────────────────────────────────────────────

// mdi:key-alert
export const ICON_CREDENTIAL_ATTACK = mdi(`<path fill="currentColor" d="M4 6.5C4 4 6 2 8.5 2S13 4 13 6.5c0 1.96-1.25 3.63-3 4.24V15h3v3h-3v4H7V10.74c-1.75-.61-3-2.28-3-4.24m3 0C7 7.33 7.67 8 8.5 8S10 7.33 10 6.5S9.33 5 8.5 5S7 5.67 7 6.5M18 7h2v6h-2m0 4h2v-2h-2"/>`);

// mdi:shield-account
export const ICON_PRIVILEGE_ESCALATION = mdi(`<path fill="currentColor" d="M12 1L3 5v6c0 5.55 3.84 10.74 9 12c5.16-1.26 9-6.45 9-12V5zm0 4a3 3 0 0 1 3 3a3 3 0 0 1-3 3a3 3 0 0 1-3-3a3 3 0 0 1 3-3m5.13 12A9.7 9.7 0 0 1 12 20.92A9.7 9.7 0 0 1 6.87 17c-.34-.5-.63-1-.87-1.53c0-1.65 2.71-3 6-3s6 1.32 6 3c-.24.53-.53 1.03-.87 1.53"/>`);

// tabler:stack-push
export const ICON_PRIVILEGE_ACCUMULATION = tabler(`<g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="m6 10l-2 1l8 4l8-4l-2-1M4 15l8 4l8-4M12 4v7"/><path d="m15 8l-3 3l-3-3"/></g>`);

// mdi:database-export
export const ICON_DATA_EXFILTRATION = mdi(`<path fill="currentColor" d="M12 3C7.58 3 4 4.79 4 7s3.58 4 8 4c.5 0 1-.03 1.5-.08V9.5h2.89l-1-1L18.9 5c-1.4-1.2-3.96-2-6.9-2m6.92 4.08L17.5 8.5L20 11h-5v2h5l-2.5 2.5l1.42 1.42L23.84 12M4 9v3c0 1.68 2.07 3.12 5 3.7v-.2c0-.93.2-1.85.58-2.69C6.34 12.3 4 10.79 4 9m0 5v3c0 2.21 3.58 4 8 4c2.94 0 5.5-.8 6.9-2L17 17.1c-1.39.56-3.1.9-5 .9c-4.42 0-8-1.79-8-4"/>`);

// (no selection — kept original)
export const ICON_LATERAL_MOVEMENT = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="m7 8l-4 4l4 4m10-8l4 4l-4 4M3 12h18"/>`);

// (no selection — kept original)
export const ICON_PERSISTENCE = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v12m-8-8a8 8 0 0 0 16 0m1 0h-2M5 13H3m6-7a3 3 0 1 0 6 0a3 3 0 1 0-6 0"/>`);

// lucide:shredder
export const ICON_AUDIT_TAMPERING = lucide(`<g fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2"><path d="M4 13V4a2 2 0 0 1 2-2h8a2.4 2.4 0 0 1 1.706.706l3.588 3.588A2.4 2.4 0 0 1 20 8v5"/><path d="M14 2v5a1 1 0 0 0 1 1h5M10 22v-5m4 2v-2m4 3v-3M2 13h20M6 20v-3"/></g>`);

// ph:moon-stars
export const ICON_AFTER_HOURS = ph(`<path fill="currentColor" d="M240 96a8 8 0 0 1-8 8h-16v16a8 8 0 0 1-16 0v-16h-16a8 8 0 0 1 0-16h16V72a8 8 0 0 1 16 0v16h16a8 8 0 0 1 8 8m-96-40h8v8a8 8 0 0 0 16 0v-8h8a8 8 0 0 0 0-16h-8v-8a8 8 0 0 0-16 0v8h-8a8 8 0 0 0 0 16m72.77 97a8 8 0 0 1 1.43 8A96 96 0 1 1 95.07 37.8a8 8 0 0 1 10.6 9.06a88.07 88.07 0 0 0 103.47 103.47a8 8 0 0 1 7.63 2.67m-19.39 14.88c-1.79.09-3.59.14-5.38.14A104.11 104.11 0 0 1 88 64c0-1.79 0-3.59.14-5.38a80 80 0 1 0 109.24 109.24Z"/>`);

// icons8:hammer (kept as tabler:hammer — stroke 24x24)
export const ICON_BREAK_GLASS = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="m11.414 10l-7.383 7.418a2.09 2.09 0 0 0 0 2.967a2.11 2.11 0 0 0 2.976 0L14.414 13m3.707 2.293l2.586-2.586a1 1 0 0 0 0-1.414l-7.586-7.586a1 1 0 0 0-1.414 0L9.121 6.293a1 1 0 0 0 0 1.414l7.586 7.586a1 1 0 0 0 1.414 0"/>`);

// mdi:account-switch
export const ICON_SHARED_ACCOUNT = mdi(`<path fill="currentColor" d="M16 9c6 0 6 4 6 4v2h-6v-2s0-1.69-1.15-3.2c-.17-.23-.38-.45-.6-.66C14.77 9.06 15.34 9 16 9M2 13s0-4 6-4s6 4 6 4v2H2zm7 4v2h6v-2l3 3l-3 3v-2H9v2l-3-3zM8 1C6.34 1 5 2.34 5 4s1.34 3 3 3s3-1.34 3-3s-1.34-3-3-3m8 0c-1.66 0-3 1.34-3 3s1.34 3 3 3s3-1.34 3-3s-1.34-3-3-3"/>`);

// (no selection — kept original)
export const ICON_SERVICE_ACCOUNT_MISUSE = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 6a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v4a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2zm6-4v2m-3 8v9m6-9v9M5 16l4-2m6 0l4 2M9 18h6M10 8v.01M14 8v.01"/>`);

// mdi:firewall (alias: wall-fire)
export const ICON_NETWORK_EXPOSURE = mdi(`<path fill="currentColor" d="m22.14 15.34l-.02.01c.23.28.43.59.58.92l.09.19c.71 1.69.21 3.64-1.1 4.86c-1.19 1.09-2.85 1.38-4.39 1.18c-1.46-.18-2.8-1.1-3.57-2.37c-.23-.39-.43-.83-.53-1.28c-.13-.35-.17-.73-.2-1.1c-.09-1.6.55-3.3 1.76-4.3c-.55 1.21-.42 2.72.39 3.77l.11.13c.14.12.31.15.47.09c.15-.06.27-.21.27-.37l-.07-.24c-.88-2.33-.14-5.03 1.73-6.56c.51-.42 1.14-.8 1.8-.97c-.68 1.36-.46 3.14.63 4.2c.46.5 1.02.79 1.49 1.23zM19.86 20l-.01-.03c.45-.39.7-1.06.68-1.66L20.5 18c-.2-1-1.07-1.34-1.63-2.07l-.43-.78c-.22.5-.24.97-.15 1.51c.1.57.32 1.06.21 1.65c-.16.65-.67 1.3-1.56 1.51c.5.49 1.31.88 2.12.6c.26-.07.59-.26.8-.42M3 16h8.06L11 17c0 1.41.36 2.73 1 3.88V21H3zm-1-6h6v5H2zm7 0h6v.07A8.03 8.03 0 0 0 11.25 15H9zM3 4h8v5H3zm9 0h9v5h-9z"/>`);

// ph:bomb
export const ICON_DESTRUCTIVE_ACTION = ph(`<path fill="currentColor" d="M248 32a8 8 0 0 0-8 8a52.7 52.7 0 0 1-3.57 17.39C232.38 67.22 225.7 72 216 72c-11.06 0-18.85-9.76-29.49-24.65C176 32.66 164.12 16 144 16c-16.39 0-29 8.89-35.43 25a66 66 0 0 0-3.9 15H88a16 16 0 0 0-16 16v9.59A88 88 0 0 0 112 248h1.59A88 88 0 0 0 152 81.59V72a16 16 0 0 0-16-16h-15.12a46.8 46.8 0 0 1 2.69-9.37C127.62 36.78 134.3 32 144 32c11.06 0 18.85 9.76 29.49 24.65C184 71.34 195.88 88 216 88c16.39 0 29-8.89 35.43-25A68.7 68.7 0 0 0 256 40a8 8 0 0 0-8-8M140.8 94a72 72 0 1 1-57.6 0a8 8 0 0 0 4.8-7.34V72h48v14.66a8 8 0 0 0 4.8 7.34m-28.91 115.32A8 8 0 0 1 104 216a8.5 8.5 0 0 1-1.33-.11a57.5 57.5 0 0 1-46.57-46.57a8 8 0 1 1 15.78-2.64a41.29 41.29 0 0 0 33.43 33.43a8 8 0 0 1 6.58 9.21"/>`);

// ph:eye-slash
export const ICON_COVERAGE_GAP = ph(`<path fill="currentColor" d="M53.92 34.62a8 8 0 1 0-11.84 10.76l19.24 21.17C25 88.84 9.38 123.2 8.69 124.76a8 8 0 0 0 0 6.5c.35.79 8.82 19.57 27.65 38.4C61.43 194.74 93.12 208 128 208a127.1 127.1 0 0 0 52.07-10.83l22 24.21a8 8 0 1 0 11.84-10.76Zm47.33 75.84l41.67 45.85a32 32 0 0 1-41.67-45.85M128 192c-30.78 0-57.67-11.19-79.93-33.25A133.2 133.2 0 0 1 25 128c4.69-8.79 19.66-33.39 47.35-49.38l18 19.75a48 48 0 0 0 63.66 70l14.73 16.2A112 112 0 0 1 128 192m6-95.43a8 8 0 0 1 3-15.72a48.16 48.16 0 0 1 38.77 42.64a8 8 0 0 1-7.22 8.71a6 6 0 0 1-.75 0a8 8 0 0 1-8-7.26A32.09 32.09 0 0 0 134 96.57m113.28 34.69c-.42.94-10.55 23.37-33.36 43.8a8 8 0 1 1-10.67-11.92a132.8 132.8 0 0 0 27.8-35.14a133.2 133.2 0 0 0-23.12-30.77C185.67 75.19 158.78 64 128 64a118.4 118.4 0 0 0-19.36 1.57A8 8 0 1 1 106 49.79A134 134 0 0 1 128 48c34.88 0 66.57 13.26 91.66 38.35c18.83 18.83 27.3 37.62 27.65 38.41a8 8 0 0 1 0 6.5Z"/>`);

// ── Kill Chain Phases (unique, others reuse above) ──────────────────

// mdi:radar
export const ICON_RECONNAISSANCE = mdi(`<path fill="currentColor" d="m19.07 4.93l-1.41 1.41A8 8 0 0 1 20 12a8 8 0 0 1-8 8a8 8 0 0 1-8-8c0-4.08 3.05-7.44 7-7.93v2.02C8.16 6.57 6 9.03 6 12a6 6 0 0 0 6 6a6 6 0 0 0 6-6c0-1.66-.67-3.16-1.76-4.24l-1.41 1.41C15.55 9.9 16 10.9 16 12a4 4 0 0 1-4 4a4 4 0 0 1-4-4c0-1.86 1.28-3.41 3-3.86v2.14c-.6.35-1 .98-1 1.72a2 2 0 0 0 2 2a2 2 0 0 0 2-2c0-.74-.4-1.38-1-1.72V2h-1A10 10 0 0 0 2 12a10 10 0 0 0 10 10a10 10 0 0 0 10-10c0-2.76-1.12-5.26-2.93-7.07"/>`);

// icons8:door-opened (kept as tabler equivalent — stroke 24x24)
export const ICON_INITIAL_ACCESS = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 12v.01M3 21h18M5 21V5a2 2 0 0 1 2-2h6m4 10.5V21m4-14h-7m3-3l-3 3l3 3"/>`);

// mdi:database-search
export const ICON_COLLECTION = mdi(`<path fill="currentColor" d="M18.68 12.32a4.49 4.49 0 0 0-6.36.01a4.49 4.49 0 0 0 0 6.36a4.51 4.51 0 0 0 5.57.63L21 22.39L22.39 21l-3.09-3.11c1.13-1.77.87-4.09-.62-5.57m-1.41 4.95c-.98.98-2.56.97-3.54 0c-.97-.98-.97-2.56.01-3.54c.97-.97 2.55-.97 3.53 0c.97.98.97 2.56 0 3.54M10.9 20.1a6.5 6.5 0 0 1-1.48-2.32C6.27 17.25 4 15.76 4 14v3c0 2.21 3.58 4 8 4c-.4-.26-.77-.56-1.1-.9M4 9v3c0 1.68 2.07 3.12 5 3.7v-.2c0-.93.2-1.85.58-2.69C6.34 12.3 4 10.79 4 9m8-6C7.58 3 4 4.79 4 7c0 2 3 3.68 6.85 4h.05c1.2-1.26 2.86-2 4.6-2c.91 0 1.81.19 2.64.56A3.22 3.22 0 0 0 20 7c0-2.21-3.58-4-8-4"/>`);

// mdi:database-export
export const ICON_EXFILTRATION = mdi(`<path fill="currentColor" d="M12 3C7.58 3 4 4.79 4 7s3.58 4 8 4c.5 0 1-.03 1.5-.08V9.5h2.89l-1-1L18.9 5c-1.4-1.2-3.96-2-6.9-2m6.92 4.08L17.5 8.5L20 11h-5v2h5l-2.5 2.5l1.42 1.42L23.84 12M4 9v3c0 1.68 2.07 3.12 5 3.7v-.2c0-.93.2-1.85.58-2.69C6.34 12.3 4 10.79 4 9m0 5v3c0 2.21 3.58 4 8 4c2.94 0 5.5-.8 6.9-2L17 17.1c-1.39.56-3.1.9-5 .9c-4.42 0-8-1.79-8-4"/>`);

// mdi:flash-alert
export const ICON_IMPACT = mdi(`<path fill="currentColor" d="M5 2v11h3v9l7-12h-4l4-8m2 13h2v2h-2zm0-8h2v6h-2z"/>`);

// ── Actor Type Icons ───────────────────────────────────────────────��

// tabler:user (human identity)
export const ICON_IDENTITY_HUMAN = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7a4 4 0 1 0 8 0a4 4 0 1 0-8 0M6 21v-2a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v2"/>`);

// tabler:robot (machine identity)
export const ICON_IDENTITY_MACHINE = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 4a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v4a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2zm6-4v2m-3 8v9m6-9v9M5 16l4-2m6 0l4 2M9 18h6M10 8v.01M14 8v.01"/>`);

// tabler:crown (root identity)
export const ICON_IDENTITY_ROOT = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6l4 6l5-4l-2 10H5L3 8l5 4z"/>`);

// tabler:question-mark (unknown identity)
export const ICON_IDENTITY_UNKNOWN = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 8a3.5 3.5 0 0 1 3.5-3.5h1A3.5 3.5 0 0 1 16 8a3 3 0 0 1-2 3a3 3 0 0 0-2 3m0 4h.01"/>`);

// tabler:world (IP / endpoint)
export const ICON_ENDPOINT_IP = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 12a9 9 0 1 0 18 0a9 9 0 1 0-18 0m.6-3h16.8M3.6 15h16.8M11.5 3a17 17 0 0 0 0 18m1-18a17 17 0 0 1 0 18"/>`);

// tabler:device-desktop (workstation endpoint)
export const ICON_ENDPOINT_WORKSTATION = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 5a1 1 0 0 1 1-1h16a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1zm4 16h10m-6 0v-4m2 4v-4"/>`);

// tabler:server (server endpoint)
export const ICON_ENDPOINT_SERVER = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 4h18a1 1 0 0 1 1 1v4a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1m0 10h18a1 1 0 0 1 1 1v4a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1v-4a1 1 0 0 1 1-1M7 8h.01M7 18h.01"/>`);

// tabler:app-window (app reference)
export const ICON_APP = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zm6 0v16M6 8h.01M6 11h.01"/>`);

// mdi:database
export const ICON_DATABASE = mdi(`<path fill="currentColor" d="M12 3C7.58 3 4 4.79 4 7s3.58 4 8 4s8-1.79 8-4s-3.58-4-8-4M4 9v3c0 2.21 3.58 4 8 4s8-1.79 8-4V9c0 2.21-3.58 4-8 4s-8-1.79-8-4m0 5v3c0 2.21 3.58 4 8 4s8-1.79 8-4v-3c0 2.21-3.58 4-8 4s-8-1.79-8-4"/>`);

// mdi:lock
export const ICON_SECRET = mdi(`<path fill="currentColor" d="M12 17a2 2 0 0 0 2-2a2 2 0 0 0-2-2a2 2 0 0 0-2 2a2 2 0 0 0 2 2m6-9a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V10a2 2 0 0 1 2-2h1V6a5 5 0 0 1 5-5a5 5 0 0 1 5 5v2zm-6-5a3 3 0 0 0-3 3v2h6V6a3 3 0 0 0-3-3"/>`);

// ── Provenance & Data Source Icons ─────────────────────────────────

// tabler:brain (AI model)
export const ICON_AI_MODEL = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.5 13a3.5 3.5 0 0 0-3.5 3.5v1a3.5 3.5 0 0 0 7 0v-1.8M8.5 13a3.5 3.5 0 0 1 3.5 3.5v1a3.5 3.5 0 0 1-7 0v-1.8M17.5 16a3.5 3.5 0 0 0 0-7H17M6.5 16a3.5 3.5 0 0 1 0-7H7m8.5-2a3.5 3.5 0 0 0-3.5-3.5a3.5 3.5 0 0 0-3.5 3.5v.5"/>`);

// tabler:clock (timestamp)
export const ICON_CLOCK = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 12a9 9 0 1 0 18 0a9 9 0 1 0-18 0m9-5v5l3 3"/>`);

// tabler:hash (run ID)
export const ICON_HASH = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 9h14M5 15h14M11 4L7 20m10-16l-4 16"/>`);

// tabler:tool (analyzer tool)
export const ICON_TOOL = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 10h3V7L6.5 3.5a6 6 0 0 1 8 8l6 6a2 2 0 0 1-3 3l-6-6a6 6 0 0 1-8-8z"/>`);

// tabler:tag (version)
export const ICON_VERSION = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7.5 7.5m-1 0a1 1 0 1 0 2 0a1 1 0 1 0-2 0M3 6v5.172a2 2 0 0 0 .586 1.414l7.71 7.71a2.41 2.41 0 0 0 3.408 0l5.592-5.592a2.41 2.41 0 0 0 0-3.408l-7.71-7.71A2 2 0 0 0 11.172 3H6a3 3 0 0 0-3 3"/>`);

// tabler:bucket (S3/storage)
export const ICON_BUCKET = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 7h18M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2l1-12M9 3h6l1 4H8z"/>`);

// tabler:file-analytics (audit log)
export const ICON_AUDIT_LOG = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14 3v4a1 1 0 0 0 1 1h4M4 4.5A2.5 2.5 0 0 1 6.5 2H14l5 5v10.5a2.5 2.5 0 0 1-2.5 2.5h-11A2.5 2.5 0 0 1 4 17.5zM9 17v-4m4 4v-6m-8 6v-2"/>`);

// tabler:cloud (cloud trail / cloud source)
export const ICON_CLOUD = tabler(`<path fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6.657 18C4.085 18 2 15.993 2 13.517s2.085-4.482 4.657-4.482c.393-1.762 1.794-3.2 3.675-3.773c1.88-.572 3.956-.193 5.444 1c1.488 1.19 2.162 3.007 1.77 4.769h.99c1.913 0 3.464 1.56 3.464 3.486C22 16.44 20.449 18 18.536 18z"/>`);

// ── Lookup Maps ─────────────────────────────────────────────────────

export const OUTCOME_ICONS: Record<string, IconDef> = {
  "safety-switch": ICON_SAFETY_SWITCH,
  "page-oncall": ICON_PAGE_ONCALL,
  "high-ticket": ICON_HIGH_TICKET,
  "low-ticket": ICON_LOW_TICKET,
  "informational": ICON_INFORMATIONAL,
};

export const CATEGORY_ICONS: Record<string, IconDef> = {
  "credential-attack": ICON_CREDENTIAL_ATTACK,
  "privilege-escalation": ICON_PRIVILEGE_ESCALATION,
  "privilege-accumulation": ICON_PRIVILEGE_ACCUMULATION,
  "data-exfiltration": ICON_DATA_EXFILTRATION,
  "lateral-movement": ICON_LATERAL_MOVEMENT,
  "persistence": ICON_PERSISTENCE,
  "audit-tampering": ICON_AUDIT_TAMPERING,
  "after-hours": ICON_AFTER_HOURS,
  "break-glass": ICON_BREAK_GLASS,
  "shared-account": ICON_SHARED_ACCOUNT,
  "service-account-misuse": ICON_SERVICE_ACCOUNT_MISUSE,
  "network-exposure": ICON_NETWORK_EXPOSURE,
  "destructive-action": ICON_DESTRUCTIVE_ACTION,
  "coverage-gap": ICON_COVERAGE_GAP,
};

export const KILL_CHAIN_ICONS: Record<string, IconDef> = {
  "reconnaissance": ICON_RECONNAISSANCE,
  "initial-access": ICON_INITIAL_ACCESS,
  "persistence": ICON_PERSISTENCE,
  "privilege-escalation": ICON_PRIVILEGE_ESCALATION,
  "lateral-movement": ICON_LATERAL_MOVEMENT,
  "collection": ICON_COLLECTION,
  "exfiltration": ICON_EXFILTRATION,
  "impact": ICON_IMPACT,
};

export const IDENTITY_ICONS: Record<string, IconDef> = {
  "human": ICON_IDENTITY_HUMAN,
  "service-account": ICON_SERVICE_ACCOUNT_MISUSE,
  "break-glass": ICON_BREAK_GLASS,
  "admin": ICON_PRIVILEGE_ESCALATION,
  "machine": ICON_IDENTITY_MACHINE,
  "root": ICON_IDENTITY_ROOT,
  "unknown": ICON_IDENTITY_UNKNOWN,
};

export const ENDPOINT_ICONS: Record<string, IconDef> = {
  "ip": ICON_ENDPOINT_IP,
  "workstation": ICON_ENDPOINT_WORKSTATION,
  "server": ICON_ENDPOINT_SERVER,
  "vpn": ICON_ENDPOINT_IP,
};

export const RESOURCE_ICONS: Record<string, IconDef> = {
  "database": ICON_DATABASE,
  "role": ICON_PRIVILEGE_ESCALATION,
  "clusterrolebinding": ICON_PRIVILEGE_ESCALATION,
  "clusterrole": ICON_PRIVILEGE_ESCALATION,
  "security-group": ICON_NETWORK_EXPOSURE,
  "rds-instance": ICON_DATABASE,
  "audit-type": ICON_AUDIT_TAMPERING,
  "namespace": ICON_LATERAL_MOVEMENT,
  "secret": ICON_SECRET,
  "s3-bucket": ICON_DATA_EXFILTRATION,
};

export const APP_ICONS: Record<string, IconDef> = {
  "default": ICON_APP,
};

