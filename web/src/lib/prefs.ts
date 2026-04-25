// User preferences persisted in localStorage. Wire has no users endpoint;
// theme/font are client-side only and survive across reloads via these keys.

export type Theme = 'system' | 'light' | 'dark' | 'sepia';
export type Font = 'sans' | 'serif';

const THEME_KEY = 'wire.theme';
const FONT_KEY = 'wire.font';

const THEMES: readonly Theme[] = ['system', 'light', 'dark', 'sepia'];
const FONTS: readonly Font[] = ['sans', 'serif'];

function readStorage(key: string): string | null {
	if (typeof localStorage === 'undefined') return null;
	try {
		return localStorage.getItem(key);
	} catch {
		return null;
	}
}

function writeStorage(key: string, value: string): void {
	if (typeof localStorage === 'undefined') return;
	try {
		localStorage.setItem(key, value);
	} catch {
		/* private mode / quota — fall through; in-memory dataset still applies */
	}
}

export function getTheme(): Theme {
	const v = readStorage(THEME_KEY);
	return (THEMES as readonly string[]).includes(v ?? '') ? (v as Theme) : 'system';
}

export function setTheme(theme: Theme): void {
	writeStorage(THEME_KEY, theme);
	if (typeof document === 'undefined') return;
	if (theme === 'system') {
		delete document.documentElement.dataset.theme;
	} else {
		document.documentElement.dataset.theme = theme;
	}
}

export function getFont(): Font {
	const v = readStorage(FONT_KEY);
	return (FONTS as readonly string[]).includes(v ?? '') ? (v as Font) : 'sans';
}

export function setFont(font: Font): void {
	writeStorage(FONT_KEY, font);
	if (typeof document === 'undefined') return;
	document.documentElement.dataset.font = font;
}

// applyStoredPrefs reads localStorage and reflects the values onto <html>'s
// dataset. Call once on mount from +layout.svelte so the page paints with the
// user's chosen theme/font before any component reads CSS custom properties.
export function applyStoredPrefs(): void {
	setTheme(getTheme());
	setFont(getFont());
}
